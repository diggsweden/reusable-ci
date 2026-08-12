// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

const ledgerSignDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type recordingImageSigner struct {
	signs   []cosign.SignImageInput
	attests []recordedAttestation
}

type recordedAttestation struct {
	input     cosign.AttestImageInput
	predicate []byte
}

func (r *recordingImageSigner) SignImage(_ context.Context, in cosign.SignImageInput, _ io.Writer) error {
	r.signs = append(r.signs, in)

	return nil
}

func (r *recordingImageSigner) AttestImage(_ context.Context, in cosign.AttestImageInput, _ io.Writer) error {
	body, err := os.ReadFile(in.PredicatePath) //nolint:gosec // test reads app-created predicate path.
	if err != nil {
		return err
	}

	r.attests = append(r.attests, recordedAttestation{input: in, predicate: body})

	return nil
}

type fakeLedgerSyft struct {
	targets []string
	outputs []map[string]string
}

func (f *fakeLedgerSyft) Generate(_ context.Context, target string, outputs map[string]string, _ io.Writer) error {
	f.targets = append(f.targets, target)

	recorded := map[string]string{}
	for format, path := range outputs {
		recorded[format] = path
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec,mnd // test temp dir.
			return err
		}

		if err := os.WriteFile(path, []byte(`{"bomFormat":"CycloneDX"}`), 0o600); err != nil { //nolint:gosec // test temp file.
			return err
		}
	}

	f.outputs = append(f.outputs, recorded)

	return nil
}

type fakeLedgerResolver map[string]string

func (f fakeLedgerResolver) ResolveDigest(_ context.Context, ref string) (string, error) {
	digest, ok := f[ref]
	if !ok {
		return "", errors.New("ref not found") //nolint:err113 // test fake.
	}

	return digest, nil
}

func ledgerSignEntry(t *testing.T) imageledger.Entry {
	t.Helper()

	return imageledger.Entry{
		Role:         "distroless",
		Flavor:       "rust",
		Ref:          "codeberg.org/itiquette/gommitlint@" + ledgerSignDigest,
		Digest:       ledgerSignDigest,
		SBOM:         "dist/image-sbom-rust.cyclonedx.json",
		FinalTag:     "codeberg.org/itiquette/gommitlint:v1.2.3-rust",
		MovingTag:    "codeberg.org/itiquette/gommitlint:rust",
		CandidateTag: "codeberg.org/itiquette/gommitlint:staging-v1.2.3-rust",
		BaseRef:      "codeberg.org/itiquette/gommitlint-base@" + ledgerSignDigest,
		BaseInputID:  strings.TrimPrefix(ledgerSignDigest, "sha256:"),
	}
}

func writeBasePredicate(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "slsa-provenance.predicate.json")

	body := []byte(`{
  "buildDefinition": {
    "buildType": "https://codeberg.org/itiquette/forgejo-ci/container-build/v1",
    "externalParameters": {"source": "git+https://codeberg.org/itiquette/gommitlint", "ref": "v1.2.3"},
    "internalParameters": {},
    "resolvedDependencies": []
  },
  "runDetails": {"builder": {"id": "builder"}, "metadata": {}}
}
`)
	if err := os.WriteFile(path, body, 0o600); err != nil { //nolint:gosec // test fixture.
		t.Fatal(err)
	}

	return path
}

func writeBaseEnvelope(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "slsa-provenance.intoto.json")

	body := []byte(`{
  "_type": "https://in-toto.io/Statement/v1",
  "subject": [],
  "predicateType": "https://slsa.dev/provenance/v1",
  "predicate": {
    "buildDefinition": {
      "buildType": "https://codeberg.org/itiquette/forgejo-ci/container-build/v1",
      "externalParameters": {"source": "git+https://codeberg.org/itiquette/gommitlint", "ref": "v1.2.3"},
      "internalParameters": {},
      "resolvedDependencies": []
    },
    "runDetails": {"builder": {"id": "builder"}, "metadata": {}}
  }
}
`)
	if err := os.WriteFile(path, body, 0o600); err != nil { //nolint:gosec // test fixture.
		t.Fatal(err)
	}

	return path
}

