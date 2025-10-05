// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// imageDigestResolver is the registry surface ledger signing needs:
// resolve a tag or ref to its manifest digest.
type imageDigestResolver interface {
	ResolveDigest(ctx context.Context, ref string) (string, error)
}

type ledgerImageSigner interface {
	SignImage(ctx context.Context, in domaincontainer.ImageSignRequest, errOut io.Writer) error
	AttestImage(ctx context.Context, in domaincontainer.ImageAttestRequest, errOut io.Writer) error
}

// SignLedgerImagesInput drives the signer-side release-image loop: validate the
// release-image ledger, resolve the exact digest ref to sign, generate/attest a
// CycloneDX SBOM, enrich the release SLSA predicate with per-image fields, and
// attest that predicate.
type SignLedgerImagesInput struct {
	Entries               []imageledger.Entry
	ReleaseTag            string
	PredicatePath         string
	PredicateEnvelopePath string
	Method                domainrelease.SignMethod
	Recursive             bool
	KeyRef                string
	OIDCIssuer            string

	// FulcioURL and RekorURL point keyless signing and attestation at a
	// self-hosted Sigstore. Empty uses cosign's defaults; sigstore-only.
	FulcioURL string
	RekorURL  string

	// TrustedRootPath is cosign's trusted-root document, needed to verify
	// against a self-hosted CA; sigstore-only.
	TrustedRootPath         string
	ExpectedImageRepository string
	ExpectedBaseRepository  string
	SBOMPathPattern         string
}

type ledgerSignConstraints struct {
	expectedImageRepository string
	expectedBaseRepository  string
	sbomPathPattern         string
	sbomPathRE              *regexp.Regexp
}

// SignLedgerImages is the Go port of forgejo-ci's image signing half of
// sign-promote-images.sh. It deliberately re-signs already signed images on
// rerun; duplicate registry referrers are harmless and avoiding conditional
// trust-path probes keeps the signer deterministic.
func SignLedgerImages(ctx context.Context, signer ledgerImageSigner, sbom ImageEvidenceSyft, resolver imageDigestResolver, out, stderr io.Writer, in SignLedgerImagesInput) error {
	if err := validateSignLedgerImagesInput(signer, sbom, resolver, in); err != nil {
		return err
	}

	constraints, err := ledgerSignConstraintsFromInput(in)
	if err != nil {
		return err
	}

	basePredicate, err := readLedgerSignBasePredicate(in)
	if err != nil {
		return err
	}

	if entryErr := validateLedgerSignEntries(in, constraints); entryErr != nil {
		return entryErr
	}

	predicateDir, err := os.MkdirTemp("", "image-provenance-*")
	if err != nil {
		return fmt.Errorf("container ledger sign: create predicate dir: %w", err)
	}

	defer func() { _ = os.RemoveAll(predicateDir) }()

	run := ledgerSignRun{
		signer:        signer,
		sbom:          sbom,
		resolver:      resolver,
		out:           out,
		stderr:        stderr,
		in:            in,
		basePredicate: basePredicate,
		predicateDir:  predicateDir,
	}

	plans := make([]ledgerSigningPlan, 0, len(in.Entries))
	for idx, entry := range in.Entries {
		plan, err := run.prepareEntry(ctx, idx, entry)
		if err != nil {
			return fmt.Errorf("container ledger sign: entry %d: %w", idx, err)
		}

		plans = append(plans, plan)
	}

	for idx, entry := range in.Entries {
		if err := run.signEntry(ctx, entry, plans[idx]); err != nil {
			return fmt.Errorf("container ledger sign: entry %d: %w", idx, err)
		}
	}

	return nil
}

// validateSignLedgerImagesInput checks the adapters and top-level inputs
// SignLedgerImages needs before touching any entry.
func validateSignLedgerImagesInput(signer ledgerImageSigner, sbom ImageEvidenceSyft, resolver imageDigestResolver, in SignLedgerImagesInput) error {
	if signer == nil {
		return fmt.Errorf("container ledger sign: cosign adapter is required: %w", errs.ErrUsage)
	}

	if sbom == nil {
		return fmt.Errorf("container ledger sign: syft adapter is required: %w", errs.ErrUsage)
	}

	if resolver == nil {
		return fmt.Errorf("container ledger sign: registry resolver is required: %w", errs.ErrUsage)
	}

	if in.PredicatePath == "" && in.PredicateEnvelopePath == "" {
		return fmt.Errorf("container ledger sign: provenance predicate or envelope path is required: %w", errs.ErrMissingInput)
	}

	if in.PredicatePath != "" && in.PredicateEnvelopePath != "" {
		return fmt.Errorf("container ledger sign: pass only one of provenance predicate or envelope: %w", errs.ErrUsage)
	}

	if len(in.Entries) == 0 {
		return fmt.Errorf("container ledger sign: release image ledger is empty: %w", errs.ErrValidation)
	}

	return nil
}

