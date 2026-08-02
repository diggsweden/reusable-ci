// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

const (
	slsaProvenanceV1PredicateType = "https://slsa.dev/provenance/v1"

	// baseInputIDField selects the base_input_id column in BaseInputField.
	baseInputIDField = "base-input-id"

	archAMD64 = "amd64"
	archARM64 = "arm64"
)

var (
	baseImageFlavorRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	baseImageDigestRefRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)+@sha256:[0-9a-f]{64}$`)
	baseImageTagRE       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)+:[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
)

// BaseImageMetadata is the JSON shape exchanged by forgejo-ci's base-image
// workflows. Candidate fields are populated only for newly built images waiting
// to be promoted to their immutable final tag.
type BaseImageMetadata struct {
	Flavor       string `json:"flavor"`
	Tag          string `json:"tag"`
	Ref          string `json:"ref"`
	CandidateTag string `json:"candidate_tag,omitempty"`
	CandidateRef string `json:"candidate_ref,omitempty"`
	BaseInputID  string `json:"base_input_id"`
	SBOMSHA256   string `json:"sbom_sha256,omitempty"`
}

// BaseInput maps a flavor to the sha256 content ID that produced that base.
type BaseInput struct {
	Flavor      string `json:"flavor"`
	ContentID   string `json:"content_id,omitempty"`
	BaseInputID string `json:"base_input_id"`
}

// BaseInputFieldInput drives BaseInputField.
type BaseInputFieldInput struct {
	Inputs []BaseInput
	Flavor string
	Field  string
}

// BaseArchMetadata is the per-architecture base-image evidence JSON consumed by
// downstream manifest assembly jobs.
type BaseArchMetadata struct {
	Flavor      string `json:"flavor"`
	Arch        string `json:"arch"`
	Tag         string `json:"tag"`
	ArchTag     string `json:"arch_tag"`
	ArchRef     string `json:"arch_ref"`
	BaseInputID string `json:"base_input_id"`
	ContentID   string `json:"content_id"`
}

// BaseArchMetadataInput drives BaseArchMetadataJSON.
type BaseArchMetadataInput struct {
	Flavor      string
	Arch        string
	Repository  string
	Tag         string
	ArchTag     string
	ArchDigest  string
	ArchRef     string
	BaseInputID string
	ContentID   string
}

// BaseArchRefInput drives BaseArchRef.
type BaseArchRefInput struct {
	Metadata   BaseArchMetadata
	Flavor     string
	Arch       string
	ContentID  string
	Repository string
}

// BaseCandidateMetadata is the assembled multi-arch base-image evidence JSON
// consumed by downstream signing and promotion jobs.
type BaseCandidateMetadata struct {
	Flavor       string `json:"flavor"`
	Tag          string `json:"tag"`
	Ref          string `json:"ref"`
	CandidateTag string `json:"candidate_tag"`
	CandidateRef string `json:"candidate_ref"`
	BaseInputID  string `json:"base_input_id"`
	ContentID    string `json:"content_id"`
	SBOMSHA256   string `json:"sbom_sha256,omitempty"`
}

// BaseCandidateMetadataInput drives BaseCandidateMetadataJSON.
type BaseCandidateMetadataInput struct {
	Flavor       string
	Repository   string
	Tag          string
	Ref          string
	CandidateTag string
	CandidateRef string
	BaseInputID  string
	ContentID    string
	SBOMSHA256   string
}

// BaseImageCollectImage is the fan-in JSON shape exchanged by Nanolinter's
// base-image collect jobs. Verified/reused images carry empty candidate fields;
// newly built images carry candidate fields and may carry content/SBOM evidence.
type BaseImageCollectImage struct {
	Flavor       string `json:"flavor"`
	Tag          string `json:"tag"`
	Ref          string `json:"ref"`
	CandidateTag string `json:"candidate_tag"`
	CandidateRef string `json:"candidate_ref"`
	BaseInputID  string `json:"base_input_id"`
	ContentID    string `json:"content_id,omitempty"`
	SBOMSHA256   string `json:"sbom_sha256,omitempty"`
}

// BaseImageCollectInput drives CollectBaseImages.
type BaseImageCollectInput struct {
	AlreadyVerified          bool
	VerifiedImages           []BaseImageCollectImage
	BuiltImages              []BaseImageCollectImage
	MissingFlavors           []string
	IncludeSigningSBOMSHA256 bool
	EmitDecision             bool
	BaseInputSetID           string
	BaseInputs               []BaseInput
	SourceSHA                string
}

// BaseImageCollectResult is the compact JSON fan-in emitted by CollectBaseImages.
type BaseImageCollectResult struct {
	ImagesJSON            string
	AllImagesJSON         string
	UnresolvedFlavorsJSON string
	DecisionJSON          string
}

// BaseImageVerifyExistingInput drives VerifyExistingBaseImages.
type BaseImageVerifyExistingInput struct {
	Flavors            []string
	BaseInputs         []BaseInput
	BaseInputID        string
	ExpectedRepository string
	ExpectedSource     string
	ExpectedWorkflow   string
	CosignPublicKey    string
}

// BaseImageVerifyExistingResult reports which final base tags exist and verify.
type BaseImageVerifyExistingResult struct {
	AllFound           bool
	AllImagesJSON      string
	MissingFlavorsJSON string
}

// BaseImagePromoteInput drives PromoteBaseImages.
type BaseImagePromoteInput struct {
	Images             []BaseImageMetadata
	BaseInputID        string
	ExpectedRepository string
	ExpectedSource     string
	ExpectedWorkflow   string
	CosignPublicKey    string
}

// BaseImagePromoteResult is the compact JSON output of PromoteBaseImages.
type BaseImagePromoteResult struct {
	BaseInputID      string
	BaseImagesJSON   string
	BaseInputIDsJSON string
}

// BaseImageCleanupStagingInput drives CleanupStagingBaseImages.
type BaseImageCleanupStagingInput struct {
	Images             []BaseImageMetadata
	BaseInputs         []BaseInput
	ExpectedRepository string
}

// BaseArchMetadataJSON validates and renders the per-architecture base-image
// evidence JSON uploaded by each arch build job.
func BaseArchMetadataJSON(in BaseArchMetadataInput) (string, error) {
	metadata, err := normalizeBaseArchMetadata(in)
	if err != nil {
		return "", err
	}

	body, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return "", fmt.Errorf("base images arch metadata: marshal JSON: %w", err)
	}

	return string(body) + "\n", nil
}

// BaseInputField prints one field (base-input-id or content-id) for a flavor
// from a validated base-inputs JSON document.
func BaseInputField(in BaseInputFieldInput) (string, error) {
	flavor := strings.TrimSpace(in.Flavor)
	if flavor == "" || !baseImageFlavorRE.MatchString(flavor) {
		return "", fmt.Errorf("base images input field: invalid flavor: %s: %w", flavor, errs.ErrValidation)
	}

	field := strings.TrimSpace(in.Field)
	if field != baseInputIDField && field != "content-id" {
		return "", fmt.Errorf("base images input field: unsupported field: %s: %w", field, errs.ErrUsage)
	}

	for idx, input := range in.Inputs {
		if err := validateBaseInputFieldEntry(idx, input); err != nil {
			return "", err
		}

		if input.Flavor != flavor {
			continue
		}

		if field == baseInputIDField {
			return input.BaseInputID, nil
		}

		return input.ContentID, nil
	}

	return "", fmt.Errorf("base images input field: flavor not found: %s: %w", flavor, errs.ErrValidation)
}

func validateBaseInputFieldEntry(idx int, input BaseInput) error {
	if input.Flavor == "" || !baseImageFlavorRE.MatchString(input.Flavor) || !hex64RE.MatchString(input.BaseInputID) || !hex64RE.MatchString(input.ContentID) {
		return fmt.Errorf("base images input field: base-inputs-json entry %d must include flavor, sha256 content_id, and sha256 base_input_id: %w", idx, errs.ErrValidation)
	}

	return nil
}

// BaseArchRef cross-checks per-arch metadata against the expected flavor, arch,
// and content ID and returns the validated digest-pinned arch ref.
func BaseArchRef(in BaseArchRefInput) (string, error) {
	metadata := in.Metadata
	if err := validateBaseArchRefExpectations(metadata, in); err != nil {
		return "", err
	}

	expectedRepository := strings.TrimSpace(in.Repository)
	if expectedRepository == "" {
		expectedRepository = domaincontainer.StripTagOrDigest(metadata.ArchRef)
	}

	if err := validateBaseImageRef(metadata.ArchRef, expectedRepository); err != nil {
		return "", err
	}

	return metadata.ArchRef, nil
}

// validateBaseArchRefExpectations cross-checks the metadata against the
// caller-expected flavor, arch, and content ID.
func validateBaseArchRefExpectations(metadata BaseArchMetadata, in BaseArchRefInput) error {
	flavor := strings.TrimSpace(in.Flavor)
	if flavor != "" && (!baseImageFlavorRE.MatchString(flavor) || metadata.Flavor != flavor) {
		return fmt.Errorf("base images arch ref: metadata flavor mismatch, expected %s actual %s: %w", flavor, metadata.Flavor, errs.ErrValidation)
	}

	arch := strings.TrimSpace(in.Arch)
	if arch == "" || !baseImageFlavorRE.MatchString(arch) || metadata.Arch != arch {
		return fmt.Errorf("base images arch ref: metadata arch mismatch, expected %s actual %s: %w", arch, metadata.Arch, errs.ErrValidation)
	}

	contentID := strings.TrimSpace(in.ContentID)
	if !hex64RE.MatchString(contentID) || metadata.ContentID != contentID {
		return fmt.Errorf("base images arch ref: metadata content_id mismatch, expected %s actual %s: %w", contentID, metadata.ContentID, errs.ErrValidation)
	}

	return nil
}

// BaseCandidateMetadataJSON validates and renders the assembled multi-arch
// base-image candidate evidence JSON.
func BaseCandidateMetadataJSON(in BaseCandidateMetadataInput) (string, error) {
	metadata, err := normalizeBaseCandidateMetadata(in)
	if err != nil {
		return "", err
	}

	body, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return "", fmt.Errorf("base images candidate metadata: marshal JSON: %w", err)
	}

	return string(body) + "\n", nil
}

// CollectBaseImages merges verified and freshly built base images into the
// deterministic fan-in JSON outputs consumed by downstream signing jobs.
func CollectBaseImages(in BaseImageCollectInput) (BaseImageCollectResult, error) {
	verified, err := normalizeCollectImages("verified images", in.VerifiedImages)
	if err != nil {
		return BaseImageCollectResult{}, err
	}

	built, err := normalizeCollectImages("built images", in.BuiltImages)
	if err != nil {
		return BaseImageCollectResult{}, err
	}

	missing, err := normalizeCollectFlavors(in.MissingFlavors)
	if err != nil {
		return BaseImageCollectResult{}, err
	}

	allImages, unresolved := mergeCollectImages(in.AlreadyVerified, verified, built, missing)

	if dupErr := rejectDuplicateCollectFlavors(allImages); dupErr != nil {
		return BaseImageCollectResult{}, dupErr
	}

	allImagesJSON, err := compactJSON(allImages)
	if err != nil {
		return BaseImageCollectResult{}, err
	}

	signingImagesJSON, err := compactJSON(collectSigningImages(allImages, in.IncludeSigningSBOMSHA256))
	if err != nil {
		return BaseImageCollectResult{}, err
	}

	unresolvedJSON, err := compactJSON(unresolved)
	if err != nil {
		return BaseImageCollectResult{}, err
	}

	result := BaseImageCollectResult{ImagesJSON: signingImagesJSON, AllImagesJSON: allImagesJSON, UnresolvedFlavorsJSON: unresolvedJSON}

	if in.EmitDecision {
		decisionJSON, decisionErr := collectDecisionJSON(in, allImages, missing, unresolved)
		if decisionErr != nil {
			return BaseImageCollectResult{}, decisionErr
		}

		result.DecisionJSON = decisionJSON
	}

	return result, nil
}

// mergeCollectImages merges verified and built images (built images are
// skipped entirely when the input set was already verified) and reports the
// missing flavors that no built image resolved.
func mergeCollectImages(alreadyVerified bool, verified, built []BaseImageCollectImage, missing []string) ([]BaseImageCollectImage, []string) {
	allImages := make([]BaseImageCollectImage, 0, len(verified)+len(built))
	unresolved := make([]string, 0)

	if alreadyVerified {
		allImages = append(allImages, verified...)
	} else {
		allImages = append(allImages, verified...)
		allImages = append(allImages, built...)

		builtFlavors := make(map[string]bool, len(built))
		for _, image := range built {
			builtFlavors[image.Flavor] = true
		}

		for _, flavor := range missing {
			if !builtFlavors[flavor] {
				unresolved = append(unresolved, flavor)
			}
		}
	}

	slices.SortFunc(allImages, func(a, b BaseImageCollectImage) int { return strings.Compare(a.Flavor, b.Flavor) })

	return allImages, unresolved
}

type baseImageRegistry interface {
	ResolveDigest(ctx context.Context, ref string) (string, error)
	CopyTag(ctx context.Context, source, dest string) error
}

// imageDigestResolver is the registry surface shared by base-image cleanup
// and ledger signing: resolve a tag or ref to its manifest digest.
type imageDigestResolver interface {
	ResolveDigest(ctx context.Context, ref string) (string, error)
}

type baseImageStagingCleaner interface {
	DeleteTag(ctx context.Context, ref string) error
	ListContainerPackageVersions(ctx context.Context, owner, name string) ([]string, error)
}

// imageEvidenceVerifier is the cosign surface shared by base-image and
// release-image verification.
type imageEvidenceVerifier interface {
	VerifyImage(ctx context.Context, in cosign.VerifyImageInput, errOut io.Writer) error
	VerifyAttestation(ctx context.Context, in cosign.VerifyAttestationInput, errOut io.Writer) error
	VerifyAttestationOutput(ctx context.Context, in cosign.VerifyAttestationInput, out, errOut io.Writer) error
}

type verifiedBaseImage struct {
	Flavor       string `json:"flavor"`
	Tag          string `json:"tag"`
	Ref          string `json:"ref"`
	CandidateTag string `json:"candidate_tag"`
	CandidateRef string `json:"candidate_ref"`
	BaseInputID  string `json:"base_input_id"`
}

type promotedBaseImage struct {
	Flavor      string `json:"flavor"`
	Tag         string `json:"tag"`
	Ref         string `json:"ref"`
	BaseInputID string `json:"base_input_id"`
}

type baseImageRefOutput struct {
	Flavor      string `json:"flavor"`
	Ref         string `json:"ref"`
	BaseInputID string `json:"base_input_id"`
}

type baseImageInputOutput struct {
	Flavor      string `json:"flavor"`
	BaseInputID string `json:"base_input_id"`
}

type baseImageLineageExpectation struct {
	Source      string
	Workflow    string
	Flavor      string
	BaseInputID string
}

// VerifyExistingBaseImages checks the immutable final base-image tags for the
// requested flavors. Missing tags are reported as cache misses, matching the
// previous workflow behavior; a tag that exists must verify with signature, SBOM
// attestation, and SLSA lineage before it is returned.
func VerifyExistingBaseImages(ctx context.Context, resolver baseImageRegistry, verifier imageEvidenceVerifier, out io.Writer, in BaseImageVerifyExistingInput) (BaseImageVerifyExistingResult, error) {
	if err := validateVerifyExistingBaseInput(resolver, verifier, in); err != nil {
		return BaseImageVerifyExistingResult{}, err
	}

	baseInputs, err := baseInputIDByFlavor(in.BaseInputs)
	if err != nil {
		return BaseImageVerifyExistingResult{}, err
	}

	allFound := true
	existing := make([]verifiedBaseImage, 0, len(in.Flavors))
	missing := make([]string, 0)

	for _, flavor := range in.Flavors {
		baseInputID, idErr := existingBaseInputID(baseInputs, flavor, in.BaseInputID)
		if idErr != nil {
			return BaseImageVerifyExistingResult{}, idErr
		}

		image, found, verifyErr := verifyOneExistingBaseImage(ctx, resolver, verifier, out, in, flavor, baseInputID)
		if verifyErr != nil {
			return BaseImageVerifyExistingResult{}, verifyErr
		}

		if !found {
			allFound = false

			missing = append(missing, flavor)

			continue
		}

		existing = append(existing, image)
	}

	allImagesJSON, err := marshalSortedBaseImages(existing)
	if err != nil {
		return BaseImageVerifyExistingResult{}, err
	}

	slices.Sort(missing)

	missingJSON, err := compactJSON(missing)
	if err != nil {
		return BaseImageVerifyExistingResult{}, err
	}

	return BaseImageVerifyExistingResult{AllFound: allFound, AllImagesJSON: allImagesJSON, MissingFlavorsJSON: missingJSON}, nil
}

func validateVerifyExistingBaseInput(resolver baseImageRegistry, verifier imageEvidenceVerifier, in BaseImageVerifyExistingInput) error {
	if resolver == nil {
		return fmt.Errorf("base images verify: registry resolver is required: %w", errs.ErrUsage)
	}

	if verifier == nil {
		return fmt.Errorf("base images verify: cosign verifier is required: %w", errs.ErrUsage)
	}

	if err := validateBaseImageCommon(in.ExpectedRepository, in.ExpectedSource, in.ExpectedWorkflow, in.BaseInputID, in.CosignPublicKey); err != nil {
		return err
	}

	if len(in.Flavors) == 0 {
		return fmt.Errorf("base images verify: flavors file contains no flavors: %w", errs.ErrValidation)
	}

	return nil
}

// existingBaseInputID picks the per-flavor base input ID, falling back to the
// shared one, and validates both the flavor and the ID.
func existingBaseInputID(baseInputs map[string]string, flavor, fallback string) (string, error) {
	if !baseImageFlavorRE.MatchString(flavor) {
		return "", fmt.Errorf("base images verify: invalid flavor name: %s: %w", flavor, errs.ErrValidation)
	}

	baseInputID := baseInputs[flavor]
	if baseInputID == "" {
		baseInputID = fallback
	}

	if !hex64RE.MatchString(baseInputID) {
		return "", fmt.Errorf("base images verify: missing or invalid base input ID for flavor: %s: %w", flavor, errs.ErrValidation)
	}

	return baseInputID, nil
}

// verifyOneExistingBaseImage resolves one flavor's final tag and verifies its
// evidence. A missing tag is a cache miss (found=false), not an error.
func verifyOneExistingBaseImage(ctx context.Context, resolver baseImageRegistry, verifier imageEvidenceVerifier, out io.Writer, in BaseImageVerifyExistingInput, flavor, baseInputID string) (verifiedBaseImage, bool, error) {
	tag := fmt.Sprintf("%s:%s-%s", in.ExpectedRepository, baseInputID, flavor)

	digest, err := resolver.ResolveDigest(ctx, tag)
	if err != nil {
		_, _ = fmt.Fprintf(out, "Base image missing and will be built if needed: %s\n", tag)

		return verifiedBaseImage{}, false, nil //nolint:nilerr // missing final tag is a cache miss (found=false), not an error.
	}

	ref := in.ExpectedRepository + "@" + digest
	if err := verifyBaseImageEvidence(ctx, verifier, out, ref, in.CosignPublicKey, baseImageLineageExpectation{
		Source: in.ExpectedSource, Workflow: in.ExpectedWorkflow, Flavor: flavor, BaseInputID: baseInputID,
	}, 1); err != nil {
		return verifiedBaseImage{}, false, fmt.Errorf("base images verify: final base tag exists but does not have the expected signature and attestation: %s: %w", tag, err)
	}

	_, _ = fmt.Fprintf(out, "Base image already exists and verifies: %s\n", ref)

	return verifiedBaseImage{Flavor: flavor, Tag: tag, Ref: ref, CandidateTag: "", CandidateRef: "", BaseInputID: baseInputID}, true, nil
}

// PromoteBaseImages verifies candidate/base evidence, promotes missing final
// immutable tags, and emits the compact JSON outputs consumed by downstream base
// image jobs.
func PromoteBaseImages(ctx context.Context, registry baseImageRegistry, verifier imageEvidenceVerifier, out io.Writer, in BaseImagePromoteInput) (BaseImagePromoteResult, error) {
	if registry == nil {
		return BaseImagePromoteResult{}, fmt.Errorf("base images promote: registry is required: %w", errs.ErrUsage)
	}

	if verifier == nil {
		return BaseImagePromoteResult{}, fmt.Errorf("base images promote: cosign verifier is required: %w", errs.ErrUsage)
	}

	if err := validateBaseImageCommon(in.ExpectedRepository, in.ExpectedSource, in.ExpectedWorkflow, in.BaseInputID, in.CosignPublicKey); err != nil {
		return BaseImagePromoteResult{}, err
	}

	if len(in.Images) == 0 {
		return BaseImagePromoteResult{}, fmt.Errorf("base images promote: all-images-json must contain at least one image: %w", errs.ErrValidation)
	}

	promoted := make([]promotedBaseImage, 0, len(in.Images))
	for idx, image := range in.Images {
		item, err := normalizePromoteBaseImage(image, in.BaseInputID, in.ExpectedRepository)
		if err != nil {
			return BaseImagePromoteResult{}, fmt.Errorf("base images promote: entry %d: %w", idx, err)
		}

		ref, err := promoteOneBaseImage(ctx, registry, verifier, out, in, item)
		if err != nil {
			return BaseImagePromoteResult{}, err
		}

		promoted = append(promoted, promotedBaseImage{Flavor: item.Flavor, Tag: item.Tag, Ref: ref, BaseInputID: item.BaseInputID})
	}

	return baseImagePromotionResult(promoted)
}

// promoteOneBaseImage promotes one normalized base image (verifying its
// candidate evidence and copying the final tag when needed) and returns the
// digest-pinned ref recorded for downstream jobs.
func promoteOneBaseImage(ctx context.Context, registry baseImageRegistry, verifier imageEvidenceVerifier, out io.Writer, in BaseImagePromoteInput, item BaseImageMetadata) (string, error) {
	ref := item.Ref
	if item.CandidateRef != "" {
		if err := promoteCandidateBaseImage(ctx, registry, verifier, out, in, item); err != nil {
			return "", err
		}

		ref = item.CandidateRef
	} else if err := verifyBaseTagDigest(ctx, registry, item.Tag, digestFromRef(item.Ref), in.ExpectedRepository); err != nil {
		return "", err
	}

	_, _ = fmt.Fprintf(out, "Verifying promoted base image evidence: flavor=%s digest=%s\n", item.Flavor, digestFromRef(ref))
	if err := verifyBaseImageEvidence(ctx, verifier, out, ref, in.CosignPublicKey, baseImageLineageExpectation{
		Source: in.ExpectedSource, Workflow: in.ExpectedWorkflow, Flavor: item.Flavor, BaseInputID: item.BaseInputID,
	}, 3); err != nil {
		return "", err
	}

	return ref, nil
}

// promoteCandidateBaseImage verifies the candidate's evidence and tag digest,
// then ensures the immutable final tag exists and points at the candidate.
func promoteCandidateBaseImage(ctx context.Context, registry baseImageRegistry, verifier imageEvidenceVerifier, out io.Writer, in BaseImagePromoteInput, item BaseImageMetadata) error {
	digest := digestFromRef(item.CandidateRef)

	_, _ = fmt.Fprintf(out, "Verifying candidate base image before promotion: flavor=%s digest=%s\n", item.Flavor, digest)
	if err := verifyBaseImageEvidence(ctx, verifier, out, item.CandidateRef, in.CosignPublicKey, baseImageLineageExpectation{
		Source: in.ExpectedSource, Workflow: in.ExpectedWorkflow, Flavor: item.Flavor, BaseInputID: item.BaseInputID,
	}, 3); err != nil {
		return err
	}

	if item.CandidateTag != "" {
		if err := verifyBaseTagDigest(ctx, registry, item.CandidateTag, digest, in.ExpectedRepository); err != nil {
			return err
		}
	}

	return ensureFinalBaseTag(ctx, registry, out, item, digest)
}

// ensureFinalBaseTag copies the candidate to the immutable final tag when the
// final tag is absent, and rejects promotion when an existing final tag (or
// the tag just copied) does not resolve to the candidate digest.
func ensureFinalBaseTag(ctx context.Context, registry baseImageRegistry, out io.Writer, item BaseImageMetadata, digest string) error {
	finalDigest, err := registry.ResolveDigest(ctx, item.Tag)
	if err == nil {
		if finalDigest != digest {
			return fmt.Errorf("base images promote: immutable final base tag already exists with a different digest\n  tag:       %s\n  existing:  %s\n  candidate: %s: %w", item.Tag, finalDigest, digest, errs.ErrValidation)
		}

		return nil
	}

	_, _ = fmt.Fprintf(out, "Promoting base image: flavor=%s digest=%s final=%s\n", item.Flavor, digest, item.Tag)
	if copyErr := registry.CopyTag(ctx, item.CandidateRef, item.Tag); copyErr != nil {
		return copyErr
	}

	finalDigest, err = registry.ResolveDigest(ctx, item.Tag)
	if err != nil {
		return err
	}

	if finalDigest != digest {
		return fmt.Errorf("base images promote: final base digest changed during promotion: %w", errs.ErrValidation)
	}

	return nil
}

// CleanupStagingBaseImages deletes promoted staging base tags through the forge
// package-version API, verifies final tags before and after deletion, and sweeps
// stale staging versions that are still present in the package registry.
func CleanupStagingBaseImages(ctx context.Context, registry imageDigestResolver, cleaner baseImageStagingCleaner, out io.Writer, in BaseImageCleanupStagingInput) error {
	if registry == nil {
		return fmt.Errorf("base images cleanup: registry resolver is required: %w", errs.ErrUsage)
	}

	if cleaner == nil {
		return fmt.Errorf("base images cleanup: staging tag cleaner is required: %w", errs.ErrUsage)
	}

	if strings.TrimSpace(in.ExpectedRepository) == "" {
		return fmt.Errorf("base images cleanup: expected repository is required: %w", errs.ErrUsage)
	}

	archFinalTags, err := reusableBaseArchFinalTags(in.ExpectedRepository, in.BaseInputs)
	if err != nil {
		return err
	}

	cleanupError, err := cleanupPromotedStagingImages(ctx, registry, cleaner, out, in, archFinalTags)
	if err != nil {
		return err
	}

	if err := sweepStaleStagingVersions(ctx, registry, cleaner, out, in.ExpectedRepository, archFinalTags); err != nil {
		return err
	}

	if cleanupError {
		return fmt.Errorf("base images cleanup: staging cleanup safety check failed: %w", errs.ErrValidation)
	}

	return nil
}

// cleanupPromotedStagingImages deletes each promoted image's staging tag,
// reporting (but not aborting on) failed final-tag safety checks.
func cleanupPromotedStagingImages(ctx context.Context, registry imageDigestResolver, cleaner baseImageStagingCleaner, out io.Writer, in BaseImageCleanupStagingInput, archFinalTags map[string]string) (bool, error) {
	cleanupError := false

	for idx, image := range in.Images {
		item, digest, skip, err := normalizeCleanupStagingImage(image, in.ExpectedRepository, idx)
		if err != nil {
			return false, err
		}

		if skip {
			continue
		}

		flagged, err := cleanupOnePromotedStagingImage(ctx, registry, cleaner, out, in.ExpectedRepository, archFinalTags, item, digest)
		if err != nil {
			return false, err
		}

		if flagged {
			cleanupError = true
		}
	}

	return cleanupError, nil
}

// normalizeCleanupStagingImage validates one cleanup entry and returns the
// promoted digest it must keep pointing at. skip=true means the entry has no
// staging candidate to clean up.
func normalizeCleanupStagingImage(image BaseImageMetadata, expectedRepository string, idx int) (BaseImageMetadata, string, bool, error) {
	item, err := normalizePromoteBaseImage(image, "", expectedRepository)
	if err != nil {
		return item, "", false, fmt.Errorf("base images cleanup: entry %d: %w", idx, err)
	}

	if item.CandidateTag == "" {
		return item, "", true, nil
	}

	digest := digestFromRef(item.CandidateRef)
	if digest == "" {
		digest = digestFromRef(item.Ref)
	}

	if !domaincontainer.ValidDigest(digest) {
		return item, "", false, fmt.Errorf("base images cleanup: candidate ref lacks a valid digest for %s: %w", item.Flavor, errs.ErrValidation)
	}

	if !strings.HasPrefix(item.CandidateTag, expectedRepository+":staging-"+item.BaseInputID+"-") {
		return item, "", false, fmt.Errorf("base images cleanup: refusing to delete unexpected staging tag: %s: %w", item.CandidateTag, errs.ErrValidation)
	}

	if !strings.HasPrefix(item.Tag, expectedRepository+":"+item.BaseInputID+"-") {
		return item, "", false, fmt.Errorf("base images cleanup: refusing cleanup with unexpected final tag: %s: %w", item.Tag, errs.ErrValidation)
	}

	return item, digest, false, nil
}

// cleanupOnePromotedStagingImage verifies the final tag before and after
// deleting one staging tag. It returns true when a safety check failed and
// the overall cleanup must be reported as failed.
func cleanupOnePromotedStagingImage(ctx context.Context, registry imageDigestResolver, cleaner baseImageStagingCleaner, out io.Writer, expectedRepository string, archFinalTags map[string]string, item BaseImageMetadata, digest string) (bool, error) {
	finalDigest, err := registry.ResolveDigest(ctx, item.Tag)
	if err != nil {
		_, _ = fmt.Fprintf(out, "Final base tag is absent before staging cleanup; stale sweep will remove the candidate if it still exists: %s\n", item.Tag)

		return false, nil //nolint:nilerr // absent final tag defers cleanup to the stale sweep; not a failure.
	}

	if finalDigest != digest {
		_, _ = fmt.Fprintln(out, "ERROR: final base tag does not point to the promoted digest before cleanup")
		_, _ = fmt.Fprintf(out, "  tag:      %s\n", item.Tag)
		_, _ = fmt.Fprintf(out, "  expected: %s\n", digest)
		_, _ = fmt.Fprintf(out, "  actual:   %s\n", finalDigest)

		return true, nil
	}

	stagingVersion := tagName(item.CandidateTag)
	if deleteErr := deleteStagingBaseVersion(ctx, registry, cleaner, out, expectedRepository, archFinalTags, stagingVersion, item.CandidateTag); deleteErr != nil {
		return false, deleteErr
	}

	finalDigestAfter, err := registry.ResolveDigest(ctx, item.Tag)
	if err != nil {
		_, _ = fmt.Fprintln(out, "ERROR: final base tag disappeared after staging cleanup")
		_, _ = fmt.Fprintf(out, "  tag: %s\n", item.Tag)

		return true, nil //nolint:nilerr // reported as a failed safety check (true), not an error, so other flavors still clean up.
	}

	if finalDigestAfter != digest {
		_, _ = fmt.Fprintln(out, "ERROR: final base tag changed after staging cleanup")
		_, _ = fmt.Fprintf(out, "  tag:      %s\n", item.Tag)
		_, _ = fmt.Fprintf(out, "  expected: %s\n", digest)
		_, _ = fmt.Fprintf(out, "  actual:   %s\n", finalDigestAfter)

		return true, nil
	}

	return false, nil
}

// sweepStaleStagingVersions deletes staging package versions that are still
// present in the registry after promotion.
func sweepStaleStagingVersions(ctx context.Context, registry imageDigestResolver, cleaner baseImageStagingCleaner, out io.Writer, expectedRepository string, archFinalTags map[string]string) error {
	owner, name, err := baseImagePackageOwnerName(expectedRepository)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(out, "Checking for stale staging base tags.")

	staleVersions, err := cleaner.ListContainerPackageVersions(ctx, owner, name)
	if err != nil {
		return err
	}

	slices.Sort(staleVersions)

	for _, version := range staleVersions {
		if !strings.HasPrefix(version, "staging-") {
			continue
		}

		if err := deleteStagingBaseVersion(ctx, registry, cleaner, out, expectedRepository, archFinalTags, version, expectedRepository+":"+version); err != nil {
			return err
		}
	}

	return nil
}

func reusableBaseArchFinalTags(expectedRepository string, inputs []BaseInput) (map[string]string, error) {
	finalTags := make(map[string]string, len(inputs)*2)
	for idx, input := range inputs {
		if input.Flavor == "" || !hex64RE.MatchString(input.BaseInputID) || !hex64RE.MatchString(input.ContentID) {
			return nil, fmt.Errorf("base images cleanup: base-inputs-json entry %d must include flavor, sha256 content_id, and sha256 base_input_id: %w", idx, errs.ErrValidation)
		}

		if !baseImageFlavorRE.MatchString(input.Flavor) {
			return nil, fmt.Errorf("base images cleanup: invalid flavor name in base-inputs-json: %s: %w", input.Flavor, errs.ErrValidation)
		}

		for _, arch := range []string{archAMD64, archARM64} {
			version := fmt.Sprintf("staging-%s-%s-%s", input.ContentID, input.Flavor, arch)
			finalTags[version] = fmt.Sprintf("%s:%s-%s", expectedRepository, input.BaseInputID, input.Flavor)
		}
	}

	return finalTags, nil
}

func deleteStagingBaseVersion(ctx context.Context, registry imageDigestResolver, cleaner baseImageStagingCleaner, out io.Writer, expectedRepository string, archFinalTags map[string]string, stagingVersion, stagingLabel string) error {
	if isBaseArchStagingVersion(stagingVersion) && !shouldDeleteArchStagingVersion(ctx, registry, out, archFinalTags, stagingVersion, stagingLabel) {
		return nil
	}

	if !validBaseStagingVersion(stagingVersion) {
		return fmt.Errorf("base images cleanup: refusing to delete unexpected staging version: %s: %w", stagingVersion, errs.ErrValidation)
	}

	stagingTag := expectedRepository + ":" + stagingVersion

	stagingDigest, _ := registry.ResolveDigest(ctx, stagingTag)
	if err := cleaner.DeleteTag(ctx, stagingTag); err != nil {
		if errors.Is(err, errs.ErrPermissionDenied) {
			_, _ = fmt.Fprintf(out, "Registry refused staging base tag deletion; preserving: %s\n", stagingLabel)

			return cleanupOrphanBaseSignatureVersions(ctx, registry, out, expectedRepository, stagingVersion, stagingDigest)
		}

		return fmt.Errorf("base images cleanup: failed to delete staging base tag: %s: %w", stagingLabel, err)
	}

	_, _ = fmt.Fprintf(out, "Deleted staging base tag: %s\n", stagingLabel)

	return cleanupOrphanBaseSignatureVersions(ctx, registry, out, expectedRepository, stagingVersion, stagingDigest)
}

// shouldDeleteArchStagingVersion decides whether a per-arch staging tag may
// be deleted: only after its expected final tag resolves, or when no final
// tag is expected for it at all (a stale leftover).
func shouldDeleteArchStagingVersion(ctx context.Context, registry imageDigestResolver, out io.Writer, archFinalTags map[string]string, stagingVersion, stagingLabel string) bool {
	finalTag := archFinalTags[stagingVersion]
	if finalTag == "" {
		_, _ = fmt.Fprintf(out, "Deleting stale architecture base tag: %s\n", stagingLabel)

		return true
	}

	if _, err := registry.ResolveDigest(ctx, finalTag); err != nil {
		_, _ = fmt.Fprintf(out, "Preserving reusable architecture base tag until final exists: %s\n", stagingLabel)

		return false
	}

	_, _ = fmt.Fprintf(out, "Deleting reusable architecture base tag after final promotion: %s\n", stagingLabel)

	return true
}

func cleanupOrphanBaseSignatureVersions(ctx context.Context, registry imageDigestResolver, out io.Writer, expectedRepository, stagingVersion, digest string) error {
	if digest == "" || isBaseArchStagingVersion(stagingVersion) {
		return nil
	}

	finalTag := expectedRepository + ":" + strings.TrimPrefix(stagingVersion, "staging-")
	if finalDigest, err := registry.ResolveDigest(ctx, finalTag); err == nil && finalDigest == digest {
		_, _ = fmt.Fprintf(out, "Preserving base signature artifacts for promoted digest: %s\n", digest)

		return nil
	}

	_, _ = fmt.Fprintf(out, "Preserving base signature artifacts because promoted final tag is not currently readable as this digest: %s\n", finalTag)

	return nil
}

func validBaseStagingVersion(version string) bool {
	if !strings.HasPrefix(version, "staging-") {
		return false
	}

	parts := strings.SplitN(strings.TrimPrefix(version, "staging-"), "-", 2)

	return len(parts) == 2 && hex64RE.MatchString(parts[0]) && baseImageFlavorRE.MatchString(parts[1])
}

func isBaseArchStagingVersion(version string) bool {
	return strings.HasSuffix(version, "-amd64") || strings.HasSuffix(version, "-arm64")
}

func tagName(ref string) string {
	slash := strings.LastIndex(ref, "/")

	colon := strings.LastIndex(ref, ":")
	if colon <= slash {
		return ""
	}

	return ref[colon+1:]
}

func baseImagePackageOwnerName(repository string) (string, string, error) {
	parts := strings.Split(repository, "/")
	if len(parts) < 3 || parts[1] == "" || parts[2] == "" || strings.Contains(repository, "@") {
		return "", "", fmt.Errorf("base images cleanup: expected repository must be <host>/<owner>/<name>: %s: %w", repository, errs.ErrUsage)
	}

	return parts[1], strings.Join(parts[2:], "/"), nil
}

func validateBaseImageCommon(expectedRepository, expectedSource, expectedWorkflow, baseInputID, publicKey string) error {
	if expectedRepository == "" {
		return fmt.Errorf("base images: expected repository is required: %w", errs.ErrMissingInput)
	}

	if expectedSource == "" {
		return fmt.Errorf("base images: expected source is required: %w", errs.ErrMissingInput)
	}

	if expectedWorkflow == "" {
		return fmt.Errorf("base images: expected workflow is required: %w", errs.ErrMissingInput)
	}

	if publicKey == "" {
		return fmt.Errorf("base images: Cosign public key path is required: %w", errs.ErrMissingInput)
	}

	if baseInputID != "" && !hex64RE.MatchString(baseInputID) {
		return fmt.Errorf("base images: base-input-id must be a sha256 hex digest: %w", errs.ErrValidation)
	}

	return nil
}

func normalizeCollectImages(kind string, images []BaseImageCollectImage) ([]BaseImageCollectImage, error) {
	normalized := make([]BaseImageCollectImage, 0, len(images))
	for idx, image := range images {
		image = BaseImageCollectImage{
			Flavor:       strings.TrimSpace(image.Flavor),
			Tag:          strings.TrimSpace(image.Tag),
			Ref:          strings.TrimSpace(image.Ref),
			CandidateTag: strings.TrimSpace(image.CandidateTag),
			CandidateRef: strings.TrimSpace(image.CandidateRef),
			BaseInputID:  strings.TrimSpace(image.BaseInputID),
			ContentID:    strings.TrimSpace(image.ContentID),
			SBOMSHA256:   strings.TrimSpace(image.SBOMSHA256),
		}
		if err := validateCollectImageIDs(kind, idx, image); err != nil {
			return nil, err
		}

		if err := validateCollectImageRefs(kind, idx, image); err != nil {
			return nil, err
		}

		normalized = append(normalized, image)
	}

	slices.SortFunc(normalized, func(a, b BaseImageCollectImage) int { return strings.Compare(a.Flavor, b.Flavor) })

	if err := rejectDuplicateCollectFlavors(normalized); err != nil {
		return nil, err
	}

	return normalized, nil
}

// validateCollectImageIDs checks the flavor and hex-digest identity fields of
// one collect entry.
func validateCollectImageIDs(kind string, idx int, image BaseImageCollectImage) error {
	if image.Flavor == "" || !baseImageFlavorRE.MatchString(image.Flavor) {
		return fmt.Errorf("base images collect: %s entry %d has invalid flavor: %s: %w", kind, idx, image.Flavor, errs.ErrValidation)
	}

	if !hex64RE.MatchString(image.BaseInputID) {
		return fmt.Errorf("base images collect: %s entry %d base_input_id must be a sha256 hex digest: %w", kind, idx, errs.ErrValidation)
	}

	if image.ContentID != "" && !hex64RE.MatchString(image.ContentID) {
		return fmt.Errorf("base images collect: %s entry %d content_id must be a sha256 hex digest: %w", kind, idx, errs.ErrValidation)
	}

	if image.SBOMSHA256 != "" && !hex64RE.MatchString(image.SBOMSHA256) {
		return fmt.Errorf("base images collect: %s entry %d sbom_sha256 must be a sha256 hex digest: %w", kind, idx, errs.ErrValidation)
	}

	return nil
}

// validateCollectImageRefs checks the tag/ref shapes of one collect entry,
// including the candidate pair when present.
func validateCollectImageRefs(kind string, idx int, image BaseImageCollectImage) error {
	if err := validateBaseImageTag(image.Tag, domaincontainer.StripTagOrDigest(image.Tag)); err != nil {
		return fmt.Errorf("base images collect: %s entry %d: %w", kind, idx, err)
	}

	if err := validateBaseImageRef(image.Ref, domaincontainer.StripTagOrDigest(image.Ref)); err != nil {
		return fmt.Errorf("base images collect: %s entry %d: %w", kind, idx, err)
	}

	if (image.CandidateTag == "") != (image.CandidateRef == "") {
		return fmt.Errorf("base images collect: %s entry %d must set candidate_tag and candidate_ref together: %w", kind, idx, errs.ErrValidation)
	}

	if image.CandidateTag != "" {
		if err := validateBaseImageTag(image.CandidateTag, domaincontainer.StripTagOrDigest(image.CandidateTag)); err != nil {
			return fmt.Errorf("base images collect: %s entry %d: %w", kind, idx, err)
		}

		if err := validateBaseImageRef(image.CandidateRef, domaincontainer.StripTagOrDigest(image.CandidateRef)); err != nil {
			return fmt.Errorf("base images collect: %s entry %d: %w", kind, idx, err)
		}
	}

	return nil
}

func normalizeCollectFlavors(flavors []string) ([]string, error) {
	normalized := make([]string, 0, len(flavors))
	seen := map[string]bool{}

	for _, flavor := range flavors {
		flavor = strings.TrimSpace(flavor)
		if flavor == "" {
			continue
		}

		if !baseImageFlavorRE.MatchString(flavor) {
			return nil, fmt.Errorf("base images collect: invalid missing flavor: %s: %w", flavor, errs.ErrValidation)
		}

		if !seen[flavor] {
			seen[flavor] = true
			normalized = append(normalized, flavor)
		}
	}

	return normalized, nil
}

func rejectDuplicateCollectFlavors(images []BaseImageCollectImage) error {
	seen := map[string]bool{}
	for _, image := range images {
		if seen[image.Flavor] {
			return fmt.Errorf("base images collect: duplicate image flavor: %s: %w", image.Flavor, errs.ErrValidation)
		}

		seen[image.Flavor] = true
	}

	return nil
}

type baseImageSigningRef struct {
	Flavor      string `json:"flavor"`
	Ref         string `json:"ref"`
	Tag         string `json:"tag"`
	BaseInputID string `json:"base_input_id"`
}

type baseImageSigningRefWithSBOM struct {
	Flavor      string `json:"flavor"`
	Ref         string `json:"ref"`
	Tag         string `json:"tag"`
	BaseInputID string `json:"base_input_id"`
	SBOMSHA256  string `json:"sbom_sha256"`
}

func collectSigningImages(images []BaseImageCollectImage, includeSBOM bool) any {
	if includeSBOM {
		signing := make([]baseImageSigningRefWithSBOM, 0)

		for _, image := range images {
			if image.CandidateRef == "" {
				continue
			}

			signing = append(signing, baseImageSigningRefWithSBOM{Flavor: image.Flavor, Ref: image.CandidateRef, Tag: image.CandidateTag, BaseInputID: image.BaseInputID, SBOMSHA256: image.SBOMSHA256})
		}

		return signing
	}

	signing := make([]baseImageSigningRef, 0)

	for _, image := range images {
		if image.CandidateRef == "" {
			continue
		}

		signing = append(signing, baseImageSigningRef{Flavor: image.Flavor, Ref: image.CandidateRef, Tag: image.CandidateTag, BaseInputID: image.BaseInputID})
	}

	return signing
}

type baseImageCollectDecision struct {
	BaseInputSetID   string                 `json:"base_input_set_id"`
	BaseInputs       []BaseInput            `json:"base_inputs"`
	SourceSHA        string                 `json:"source_sha"`
	AllFound         bool                   `json:"all_found"`
	MissingFlavors   []string               `json:"missing_flavors"`
	UnresolvedFlavor []string               `json:"unresolved_flavors"`
	ReusedRefs       []baseImagePromotedRef `json:"reused_refs"`
	BuiltRefs        []baseImagePromotedRef `json:"built_refs"`
	PromotedRefs     []baseImagePromotedRef `json:"promoted_refs"`
}

type baseImagePromotedRef struct {
	Flavor      string `json:"flavor"`
	Tag         string `json:"tag"`
	Ref         string `json:"ref"`
	BaseInputID string `json:"base_input_id"`
}

func collectDecisionJSON(in BaseImageCollectInput, allImages []BaseImageCollectImage, missing, unresolved []string) (string, error) {
	baseInputSetID := strings.TrimSpace(in.BaseInputSetID)
	if !hex64RE.MatchString(baseInputSetID) {
		return "", fmt.Errorf("base images collect: base-input-set-id must be a sha256 hex digest: %w", errs.ErrValidation)
	}

	sourceSHA := strings.TrimSpace(in.SourceSHA)
	if !baseImagesSourceSHARE.MatchString(sourceSHA) {
		return "", fmt.Errorf("base images collect: source-sha must be a git commit hex digest: %w", errs.ErrValidation)
	}

	baseInputs := make([]BaseInput, 0, len(in.BaseInputs))
	for idx, input := range in.BaseInputs {
		input = BaseInput{Flavor: strings.TrimSpace(input.Flavor), ContentID: strings.TrimSpace(input.ContentID), BaseInputID: strings.TrimSpace(input.BaseInputID)}
		if input.Flavor == "" || !baseImageFlavorRE.MatchString(input.Flavor) || !hex64RE.MatchString(input.ContentID) || !hex64RE.MatchString(input.BaseInputID) {
			return "", fmt.Errorf("base images collect: base-inputs-json entry %d must include flavor, sha256 content_id, and sha256 base_input_id: %w", idx, errs.ErrValidation)
		}

		baseInputs = append(baseInputs, input)
	}

	reused := make([]baseImagePromotedRef, 0)
	built := make([]baseImagePromotedRef, 0)

	promoted := make([]baseImagePromotedRef, 0, len(allImages))
	for _, image := range allImages {
		promoted = append(promoted, baseImagePromotedRef{Flavor: image.Flavor, Tag: image.Tag, Ref: image.Ref, BaseInputID: image.BaseInputID})
		if image.CandidateRef == "" {
			reused = append(reused, baseImagePromotedRef{Flavor: image.Flavor, Tag: image.Tag, Ref: image.Ref, BaseInputID: image.BaseInputID})

			continue
		}

		built = append(built, baseImagePromotedRef{Flavor: image.Flavor, Tag: image.CandidateTag, Ref: image.CandidateRef, BaseInputID: image.BaseInputID})
	}

	return compactJSON(baseImageCollectDecision{
		BaseInputSetID:   baseInputSetID,
		BaseInputs:       baseInputs,
		SourceSHA:        sourceSHA,
		AllFound:         in.AlreadyVerified,
		MissingFlavors:   missing,
		UnresolvedFlavor: unresolved,
		ReusedRefs:       reused,
		BuiltRefs:        built,
		PromotedRefs:     promoted,
	})
}

func normalizeBaseArchMetadata(in BaseArchMetadataInput) (BaseArchMetadata, error) {
	metadata := BaseArchMetadata{
		Flavor:      strings.TrimSpace(in.Flavor),
		Arch:        strings.TrimSpace(in.Arch),
		Tag:         strings.TrimSpace(in.Tag),
		ArchTag:     strings.TrimSpace(in.ArchTag),
		ArchRef:     strings.TrimSpace(in.ArchRef),
		BaseInputID: strings.TrimSpace(in.BaseInputID),
		ContentID:   strings.TrimSpace(in.ContentID),
	}

	expectedRepository := strings.TrimSpace(in.Repository)
	if expectedRepository == "" && metadata.Tag != "" {
		expectedRepository = domaincontainer.StripTagOrDigest(metadata.Tag)
	}

	if expectedRepository == "" {
		return BaseArchMetadata{}, fmt.Errorf("base images arch metadata: --repository or --tag is required: %w", errs.ErrUsage)
	}

	archRef, err := resolveBaseArchRef(metadata.ArchRef, in.ArchDigest, expectedRepository)
	if err != nil {
		return BaseArchMetadata{}, err
	}

	metadata.ArchRef = archRef

	if err := validateBaseArchIdentity(metadata); err != nil {
		return BaseArchMetadata{}, err
	}

	if err := validateBaseImageTag(metadata.Tag, expectedRepository); err != nil {
		return BaseArchMetadata{}, err
	}

	if err := validateBaseImageTag(metadata.ArchTag, expectedRepository); err != nil {
		return BaseArchMetadata{}, err
	}

	if err := validateBaseImageRef(metadata.ArchRef, expectedRepository); err != nil {
		return BaseArchMetadata{}, err
	}

	return metadata, nil
}

// resolveBaseArchRef returns the explicit arch ref, or builds one from the
// arch digest when only the digest was supplied.
func resolveBaseArchRef(archRef, archDigest, expectedRepository string) (string, error) {
	if archRef != "" {
		return archRef, nil
	}

	digest := strings.TrimSpace(archDigest)
	if digest == "" {
		return "", fmt.Errorf("base images arch metadata: --arch-digest or --arch-ref is required: %w", errs.ErrUsage)
	}

	if !strings.HasPrefix(digest, "sha256:") || !hex64RE.MatchString(strings.TrimPrefix(digest, "sha256:")) {
		return "", fmt.Errorf("base images arch metadata: arch digest must be sha256:<64 lowercase hex>: %w", errs.ErrValidation)
	}

	return expectedRepository + "@" + digest, nil
}

// validateBaseArchIdentity checks the flavor, arch, and hex-digest identity
// fields of per-arch metadata.
func validateBaseArchIdentity(metadata BaseArchMetadata) error {
	if metadata.Flavor == "" || !baseImageFlavorRE.MatchString(metadata.Flavor) {
		return fmt.Errorf("base images arch metadata: invalid flavor: %s: %w", metadata.Flavor, errs.ErrValidation)
	}

	if metadata.Arch == "" || !baseImageFlavorRE.MatchString(metadata.Arch) {
		return fmt.Errorf("base images arch metadata: invalid architecture: %s: %w", metadata.Arch, errs.ErrValidation)
	}

	if !hex64RE.MatchString(metadata.BaseInputID) {
		return fmt.Errorf("base images arch metadata: base-input-id must be a sha256 hex digest: %w", errs.ErrValidation)
	}

	if !hex64RE.MatchString(metadata.ContentID) {
		return fmt.Errorf("base images arch metadata: content-id must be a sha256 hex digest: %w", errs.ErrValidation)
	}

	return nil
}

func normalizeBaseCandidateMetadata(in BaseCandidateMetadataInput) (BaseCandidateMetadata, error) {
	metadata := BaseCandidateMetadata{
		Flavor:       strings.TrimSpace(in.Flavor),
		Tag:          strings.TrimSpace(in.Tag),
		Ref:          strings.TrimSpace(in.Ref),
		CandidateTag: strings.TrimSpace(in.CandidateTag),
		CandidateRef: strings.TrimSpace(in.CandidateRef),
		BaseInputID:  strings.TrimSpace(in.BaseInputID),
		ContentID:    strings.TrimSpace(in.ContentID),
		SBOMSHA256:   strings.TrimSpace(in.SBOMSHA256),
	}
	if metadata.CandidateRef == "" {
		metadata.CandidateRef = metadata.Ref
	}

	expectedRepository := strings.TrimSpace(in.Repository)
	if expectedRepository == "" && metadata.Tag != "" {
		expectedRepository = domaincontainer.StripTagOrDigest(metadata.Tag)
	}

	if expectedRepository == "" {
		return BaseCandidateMetadata{}, fmt.Errorf("base images candidate metadata: --repository or --tag is required: %w", errs.ErrUsage)
	}

	if err := validateBaseCandidateIdentity(metadata); err != nil {
		return BaseCandidateMetadata{}, err
	}

	if err := validateBaseCandidateRefs(metadata, expectedRepository); err != nil {
		return BaseCandidateMetadata{}, err
	}

	return metadata, nil
}

// validateBaseCandidateIdentity checks the flavor and hex-digest identity
// fields of the assembled candidate metadata.
func validateBaseCandidateIdentity(metadata BaseCandidateMetadata) error {
	if metadata.Flavor == "" || !baseImageFlavorRE.MatchString(metadata.Flavor) {
		return fmt.Errorf("base images candidate metadata: invalid flavor: %s: %w", metadata.Flavor, errs.ErrValidation)
	}

	if !hex64RE.MatchString(metadata.BaseInputID) {
		return fmt.Errorf("base images candidate metadata: base-input-id must be a sha256 hex digest: %w", errs.ErrValidation)
	}

	if !hex64RE.MatchString(metadata.ContentID) {
		return fmt.Errorf("base images candidate metadata: content-id must be a sha256 hex digest: %w", errs.ErrValidation)
	}

	if metadata.SBOMSHA256 != "" && !hex64RE.MatchString(metadata.SBOMSHA256) {
		return fmt.Errorf("base images candidate metadata: sbom-sha256 must be a sha256 hex digest: %w", errs.ErrValidation)
	}

	return nil
}

// validateBaseCandidateRefs checks every tag/ref of the assembled candidate
// metadata against the expected repository.
func validateBaseCandidateRefs(metadata BaseCandidateMetadata, expectedRepository string) error {
	if err := validateBaseImageTag(metadata.Tag, expectedRepository); err != nil {
		return err
	}

	if err := validateBaseImageRef(metadata.Ref, expectedRepository); err != nil {
		return err
	}

	if err := validateBaseImageTag(metadata.CandidateTag, expectedRepository); err != nil {
		return err
	}

	return validateBaseImageRef(metadata.CandidateRef, expectedRepository)
}

func baseInputIDByFlavor(inputs []BaseInput) (map[string]string, error) {
	byFlavor := make(map[string]string, len(inputs))
	for idx, input := range inputs {
		if input.Flavor == "" || !hex64RE.MatchString(input.BaseInputID) {
			return nil, fmt.Errorf("base images verify: base-inputs-json entry %d must include flavor and sha256 base_input_id: %w", idx, errs.ErrValidation)
		}

		if _, exists := byFlavor[input.Flavor]; !exists {
			byFlavor[input.Flavor] = input.BaseInputID
		}
	}

	return byFlavor, nil
}

func normalizePromoteBaseImage(image BaseImageMetadata, fallbackBaseInputID, expectedRepository string) (BaseImageMetadata, error) {
	if image.BaseInputID == "" {
		image.BaseInputID = fallbackBaseInputID
	}

	if image.Flavor == "" || image.Tag == "" || image.Ref == "" {
		return image, fmt.Errorf("base image metadata must include flavor, tag, and ref: %w", errs.ErrValidation)
	}

	if !baseImageFlavorRE.MatchString(image.Flavor) {
		return image, fmt.Errorf("invalid flavor name: %s: %w", image.Flavor, errs.ErrValidation)
	}

	if err := validatePromoteBaseInputID(image, fallbackBaseInputID); err != nil {
		return image, err
	}

	if err := validateBaseImageTag(image.Tag, expectedRepository); err != nil {
		return image, err
	}

	if err := validateBaseImageRef(image.Ref, expectedRepository); err != nil {
		return image, err
	}

	return image, validatePromoteCandidateFields(image, expectedRepository)
}

// validatePromoteBaseInputID checks the metadata's base_input_id shape and,
// when a fallback/expected ID is supplied, that it matches.
func validatePromoteBaseInputID(image BaseImageMetadata, fallbackBaseInputID string) error {
	if !hex64RE.MatchString(image.BaseInputID) {
		return fmt.Errorf("base image metadata has missing or invalid base_input_id\n  flavor: %s: %w", image.Flavor, errs.ErrValidation)
	}

	if fallbackBaseInputID != "" && image.BaseInputID != fallbackBaseInputID {
		return fmt.Errorf("base image metadata has unexpected base_input_id\n  flavor:   %s\n  expected: %s\n  actual:   %s: %w", image.Flavor, fallbackBaseInputID, image.BaseInputID, errs.ErrValidation)
	}

	return nil
}

// validatePromoteCandidateFields checks the optional candidate tag/ref pair.
func validatePromoteCandidateFields(image BaseImageMetadata, expectedRepository string) error {
	if image.CandidateRef != "" {
		if err := validateBaseImageRef(image.CandidateRef, expectedRepository); err != nil {
			return err
		}
	}

	if image.CandidateTag != "" {
		if err := validateBaseImageTag(image.CandidateTag, expectedRepository); err != nil {
			return err
		}
	}

	return nil
}

func validateBaseImageRef(ref, expectedRepository string) error {
	if !baseImageDigestRefRE.MatchString(ref) {
		return fmt.Errorf("base images: image ref must be digest-pinned: %s: %w", ref, errs.ErrValidation)
	}

	if got := domaincontainer.StripTagOrDigest(ref); got != expectedRepository {
		return fmt.Errorf("base images: image ref must be under %s: %s: %w", expectedRepository, ref, errs.ErrValidation)
	}

	return nil
}

func validateBaseImageTag(tag, expectedRepository string) error {
	if !baseImageTagRE.MatchString(tag) {
		return fmt.Errorf("base images: image tag must be a registry path with a tag: %s: %w", tag, errs.ErrValidation)
	}

	if got := domaincontainer.StripTagOrDigest(tag); got != expectedRepository {
		return fmt.Errorf("base images: image tag must be under %s: %s: %w", expectedRepository, tag, errs.ErrValidation)
	}

	return nil
}

func verifyBaseTagDigest(ctx context.Context, resolver baseImageRegistry, tag, digest, expectedRepository string) error {
	if err := validateBaseImageTag(tag, expectedRepository); err != nil {
		return err
	}

	actualDigest, err := resolver.ResolveDigest(ctx, tag)
	if err != nil {
		return err
	}

	if actualDigest != digest {
		return fmt.Errorf("base images: image tag does not point to expected digest\n  tag:      %s\n  expected: %s\n  actual:   %s: %w", tag, digest, actualDigest, errs.ErrValidation)
	}

	return nil
}

func verifyBaseImageEvidence(ctx context.Context, verifier imageEvidenceVerifier, out io.Writer, ref, publicKey string, expected baseImageLineageExpectation, attempts int) error {
	if attempts < 1 {
		attempts = 1
	}

	if err := validateBaseImageRef(ref, domaincontainer.StripTagOrDigest(ref)); err != nil {
		return err
	}

	digest := digestFromRef(ref)

	for attempt := 1; attempt <= attempts; attempt++ {
		verifyStep, cosignErr, err := verifyBaseImageEvidenceOnce(ctx, verifier, ref, publicKey, expected)
		if err == nil {
			_, _ = fmt.Fprintf(out, "Verified base image signature and lineage: flavor=%s digest=%s\n", expected.Flavor, digest)

			return nil
		}

		if attempt < attempts {
			_, _ = fmt.Fprintf(out, "Base image signature/attestation verification did not pass (attempt %d/%d); retrying.\n", attempt, attempts)
			printBaseImageVerificationError(out, expected.Flavor, digest, verifyStep, cosignErr)
			time.Sleep(time.Duration(attempt*15) * time.Second)
		} else {
			_, _ = fmt.Fprintln(out, "ERROR: failed to verify base image signature and lineage after retries")
			printBaseImageVerificationError(out, expected.Flavor, digest, verifyStep, cosignErr)

			return fmt.Errorf("base images: failed to verify base image signature and lineage: %w", errs.ErrValidation)
		}
	}

	return fmt.Errorf("base images: failed to verify base image signature and lineage: %w", errs.ErrValidation)
}

// verifyBaseImageEvidenceOnce runs one signature/SBOM/lineage verification
// pass. On failure it names the step that failed and returns the cosign
// stderr captured for that step.
func verifyBaseImageEvidenceOnce(ctx context.Context, verifier imageEvidenceVerifier, ref, publicKey string, expected baseImageLineageExpectation) (string, []byte, error) {
	var errBuf bytes.Buffer

	if err := verifier.VerifyImage(ctx, cosign.VerifyImageInput{ImageRef: ref, KeyRef: publicKey}, &errBuf); err != nil {
		return "signature", errBuf.Bytes(), err
	}

	errBuf.Reset()

	if err := verifier.VerifyAttestation(ctx, cosign.VerifyAttestationInput{ImageRef: ref, PredicateType: predicateTypeCycloneDX, KeyRef: publicKey}, &errBuf); err != nil {
		return "CycloneDX attestation", errBuf.Bytes(), err
	}

	errBuf.Reset()

	var lineage bytes.Buffer
	if err := verifier.VerifyAttestationOutput(ctx, cosign.VerifyAttestationInput{ImageRef: ref, PredicateType: predicateTypeSLSAProvenance1, KeyRef: publicKey}, &lineage, &errBuf); err != nil {
		return "SLSA provenance (lineage) attestation", errBuf.Bytes(), err
	}

	errBuf.Reset()

	if err := baseLineageAttestationMatches(lineage.Bytes(), expected); err != nil {
		_, _ = fmt.Fprintf(&errBuf, "%v\n", err)

		return "SLSA provenance (lineage) predicate", errBuf.Bytes(), err
	}

	return "", nil, nil
}

func printBaseImageVerificationError(out io.Writer, flavor, digest, verifyStep string, raw []byte) {
	_, _ = fmt.Fprintf(out, "  flavor: %s\n", flavor)
	_, _ = fmt.Fprintf(out, "  digest: %s\n", digest)
	_, _ = fmt.Fprintf(out, "  check:  %s\n", verifyStep)

	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || unsafeCosignErrorLine(line) {
			continue
		}

		_, _ = fmt.Fprintf(out, "  cosign: %s\n", line)
	}
}

func unsafeCosignErrorLine(line string) bool {
	lower := strings.ToLower(line)
	for _, marker := range []string{"authorization", "bearer", "token", "password", "secret"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	return false
}

func baseLineageAttestationMatches(body []byte, expected baseImageLineageExpectation) error {
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("parse SLSA provenance attestation output: %w: %w", err, errs.ErrMalformedInput)
	}

	envelopes, ok := raw.([]any)
	if !ok {
		envelopes = []any{raw}
	}

	for _, envelope := range envelopes {
		payload, ok := envelopePayload(envelope)
		if !ok {
			continue
		}

		statement, err := decodeBaseLineageStatement(payload)
		if err != nil {
			return err
		}

		if baseLineageStatementMatches(statement, expected) {
			return nil
		}
	}

	return fmt.Errorf("SLSA provenance attestation lacks expected base lineage fields: %w", errs.ErrValidation)
}

func envelopePayload(envelope any) (string, bool) {
	obj, ok := envelope.(map[string]any)
	if !ok {
		return "", false
	}

	payload, ok := obj["payload"].(string)

	return payload, ok && payload != ""
}

func decodeBaseLineageStatement(payload string) (map[string]any, error) {
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("decode SLSA provenance payload: %w: %w", err, errs.ErrMalformedInput)
	}

	var statement map[string]any
	if err := json.Unmarshal(decoded, &statement); err != nil {
		return nil, fmt.Errorf("parse SLSA provenance statement: %w: %w", err, errs.ErrMalformedInput)
	}

	return statement, nil
}

func baseLineageStatementMatches(statement map[string]any, expected baseImageLineageExpectation) bool {
	if statementString(statement, "predicateType") != slsaProvenanceV1PredicateType {
		return false
	}

	params, ok := nestedMap(statement, "predicate", "buildDefinition", "externalParameters")
	if !ok {
		return false
	}

	return statementString(params, "source") == expected.Source &&
		statementString(params, "workflow") == expected.Workflow &&
		statementString(params, "flavor") == expected.Flavor &&
		statementString(params, "base_input_id") == expected.BaseInputID
}

func nestedMap(root map[string]any, keys ...string) (map[string]any, bool) {
	current := root
	for _, key := range keys {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil, false
		}

		current = next
	}

	return current, true
}

func statementString(root map[string]any, key string) string {
	value, _ := root[key].(string)

	return value
}

func marshalSortedBaseImages(images []verifiedBaseImage) (string, error) {
	slices.SortFunc(images, func(a, b verifiedBaseImage) int { return strings.Compare(a.Flavor, b.Flavor) })

	return compactJSON(images)
}

func baseImagePromotionResult(promoted []promotedBaseImage) (BaseImagePromoteResult, error) {
	slices.SortFunc(promoted, func(a, b promotedBaseImage) int { return strings.Compare(a.Flavor, b.Flavor) })

	refs := make([]baseImageRefOutput, 0, len(promoted))
	inputs := make([]baseImageInputOutput, 0, len(promoted))

	uniqueIDs := make(map[string]bool, len(promoted))
	for _, image := range promoted {
		refs = append(refs, baseImageRefOutput{Flavor: image.Flavor, Ref: image.Ref, BaseInputID: image.BaseInputID})
		inputs = append(inputs, baseImageInputOutput{Flavor: image.Flavor, BaseInputID: image.BaseInputID})
		uniqueIDs[image.BaseInputID] = true
	}

	refsJSON, err := compactJSON(refs)
	if err != nil {
		return BaseImagePromoteResult{}, err
	}

	inputsJSON, err := compactJSON(inputs)
	if err != nil {
		return BaseImagePromoteResult{}, err
	}

	baseInputID := ""

	if len(uniqueIDs) == 1 {
		for id := range uniqueIDs {
			baseInputID = id
		}
	}

	return BaseImagePromoteResult{BaseInputID: baseInputID, BaseImagesJSON: refsJSON, BaseInputIDsJSON: inputsJSON}, nil
}

func compactJSON(v any) (string, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("base images: marshal JSON output: %w", err)
	}

	return string(body), nil
}

func digestFromRef(ref string) string {
	_, digest, _ := strings.Cut(ref, "@")

	return digest
}