func asMap(t *testing.T, value any, label string) map[string]any {
	t.Helper()

	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s is not a map: %#v", label, value)
	}

	return m
}

func TestSignLedgerImages_SignsSBOMAndImagePredicate(t *testing.T) {
	t.Chdir(t.TempDir())

	entry := ledgerSignEntry(t)
	signer := &recordingImageSigner{}
	syft := &fakeLedgerSyft{}
	resolvedRef := entry.CandidateTag + "@" + entry.Digest

	err := appcontainer.SignLedgerImages(context.Background(), signer, syft, fakeLedgerResolver{entry.CandidateTag: entry.Digest}, &bytes.Buffer{}, io.Discard, appcontainer.SignLedgerImagesInput{
		Entries:                 []imageledger.Entry{entry},
		ReleaseTag:              "v1.2.3",
		PredicatePath:           writeBasePredicate(t),
		Method:                  domainrelease.SignMethodKMS,
		KeyRef:                  "env://COSIGN_KEY",
		ExpectedImageRepository: "codeberg.org/itiquette/gommitlint",
		ExpectedBaseRepository:  "codeberg.org/itiquette/gommitlint-base",
		SBOMPathPattern:         `^dist/image-sbom[-A-Za-z0-9_.]*\.cyclonedx\.json$`,
	})
	if err != nil {
		t.Fatalf("SignLedgerImages: %v", err)
	}

	if len(signer.signs) != 1 || signer.signs[0].ImageRef != resolvedRef {
		t.Fatalf("sign calls = %+v, want one sign of %s", signer.signs, resolvedRef)
	}

	if len(syft.targets) != 1 || syft.targets[0] != resolvedRef {
		t.Fatalf("syft targets = %v, want %s", syft.targets, resolvedRef)
	}

	if len(signer.attests) != 2 {
		t.Fatalf("attest calls = %d, want 2", len(signer.attests))
	}

	if signer.attests[0].input.PredicateType != "cyclonedx" || signer.attests[0].input.PredicatePath != entry.SBOM {
		t.Errorf("first attestation = %+v, want CycloneDX SBOM", signer.attests[0].input)
	}

	if signer.attests[1].input.PredicateType != "slsaprovenance1" {
		t.Errorf("second attestation type = %q", signer.attests[1].input.PredicateType)
	}

	var predicate map[string]any
	if err := json.Unmarshal(signer.attests[1].predicate, &predicate); err != nil {
		t.Fatalf("predicate JSON: %v\n%s", err, signer.attests[1].predicate)
	}

	build := asMap(t, predicate["buildDefinition"], "buildDefinition")
	ext := asMap(t, build["externalParameters"], "externalParameters")

	image := asMap(t, ext["image"], "image")
	if image["ref"] != resolvedRef || image["final_tag"] != entry.FinalTag || image["moving_tag"] != entry.MovingTag || image["candidate_tag"] != entry.CandidateTag || image["role"] != entry.Role || image["flavor"] != entry.Flavor || image["sbom"] != entry.SBOM {
		t.Errorf("image externalParameters mismatch: %#v", image)
	}

	if image["digest_ref"] != entry.Digest {
		t.Errorf("image.digest_ref = %#v", image["digest_ref"])
	}

	if asMap(t, ext["base"], "base")["ref"] != entry.BaseRef {
		t.Errorf("base externalParameters missing: %#v", ext["base"])
	}

	deps, ok := build["resolvedDependencies"].([]any)
	if !ok {
		t.Fatalf("resolvedDependencies is not a slice: %#v", build["resolvedDependencies"])
	}

	if len(deps) != 1 || asMap(t, deps[0], "resolvedDependencies[0]")["uri"] != "oci://"+entry.BaseRef {
		t.Errorf("base dependency mismatch: %#v", deps)
	}
}