func validateLedgerSignEntries(in SignLedgerImagesInput, constraints ledgerSignConstraints) error {
	if in.Method != domainrelease.SignMethodSigstore && in.Method != domainrelease.SignMethodKMS {
		return fmt.Errorf("ledger signing requires sigstore or kms: %w", errs.ErrInvalidConfig)
	}

	seenSBOMs := make(map[string]bool, len(in.Entries))
	for _, entry := range in.Entries {
		if err := validateLedgerSignEntry(entry, in.ReleaseTag, constraints); err != nil {
			return err
		}

		path := filepath.Clean(entry.SBOM)
		if seenSBOMs[path] {
			return fmt.Errorf("ledger entries share an SBOM output path: %w", errs.ErrValidation)
		}

		seenSBOMs[path] = true
	}

	return nil
}

// ledgerSignRun carries the resolved per-run state so each ledger entry can be
// signed and attested without re-threading every dependency.
type ledgerSignRun struct {
	signer        ledgerImageSigner
	sbom          ImageEvidenceSyft
	resolver      imageDigestResolver
	out           io.Writer
	stderr        io.Writer
	in            SignLedgerImagesInput
	basePredicate []byte
	predicateDir  string
}

type ledgerSigningPlan struct{ imageRef, predicatePath string }

// prepareEntry resolves and prepares every local input without publication.
func (run ledgerSignRun) prepareEntry(ctx context.Context, idx int, entry imageledger.Entry) (ledgerSigningPlan, error) {
	imageRef, candidateTag, err := resolveLedgerSignRef(ctx, run.resolver, entry)
	if err != nil {
		return ledgerSigningPlan{}, err
	}

	request := domaincontainer.ImageSignRequest{ImageRef: imageRef, Recursive: run.in.Recursive, Keyless: run.in.Method == domainrelease.SignMethodSigstore, KeyRef: run.in.KeyRef, OIDCIssuer: run.in.OIDCIssuer, FulcioURL: run.in.FulcioURL, RekorURL: run.in.RekorURL, TrustedRootPath: run.in.TrustedRootPath}
	if requestErr := request.Validate(); requestErr != nil {
		return ledgerSigningPlan{}, requestErr
	}

	// Everything this entry can be refused for is settled before the
	// first irreversible publish.
	//
	// Signing is not a local act: cosign writes a Rekor entry to the
	// public transparency log on every method (ADR 0002 rule 5), and
	// that entry is permanent and append-only. Both remaining checks are
	// pure functions of inputs the run already holds -- a file hash, and
	// a set of key names -- so neither needs anything signing produces.
	// Running them after it meant a refused entry had already published
	// a signature for an image whose SBOM pin did not match, or whose
	// predicate declared a key the engine reserves. Now a refused entry
	// leaves nothing behind.
	//
	// ADR 0002 rule 2 makes the signer re-validate every rule at its
	// boundary rather than trusting the build side. Doing that before
	// publishing is the same rule, applied in the order that lets it
	// have an effect.
	//
	// When the entry declares no SBOM digest, ensureImageSBOM generates
	// the SBOM rather than checking one. That is a local file write, not
	// a publish, so it is safe on this side of the signature.
	if err = ensureImageSBOM(ctx, run.sbom, run.stderr, imageRef, entry); err != nil {
		return ledgerSigningPlan{}, err
	}

	if pathErr := validateLedgerSBOMPath(entry.SBOM, false); pathErr != nil {
		return ledgerSigningPlan{}, pathErr
	}

	predicatePath, err := writeImagePredicate(run.predicateDir, idx, run.basePredicate, entry, imageRef, candidateTag)
	if err != nil {
		return ledgerSigningPlan{}, err
	}

	return ledgerSigningPlan{imageRef: imageRef, predicatePath: predicatePath}, nil
}