func TestSignLedgerImages_ExtractsPredicateFromEnvelope(t *testing.T) {
	t.Chdir(t.TempDir())

	entry := ledgerSignEntry(t)
	signer := &recordingImageSigner{}

	err := appcontainer.SignLedgerImages(context.Background(), signer, &fakeLedgerSyft{}, fakeLedgerResolver{entry.CandidateTag: entry.Digest}, io.Discard, io.Discard, appcontainer.SignLedgerImagesInput{
		Entries:               []imageledger.Entry{entry},
		ReleaseTag:            "v1.2.3",
		PredicateEnvelopePath: writeBaseEnvelope(t),
		Method:                domainrelease.SignMethodKMS,
		KeyRef:                "env://COSIGN_KEY",
	})
	if err != nil {
		t.Fatalf("SignLedgerImages: %v", err)
	}

	if len(signer.attests) != 2 {
		t.Fatalf("attest calls = %d, want 2", len(signer.attests))
	}

	var predicate map[string]any
	if err := json.Unmarshal(signer.attests[1].predicate, &predicate); err != nil {
		t.Fatalf("predicate JSON: %v\n%s", err, signer.attests[1].predicate)
	}

	if predicate["_type"] != nil || predicate["predicate"] != nil {
		t.Fatalf("attested predicate still contains envelope fields: %#v", predicate)
	}

	if predicate["buildDefinition"] == nil || predicate["runDetails"] == nil {
		t.Fatalf("predicate missing SLSA fields: %#v", predicate)
	}
}

func TestSignLedgerImages_RejectsEnvelopeWithoutPredicate(t *testing.T) {
	t.Chdir(t.TempDir())

	path := filepath.Join(t.TempDir(), "empty.intoto.json")
	if err := os.WriteFile(path, []byte(`{"predicateType":"https://slsa.dev/provenance/v1"}`), 0o600); err != nil { //nolint:gosec // test fixture.
		t.Fatal(err)
	}

	err := appcontainer.SignLedgerImages(context.Background(), &recordingImageSigner{}, &fakeLedgerSyft{}, fakeLedgerResolver{}, io.Discard, io.Discard, appcontainer.SignLedgerImagesInput{
		Entries:               []imageledger.Entry{ledgerSignEntry(t)},
		ReleaseTag:            "v1.2.3",
		PredicateEnvelopePath: path,
		Method:                domainrelease.SignMethodKMS,
		KeyRef:                "env://COSIGN_KEY",
	})
	if !errors.Is(err, errs.ErrMalformedInput) {
		t.Fatalf("err = %v, want ErrMalformedInput", err)
	}
}