// signEntry consumes only a prepared plan; later entries cannot fail preflight
// after this entry has already published a signature.
func (run ledgerSignRun) signEntry(ctx context.Context, entry imageledger.Entry, plan ledgerSigningPlan) error {
	imageRef, predicatePath := plan.imageRef, plan.predicatePath

	if err := SignImage(ctx, run.signer, run.out, SignImageInput{
		Image:           imageRef,
		Method:          run.in.Method,
		Recursive:       run.in.Recursive,
		KeyRef:          run.in.KeyRef,
		OIDCIssuer:      run.in.OIDCIssuer,
		FulcioURL:       run.in.FulcioURL,
		RekorURL:        run.in.RekorURL,
		TrustedRootPath: run.in.TrustedRootPath,
	}); err != nil {
		return err
	}

	if err := AttestImage(ctx, run.signer, run.out, AttestImageInput{
		Image:           imageRef,
		Method:          run.in.Method,
		PredicateType:   domaincontainer.PredicateTypeCycloneDX,
		PredicatePath:   entry.SBOM,
		Recursive:       run.in.Recursive,
		KeyRef:          run.in.KeyRef,
		OIDCIssuer:      run.in.OIDCIssuer,
		FulcioURL:       run.in.FulcioURL,
		RekorURL:        run.in.RekorURL,
		TrustedRootPath: run.in.TrustedRootPath,
	}); err != nil {
		return err
	}

	if err := AttestImage(ctx, run.signer, run.out, AttestImageInput{
		Image:           imageRef,
		Method:          run.in.Method,
		PredicateType:   domaincontainer.PredicateTypeSLSAProvenance1,
		PredicatePath:   predicatePath,
		Recursive:       run.in.Recursive,
		KeyRef:          run.in.KeyRef,
		OIDCIssuer:      run.in.OIDCIssuer,
		FulcioURL:       run.in.FulcioURL,
		RekorURL:        run.in.RekorURL,
		TrustedRootPath: run.in.TrustedRootPath,
	}); err != nil {
		return err
	}

	label := entry.Role
	if entry.Flavor != "" {
		label += " " + entry.Flavor
	}

	_, _ = fmt.Fprintf(run.out, "%s signed + attested: %s\n", label, imageRef)

	return nil
}