func TestSignLedgerImages_RejectsUnexpectedRepository(t *testing.T) {
	t.Chdir(t.TempDir())

	entry := ledgerSignEntry(t)
	entry.CandidateTag = "codeberg.org/evil/gommitlint:staging-v1.2.3-rust"

	err := appcontainer.SignLedgerImages(context.Background(), &recordingImageSigner{}, &fakeLedgerSyft{}, fakeLedgerResolver{}, io.Discard, io.Discard, appcontainer.SignLedgerImagesInput{
		Entries:                 []imageledger.Entry{entry},
		ReleaseTag:              "v1.2.3",
		PredicatePath:           writeBasePredicate(t),
		Method:                  domainrelease.SignMethodKMS,
		KeyRef:                  "env://COSIGN_KEY",
		ExpectedImageRepository: "codeberg.org/itiquette/gommitlint",
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(err.Error(), "candidate_tag") {
		t.Fatalf("err = %v, want candidate_tag context", err)
	}
}

func TestSignLedgerImages_RejectsStrictSBOMPatternMismatch(t *testing.T) {
	t.Chdir(t.TempDir())

	entry := ledgerSignEntry(t)
	entry.SBOM = "gommitlint-1.2.3-analyzed-container-sbom.cyclonedx.json"

	err := appcontainer.SignLedgerImages(context.Background(), &recordingImageSigner{}, &fakeLedgerSyft{}, fakeLedgerResolver{}, io.Discard, io.Discard, appcontainer.SignLedgerImagesInput{
		Entries:         []imageledger.Entry{entry},
		ReleaseTag:      "v1.2.3",
		PredicatePath:   writeBasePredicate(t),
		Method:          domainrelease.SignMethodKMS,
		KeyRef:          "env://COSIGN_KEY",
		SBOMPathPattern: `^dist/image-sbom[-A-Za-z0-9_.]*\.cyclonedx\.json$`,
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(err.Error(), "sbom") {
		t.Fatalf("err = %v, want sbom context", err)
	}
}

func TestSignLedgerImages_FallsBackToDigestRefWhenCandidateMissing(t *testing.T) {
	t.Chdir(t.TempDir())

	entry := ledgerSignEntry(t)
	signer := &recordingImageSigner{}

	err := appcontainer.SignLedgerImages(context.Background(), signer, &fakeLedgerSyft{}, fakeLedgerResolver{entry.Ref: entry.Digest}, io.Discard, io.Discard, appcontainer.SignLedgerImagesInput{
		Entries:       []imageledger.Entry{entry},
		ReleaseTag:    "v1.2.3",
		PredicatePath: writeBasePredicate(t),
		Method:        domainrelease.SignMethodKMS,
		KeyRef:        "env://COSIGN_KEY",
	})
	if err != nil {
		t.Fatalf("SignLedgerImages: %v", err)
	}

	if len(signer.signs) != 1 || signer.signs[0].ImageRef != entry.Ref {
		t.Fatalf("sign calls = %+v, want digest ref %s", signer.signs, entry.Ref)
	}

	var predicate map[string]any
	if err := json.Unmarshal(signer.attests[1].predicate, &predicate); err != nil {
		t.Fatal(err)
	}

	image := asMap(t, asMap(t, asMap(t, predicate["buildDefinition"], "buildDefinition")["externalParameters"], "externalParameters")["image"], "image")
	if image["candidate_tag"] != "" {
		t.Errorf("candidate_tag in fallback predicate = %#v, want empty", image["candidate_tag"])
	}
}

func TestSignLedgerImages_RejectsRefDigestMismatch(t *testing.T) {
	t.Chdir(t.TempDir())

	entry := ledgerSignEntry(t)
	entry.Ref = "codeberg.org/itiquette/gommitlint@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	err := appcontainer.SignLedgerImages(context.Background(), &recordingImageSigner{}, &fakeLedgerSyft{}, fakeLedgerResolver{}, io.Discard, io.Discard, appcontainer.SignLedgerImagesInput{
		Entries:       []imageledger.Entry{entry},
		ReleaseTag:    "v1.2.3",
		PredicatePath: writeBasePredicate(t),
		Method:        domainrelease.SignMethodKMS,
		KeyRef:        "env://COSIGN_KEY",
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// signInput builds the standard SignLedgerImagesInput for one entry,
// shared by the premade-SBOM and provenance-extras regressions.
func signInput(t *testing.T, entry imageledger.Entry) appcontainer.SignLedgerImagesInput {
	t.Helper()

	return appcontainer.SignLedgerImagesInput{
		Entries:                 []imageledger.Entry{entry},
		ReleaseTag:              "v1.2.3",
		PredicatePath:           writeBasePredicate(t),
		Method:                  domainrelease.SignMethodKMS,
		KeyRef:                  "env://COSIGN_KEY",
		ExpectedImageRepository: "codeberg.org/itiquette/gommitlint",
		ExpectedBaseRepository:  "codeberg.org/itiquette/gommitlint-base",
		SBOMPathPattern:         `^dist/image-sbom[-A-Za-z0-9_.]*\.cyclonedx\.json$`,
	}
}

func TestSignLedgerImages_PremadeSBOMPin(t *testing.T) {
	t.Chdir(t.TempDir())

	entry := ledgerSignEntry(t)
	content := []byte(`{"bomFormat":"CycloneDX"}`)

	if err := os.MkdirAll("dist", 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(entry.SBOM, content, 0o600); err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(content)
	entry.SBOMSHA256 = hex.EncodeToString(sum[:])

	t.Run("matching pin attests the premade file without regenerating", func(t *testing.T) {
		signer := &recordingImageSigner{}
		syft := &fakeLedgerSyft{}

		err := appcontainer.SignLedgerImages(context.Background(), signer, syft, fakeLedgerResolver{entry.CandidateTag: entry.Digest}, &bytes.Buffer{}, io.Discard, signInput(t, entry))
		if err != nil {
			t.Fatalf("SignLedgerImages: %v", err)
		}

		if len(syft.targets) != 0 {
			t.Errorf("syft ran %v; a pinned SBOM must be attested as-is, never regenerated", syft.targets)
		}

		if len(signer.attests) != 2 || signer.attests[0].input.PredicatePath != entry.SBOM {
			t.Errorf("attests = %+v, want the premade SBOM attested first", signer.attests)
		}
	})

	t.Run("mismatching pin fails closed before any attestation", func(t *testing.T) {
		bad := entry
		bad.SBOMSHA256 = strings.Repeat("0", 64)
		signer := &recordingImageSigner{}

		err := appcontainer.SignLedgerImages(context.Background(), signer, &fakeLedgerSyft{}, fakeLedgerResolver{entry.CandidateTag: entry.Digest}, &bytes.Buffer{}, io.Discard, signInput(t, bad))
		if !errors.Is(err, errs.ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}

		if len(signer.attests) != 0 {
			t.Errorf("attests = %+v, want none after a pin mismatch", signer.attests)
		}
	})
}

func TestSignLedgerImages_ProvenanceExtras(t *testing.T) {
	t.Chdir(t.TempDir())

	t.Run("declared extras land in externalParameters", func(t *testing.T) {
		entry := ledgerSignEntry(t)
		entry.Provenance = map[string]any{"base_input_set": "abc123", "build_group": "core"}
		signer := &recordingImageSigner{}

		err := appcontainer.SignLedgerImages(context.Background(), signer, &fakeLedgerSyft{}, fakeLedgerResolver{entry.CandidateTag: entry.Digest}, &bytes.Buffer{}, io.Discard, signInput(t, entry))
		if err != nil {
			t.Fatalf("SignLedgerImages: %v", err)
		}

		var predicate map[string]any
		if err := json.Unmarshal(signer.attests[1].predicate, &predicate); err != nil {
			t.Fatal(err)
		}

		ext := asMap(t, asMap(t, predicate["buildDefinition"], "buildDefinition")["externalParameters"], "externalParameters")
		if ext["base_input_set"] != "abc123" || ext["build_group"] != "core" {
			t.Errorf("extras missing from externalParameters: %#v", ext)
		}
	})

	t.Run("collision with a computed key fails the run", func(t *testing.T) {
		entry := ledgerSignEntry(t)
		entry.Provenance = map[string]any{"image": "shadowed"}
		signer := &recordingImageSigner{}

		err := appcontainer.SignLedgerImages(context.Background(), signer, &fakeLedgerSyft{}, fakeLedgerResolver{entry.CandidateTag: entry.Digest}, &bytes.Buffer{}, io.Discard, signInput(t, entry))
		if !errors.Is(err, errs.ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation for reserved-key collision", err)
		}
	})
}

// TestSignLedgerImages_BaseKindEntries is the signer-flip proof: a
// base-kind ledger entry — the exact shape nanolinter-ci's `base-images
// collect --format ledger` emits — feeds `container ledger sign` green
// with zero consumer schema. The base entry's content-addressed final tag
// needs no release scope, its premade SBOM pin is verified (never
// regenerated), and the attested predicate carries the retired
// base-lineage field names (externalParameters.base_input_id, .flavor).
func TestSignLedgerImages_BaseKindEntries(t *testing.T) {
	t.Chdir(t.TempDir())

	repo := "codeberg.org/itiquette/nanolinter-base"
	baseID := strings.Repeat("b", 64)
	digest := "sha256:" + strings.Repeat("1", 64)
	sbomContent := []byte(`{"bomFormat":"CycloneDX"}`)
	sbomSum := sha256.Sum256(sbomContent)

	ledgerJSON, err := json.Marshal([]imageledger.Entry{{
		Role:         "base",
		ImageKind:    imageledger.ImageKindBase,
		Flavor:       "go",
		Ref:          repo + "@" + digest,
		Digest:       digest,
		SBOM:         "dist/base-sboms/base-sbom-go.cyclonedx.json",
		SBOMSHA256:   hex.EncodeToString(sbomSum[:]),
		Provenance:   map[string]any{"flavor": "go"},
		FinalTag:     repo + ":" + baseID + "-go",
		CandidateTag: repo + ":staging-" + baseID + "-go",
		BaseInputID:  baseID,
	}})
	if err != nil {
		t.Fatalf("marshal base ledger: %v", err)
	}

	entries, err := imageledger.Parse(ledgerJSON)
	if err != nil {
		t.Fatalf("parse base ledger: %v", err)
	}

	if mkErr := os.MkdirAll("dist/base-sboms", 0o750); mkErr != nil {
		t.Fatal(mkErr)
	}

	if writeErr := os.WriteFile("dist/base-sboms/base-sbom-go.cyclonedx.json", sbomContent, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	signer := &recordingImageSigner{}
	syft := &fakeLedgerSyft{}
	candidateTag := repo + ":staging-" + baseID + "-go"

	err = appcontainer.SignLedgerImages(context.Background(), signer, syft, fakeLedgerResolver{candidateTag: digest}, &bytes.Buffer{}, io.Discard, appcontainer.SignLedgerImagesInput{
		Entries:                 entries,
		PredicatePath:           writeBasePredicate(t),
		Method:                  domainrelease.SignMethodKMS,
		KeyRef:                  "env://COSIGN_KEY",
		ExpectedImageRepository: repo,
	})
	if err != nil {
		t.Fatalf("SignLedgerImages: %v", err)
	}

	if len(signer.signs) != 1 || signer.signs[0].ImageRef != candidateTag+"@"+digest {
		t.Fatalf("sign calls = %+v, want one sign of the staged candidate", signer.signs)
	}

	if len(syft.targets) != 0 {
		t.Errorf("syft ran %v; the pinned premade base SBOM must be attested as-is", syft.targets)
	}

	if len(signer.attests) != 2 || signer.attests[0].input.PredicateType != "cyclonedx" {
		t.Fatalf("attests = %+v, want premade SBOM then provenance", signer.attests)
	}

	var predicate map[string]any
	if err := json.Unmarshal(signer.attests[1].predicate, &predicate); err != nil {
		t.Fatal(err)
	}

	ext := asMap(t, asMap(t, predicate["buildDefinition"], "buildDefinition")["externalParameters"], "externalParameters")
	if ext["base_input_id"] != baseID || ext["flavor"] != "go" {
		t.Errorf("base lineage fields missing from externalParameters: %#v", ext)
	}

	if _, hasBase := ext["base"]; hasBase {
		t.Errorf("base entry must not fabricate a parent base ref: %#v", ext["base"])
	}

	image := asMap(t, ext["image"], "image")
	if image["flavor"] != "go" || image["final_tag"] != repo+":"+baseID+"-go" {
		t.Errorf("image externalParameters mismatch: %#v", image)
	}
}

// TestSignLedgerImages_BaseKindRequiresBaseInputID pins the base-entry
// relaxation's floor: exempting base entries from the base_ref pairing
// never waives their own lineage — base_input_id stays mandatory.
func TestSignLedgerImages_BaseKindRequiresBaseInputID(t *testing.T) {
	t.Chdir(t.TempDir())

	repo := "codeberg.org/itiquette/nanolinter-base"
	baseID := strings.Repeat("b", 64)
	entry := imageledger.Entry{
		Role:       "base",
		ImageKind:  imageledger.ImageKindBase,
		Flavor:     "go",
		Ref:        repo + "@" + ledgerSignDigest,
		Digest:     ledgerSignDigest,
		SBOM:       "dist/base-sboms/base-sbom-go.cyclonedx.json",
		SBOMSHA256: strings.Repeat("d", 64),
		FinalTag:   repo + ":" + baseID + "-go",
	}

	err := appcontainer.SignLedgerImages(context.Background(), &recordingImageSigner{}, &fakeLedgerSyft{}, fakeLedgerResolver{}, io.Discard, io.Discard, appcontainer.SignLedgerImagesInput{
		Entries:       []imageledger.Entry{entry},
		PredicatePath: writeBasePredicate(t),
		Method:        domainrelease.SignMethodKMS,
		KeyRef:        "env://COSIGN_KEY",
	})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "base entries must declare base_input_id") {
		t.Fatalf("err = %v, want base_input_id requirement", err)
	}
}