func readLedgerSignBasePredicate(in SignLedgerImagesInput) ([]byte, error) {
	if in.PredicatePath != "" {
		body, err := os.ReadFile(in.PredicatePath) //nolint:gosec // CLI/operator-supplied release artifact path.
		if err != nil {
			return nil, fmt.Errorf("container ledger sign: read provenance predicate %s: %w", in.PredicatePath, err)
		}

		return body, nil
	}

	body, err := os.ReadFile(in.PredicateEnvelopePath) //nolint:gosec // CLI/operator-supplied release artifact path.
	if err != nil {
		return nil, fmt.Errorf("container ledger sign: read provenance envelope %s: %w", in.PredicateEnvelopePath, err)
	}

	var envelope struct {
		Predicate json.RawMessage `json:"predicate"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("container ledger sign: parse provenance envelope %s: %w: %w", in.PredicateEnvelopePath, err, errs.ErrMalformedInput)
	}

	if len(envelope.Predicate) == 0 || string(envelope.Predicate) == "null" {
		return nil, fmt.Errorf("container ledger sign: provenance envelope %s lacks predicate: %w", in.PredicateEnvelopePath, errs.ErrMalformedInput)
	}

	return envelope.Predicate, nil
}

func ledgerSignConstraintsFromInput(in SignLedgerImagesInput) (ledgerSignConstraints, error) {
	constraints := ledgerSignConstraints{
		expectedImageRepository: in.ExpectedImageRepository,
		expectedBaseRepository:  in.ExpectedBaseRepository,
		sbomPathPattern:         in.SBOMPathPattern,
	}

	if in.SBOMPathPattern != "" {
		compiled, err := regexp.Compile(in.SBOMPathPattern)
		if err != nil {
			return constraints, fmt.Errorf("container ledger sign: invalid SBOM path pattern %q: %w", in.SBOMPathPattern, err)
		}

		constraints.sbomPathRE = compiled
	}

	return constraints, nil
}

func validateLedgerSignEntry(entry imageledger.Entry, releaseTag string, constraints ledgerSignConstraints) error {
	if err := entry.Validate(releaseTag); err != nil {
		return err
	}

	if err := validateLedgerSignRepositories(entry, constraints); err != nil {
		return err
	}

	if entry.Role == "" {
		return fmt.Errorf("imageledger: role is required for signing: %w", errs.ErrValidation)
	}

	if entry.SBOM == "" {
		return fmt.Errorf("imageledger: sbom is required for signing: %w", errs.ErrValidation)
	}

	if err := validateLedgerSBOMPath(entry.SBOM, true); err != nil {
		return err
	}

	if constraints.sbomPathRE != nil && !constraints.sbomPathRE.MatchString(entry.SBOM) {
		return fmt.Errorf("imageledger: sbom %q must match %q: %w", entry.SBOM, constraints.sbomPathPattern, errs.ErrValidation)
	}

	if !strings.HasSuffix(entry.Ref, "@"+entry.Digest) {
		return fmt.Errorf("imageledger: ref/digest mismatch: ref %q digest %q: %w", entry.Ref, entry.Digest, errs.ErrValidation)
	}

	return validateLedgerSignBase(entry, constraints)
}

//nolint:cyclop // missing outputs are allowed only during preflight; ancestry, type and size checks remain explicit.
func validateLedgerSBOMPath(path string, allowMissing bool) error {
	if !pathsafe.Relative(path) {
		return fmt.Errorf("ledger SBOM must stay within the workspace: %w", errs.ErrValidation)
	}

	root, err := pathsafe.OpenRoot(filepath.Dir(path))
	if allowMissing && errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	defer func() { _ = root.Close() }()

	info, err := root.Lstat(filepath.Base(path))
	if allowMissing && errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil || !info.Mode().IsRegular() || (!allowMissing && info.Size() == 0) {
		return fmt.Errorf("ledger SBOM must be a nonempty regular file without symlinks: %w", errs.ErrValidation)
	}

	return nil
}

// validateLedgerSignRepositories pins every ref-bearing ledger field to the
// expected image/base repositories.
func validateLedgerSignRepositories(entry imageledger.Entry, constraints ledgerSignConstraints) error {
	if err := imageledger.ValidateRefUnderRepository("ref", entry.Ref, constraints.expectedImageRepository); err != nil {
		return err
	}

	if err := imageledger.ValidateRefUnderRepository("final_tag", entry.FinalTag, constraints.expectedImageRepository); err != nil {
		return err
	}

	if err := imageledger.ValidateRefUnderRepository("moving_tag", entry.MovingTag, constraints.expectedImageRepository); err != nil {
		return err
	}

	if err := imageledger.ValidateRefUnderRepository("candidate_tag", entry.CandidateTag, constraints.expectedImageRepository); err != nil {
		return err
	}

	return imageledger.ValidateRefUnderRepository("base_ref", entry.BaseRef, constraints.expectedBaseRepository)
}

// validateLedgerSignBase checks the paired base_ref/base_input_id fields.
//
// Base entries (image_kind=base) are exempt from the pairing: a base
// image has no parent base image to reference, and its base_input_id
// identifies the input set that PRODUCED it. A base entry must therefore
// declare base_input_id alone; enrichImagePredicate turns it into the
// attested externalParameters.base_input_id lineage field.
func validateLedgerSignBase(entry imageledger.Entry, constraints ledgerSignConstraints) error {
	if entry.ImageKind == imageledger.ImageKindBase {
		done, err := validateBaseKindEntry(entry, constraints)
		if err != nil || done {
			return err
		}
	}

	if (entry.BaseRef == "") != (entry.BaseInputID == "") {
		return fmt.Errorf("imageledger: base_ref and base_input_id must be set together: %w", errs.ErrValidation)
	}

	if entry.BaseRef != "" {
		if !domaincontainer.ValidDigestReference(entry.BaseRef) {
			return fmt.Errorf("imageledger: base_ref must be a registry path pinned by @sha256:<64 hex>: %q: %w", entry.BaseRef, errs.ErrValidation)
		}

		if !domaincontainer.ValidSHA256Hex(entry.BaseInputID) {
			return fmt.Errorf("imageledger: base_input_id must be a sha256 hex digest: %q: %w", entry.BaseInputID, errs.ErrValidation)
		}
	}

	return nil
}

// validateBaseKindEntry checks the rules that apply only to a base image, whose
// base_input_id identifies the inputs that produced it rather than a parent it
// was built from. It reports done when the entry carries no base_ref, which for
// a base image is complete rather than missing: the pairing rule below is about
// entries that reference a parent.
func validateBaseKindEntry(entry imageledger.Entry, constraints ledgerSignConstraints) (bool, error) {
	if !domaincontainer.ValidSHA256Hex(entry.BaseInputID) {
		return false, fmt.Errorf("imageledger: base entries must declare base_input_id as a sha256 hex digest: %q: %w", entry.BaseInputID, errs.ErrValidation)
	}

	if constraints.expectedImageRepository == "" {
		return false, fmt.Errorf("imageledger: expected image repository is required when signing a base entry: %w", errs.ErrMissingInput)
	}

	if entry.Flavor == "" {
		return false, fmt.Errorf("imageledger: base entries must declare flavor: %w", errs.ErrValidation)
	}

	wantFinalTag := fmt.Sprintf("%s:%s-%s", constraints.expectedImageRepository, entry.BaseInputID, entry.Flavor)
	if entry.FinalTag != wantFinalTag {
		return false, fmt.Errorf("imageledger: base final_tag must be %q for its base_input_id and flavor, got %q: %w", wantFinalTag, entry.FinalTag, errs.ErrValidation)
	}

	wantMovingTag := constraints.expectedImageRepository + ":" + entry.Flavor
	if entry.MovingTag != "" && entry.MovingTag != wantMovingTag {
		return false, fmt.Errorf("imageledger: base moving_tag must be %q for its flavor, got %q: %w", wantMovingTag, entry.MovingTag, errs.ErrValidation)
	}

	return entry.BaseRef == "", nil
}

func resolveLedgerSignRef(ctx context.Context, resolver imageDigestResolver, entry imageledger.Entry) (string, string, error) {
	if entry.CandidateTag != "" {
		if got, err := resolver.ResolveDigest(ctx, entry.CandidateTag); err == nil && got == entry.Digest {
			return entry.CandidateTag + "@" + entry.Digest, entry.CandidateTag, nil
		}
	}

	digestRef := domaincontainer.StripTagOrDigest(entry.Ref) + "@" + entry.Digest

	got, err := resolver.ResolveDigest(ctx, digestRef)
	if err != nil {
		return "", "", fmt.Errorf("unable to resolve image source for digest %s (candidate %q, digest ref %q): %w", entry.Digest, entry.CandidateTag, digestRef, err)
	}

	if got != entry.Digest {
		return "", "", fmt.Errorf("digest ref %s resolves to %s, want %s: %w", digestRef, got, entry.Digest, errs.ErrValidation)
	}

	return digestRef, "", nil
}

// ensureImageSBOM makes the entry's SBOM ready for attestation. A pinned
// entry (sbom_sha256 set) carries a PREMADE document: verify the file
// hashes to exactly the pin and attest it as-is — regenerating would
// break the pin by construction, and the signer must attest exactly what
// the build produced. Unpinned entries keep the generate-fresh behavior.
func ensureImageSBOM(ctx context.Context, sbom ImageEvidenceSyft, stderr io.Writer, imageRef string, entry imageledger.Entry) error {
	if entry.SBOMSHA256 == "" {
		return generateImageSBOM(ctx, sbom, stderr, imageRef, entry.SBOM)
	}

	root, err := pathsafe.OpenRoot(filepath.Dir(entry.SBOM))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	raw, err := root.ReadFile(filepath.Base(entry.SBOM))
	if err != nil {
		return fmt.Errorf("premade SBOM %s: %w", entry.SBOM, err)
	}

	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != entry.SBOMSHA256 {
		return fmt.Errorf("premade SBOM %s does not match sbom_sha256: got %s, want %s: %w",
			entry.SBOM, got, entry.SBOMSHA256, errs.ErrValidation)
	}

	return nil
}

func generateImageSBOM(ctx context.Context, sbom ImageEvidenceSyft, stderr io.Writer, imageRef, sbomPath string) error {
	if err := os.MkdirAll(filepath.Dir(sbomPath), 0o755); err != nil { //nolint:gosec,mnd // release dist dir.
		return fmt.Errorf("create SBOM dir for %s: %w", sbomPath, err)
	}

	return sbom.Generate(ctx, imageRef, map[string]string{"cyclonedx-json": sbomPath}, stderr)
}

func writeImagePredicate(dir string, idx int, base []byte, entry imageledger.Entry, imageRef, candidateTag string) (string, error) {
	body, err := enrichImagePredicate(base, entry, imageRef, candidateTag)
	if err != nil {
		return "", err
	}

	path := filepath.Join(dir, fmt.Sprintf("image-%d.predicate.json", idx))
	if err := os.WriteFile(path, body, 0o600); err != nil { //nolint:gosec // temp predicate contains release metadata, not secrets.
		return "", fmt.Errorf("write image predicate %s: %w", path, err)
	}

	return path, nil
}

func enrichImagePredicate(base []byte, entry imageledger.Entry, imageRef, candidateTag string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(base, &doc); err != nil {
		return nil, fmt.Errorf("parse provenance predicate: %w", err)
	}

	build, ok := doc["buildDefinition"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("provenance predicate buildDefinition must be an object: %w", errs.ErrMalformedInput)
	}

	ext, ok := build["externalParameters"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("provenance predicate buildDefinition.externalParameters must be an object: %w", errs.ErrMalformedInput)
	}

	imageDigestHex := strings.TrimPrefix(entry.Digest, "sha256:")
	ext["image"] = map[string]any{
		"ref":           imageRef,
		"digest":        map[string]any{"sha256": imageDigestHex}, //nolint:goconst // SLSA predicate JSON field name, not the output-sink key.
		"digest_ref":    entry.Digest,
		"final_tag":     entry.FinalTag,
		"moving_tag":    entry.MovingTag,
		"candidate_tag": candidateTag,
		"role":          entry.Role,
		"flavor":        entry.Flavor,
		"sbom":          entry.SBOM,
	}

	if entry.ImageKind == imageledger.ImageKindBase && entry.Flavor != "" {
		ext["flavor"] = entry.Flavor
	}

	if err := enrichPredicateBaseLineage(build, ext, entry); err != nil {
		return nil, err
	}

	// Caller-declared extras merge last, with every already-present key
	// reserved: the engine's computed facts and the base predicate's own
	// fields can never be shadowed by a declared document. One shared
	// merge implementation (domain/provenance) serves this path and the
	// statement builder alike.
	if err := provenance.MergeExternalParameters(ext, entry.Provenance); err != nil {
		return nil, err
	}

	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal image predicate: %w", err)
	}

	return append(body, '\n'), nil
}

// enrichPredicateBaseLineage records the entry's base lineage before the
// extras merge, so every emitted key is reserved against declared extras.
//
// A base entry (image_kind=base) with no base_ref IS the base image: its
// base_input_id identifies the input set that produced it and is emitted
// under the exact field name the retired base-lineage predicate used
// (externalParameters.base_input_id), keeping the attested content
// semantically identical across the signer flip. Every other entry with a
// base_ref records the parent base image it was built FROM, as both an
// externalParameters.base object and a resolvedDependencies pin.
func enrichPredicateBaseLineage(build, ext map[string]any, entry imageledger.Entry) error {
	if entry.ImageKind == imageledger.ImageKindBase && entry.BaseRef == "" {
		if entry.BaseInputID != "" {
			ext["base_input_id"] = entry.BaseInputID
		}

		return nil
	}

	if entry.BaseRef == "" {
		return nil
	}

	baseDigest := strings.TrimPrefix(entry.BaseRef[strings.LastIndex(entry.BaseRef, "@")+1:], "sha256:")
	ext["base"] = map[string]any{"ref": entry.BaseRef, "input_id": entry.BaseInputID}

	deps, ok := build["resolvedDependencies"].([]any)
	if !ok {
		return fmt.Errorf("provenance predicate buildDefinition.resolvedDependencies must be an array: %w", errs.ErrMalformedInput)
	}

	deps = append(deps, map[string]any{
		"uri":         "oci://" + entry.BaseRef,
		"digest":      map[string]any{"sha256": baseDigest},
		"annotations": map[string]any{"base_input_id": entry.BaseInputID},
	})
	build["resolvedDependencies"] = deps

	return nil
}
