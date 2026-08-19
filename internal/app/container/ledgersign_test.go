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
	"reflect"
	"sort"
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

// assertNothingPublished requires a refusal to have reached neither cosign nor
// the registry.
//
// These five refusals are all decided from the inputs, before any signing
// begins, and none of them said so -- each discarded its signer. That is the
// claim worth holding: a guard that refuses an unexpected repository after
// signing it has not refused anything.
//
// Two refusals elsewhere in this file deliberately do not use this helper. A
// mismatched SBOM pin and a reserved-key collision are both found later, with
// a signature already published, and their tests assert that as it is.
func assertNothingPublished(t *testing.T, signer *recordingImageSigner) {
	t.Helper()

	if len(signer.signs) != 0 {
		t.Errorf("signed despite refusing the input: %+v", signer.signs)
	}

	if len(signer.attests) != 0 {
		t.Errorf("attested despite refusing the input: %+v", signer.attests)
	}
}

// TestSignLedgerImages_AttestsAGeneratedSBOMAndTheImagesLineage covers the
// ordinary entry: no SBOM is pinned, so one is generated from the same
// digest-pinned reference that gets signed, and the provenance that follows
// records the image's tags and names the base it was built on as a resolved
// dependency.
//
// The attested externalParameters are compared whole rather than field by
// field. These are claims a verifier reads, so a field appearing that nobody
// meant to attest is as much a change as one going missing, and the previous
// seven-way condition could see neither -- it reported the entire map without
// saying which field was wrong, and never looked at image.digest at all.
func TestSignLedgerImages_AttestsAGeneratedSBOMAndTheImagesLineage(t *testing.T) {
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

	if len(signer.signs) != 1 {
		t.Fatalf("sign calls = %+v, want exactly one", signer.signs)
	}

	if got := signer.signs[0].ImageRef; got != resolvedRef {
		t.Errorf("signed %q, want the digest-pinned candidate %q", got, resolvedRef)
	}

	// The SBOM is generated from the same reference that is signed. Scanning
	// a tag while signing a digest would attest a different image than the
	// one the signature covers.
	if !reflect.DeepEqual(syft.targets, []string{resolvedRef}) {
		t.Errorf("syft targets = %v, want [%s]", syft.targets, resolvedRef)
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

	wantImage := map[string]any{
		"ref":           resolvedRef,
		"digest_ref":    entry.Digest,
		"digest":        map[string]any{"sha256": strings.TrimPrefix(entry.Digest, "sha256:")},
		"final_tag":     entry.FinalTag,
		"moving_tag":    entry.MovingTag,
		"candidate_tag": entry.CandidateTag,
		"role":          entry.Role,
		"flavor":        entry.Flavor,
		"sbom":          entry.SBOM,
	}
	if image := asMap(t, ext["image"], "image"); !reflect.DeepEqual(image, wantImage) {
		t.Errorf("image externalParameters =\n %#v\nwant %#v", image, wantImage)
	}

	// Which parameters are attested at all, not only what the ones we expected
	// contain. An extra key here is an extra claim in a signed document.
	gotKeys := make([]string, 0, len(ext))
	for key := range ext {
		gotKeys = append(gotKeys, key)
	}

	sort.Strings(gotKeys)

	if wantKeys := []string{"base", "image", "ref", "source"}; !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Errorf("externalParameters keys = %v, want %v", gotKeys, wantKeys)
	}

	if asMap(t, ext["base"], "base")["ref"] != entry.BaseRef {
		t.Errorf("base externalParameters missing: %#v", ext["base"])
	}

	deps, ok := build["resolvedDependencies"].([]any)
	if !ok {
		t.Fatalf("resolvedDependencies is not a slice: %#v", build["resolvedDependencies"])
	}

	if len(deps) != 1 {
		t.Fatalf("resolvedDependencies = %#v, want exactly the base image", deps)
	}

	if uri := asMap(t, deps[0], "resolvedDependencies[0]")["uri"]; uri != "oci://"+entry.BaseRef {
		t.Errorf("base dependency uri = %#v, want oci://%s", uri, entry.BaseRef)
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

	// The envelope's wrapper is gone.
	if predicate["_type"] != nil || predicate["predicate"] != nil {
		t.Fatalf("attested predicate still contains envelope fields: %#v", predicate)
	}

	build := asMap(t, predicate["buildDefinition"], "buildDefinition")

	// Values only the envelope could have supplied. Asserting that the SLSA
	// fields merely exist cannot tell a predicate extracted from the envelope
	// from one built here and the envelope ignored, which is the whole claim.
	if got := build["buildType"]; got != "https://codeberg.org/itiquette/forgejo-ci/container-build/v1" {
		t.Errorf("buildType = %#v, want the envelope's", got)
	}

	if got := asMap(t, asMap(t, predicate["runDetails"], "runDetails")["builder"], "builder")["id"]; got != "builder" {
		t.Errorf("builder id = %#v, want the envelope's", got)
	}
}

func TestSignLedgerImages_RejectsEnvelopeWithoutPredicate(t *testing.T) {
	t.Chdir(t.TempDir())

	path := filepath.Join(t.TempDir(), "empty.intoto.json")
	if err := os.WriteFile(path, []byte(`{"predicateType":"https://slsa.dev/provenance/v1"}`), 0o600); err != nil { //nolint:gosec // test fixture.
		t.Fatal(err)
	}

	signer := &recordingImageSigner{}

	err := appcontainer.SignLedgerImages(context.Background(), signer, &fakeLedgerSyft{}, fakeLedgerResolver{}, io.Discard, io.Discard, appcontainer.SignLedgerImagesInput{
		Entries:               []imageledger.Entry{ledgerSignEntry(t)},
		ReleaseTag:            "v1.2.3",
		PredicateEnvelopePath: path,
		Method:                domainrelease.SignMethodKMS,
		KeyRef:                "env://COSIGN_KEY",
	})
	if !errors.Is(err, errs.ErrMalformedInput) {
		t.Fatalf("err = %v, want ErrMalformedInput", err)
	}

	assertNothingPublished(t, signer)
}

// TestSignLedgerImages_RejectsUnexpectedRepository covers the guard that
// keeps a ledger entry from pointing signing at an image outside the
// release's own repository.
//
// Five ref fields are checked and only candidate_tag was covered.
//
// Each case moves the whole set of refs that must agree with the field
// under test, so the entry stays internally consistent and only the
// repository constraint can fire. Moving one field alone does not test
// this guard: Entry.Validate separately requires moving_tag to share
// final_tag's repository, and that error also mentions "final_tag" --
// so a naive fixture passes while the repository check is switched off.
// The assertion matches the repository rule's own wording for the same
// reason.
func TestSignLedgerImages_RejectsUnexpectedRepository(t *testing.T) {
	const (
		imageRepo = "codeberg.org/itiquette/gommitlint"
		baseRepo  = "codeberg.org/itiquette/gommitlint-base"
		evilRepo  = "codeberg.org/evil/gommitlint"
	)

	for _, tc := range []struct {
		name    string
		mutate  func(*imageledger.Entry)
		wantCtx string
	}{
		{
			name:    "ref",
			mutate:  func(e *imageledger.Entry) { e.Ref = evilRepo + "@" + ledgerSignDigest },
			wantCtx: "ref must be under " + imageRepo,
		},
		{
			// final_tag, moving_tag and candidate_tag must share a
			// repository with each other, so they move together.
			name: "the release tags",
			mutate: func(e *imageledger.Entry) {
				e.FinalTag = evilRepo + ":v1.2.3-rust"
				e.MovingTag = evilRepo + ":rust"
				e.CandidateTag = evilRepo + ":staging-v1.2.3-rust"
			},
			wantCtx: "final_tag must be under " + imageRepo,
		},
		// There is deliberately no row isolating moving_tag. Its
		// repository check cannot be reached on its own: Entry.Validate
		// already requires moving_tag to share final_tag's repository, so
		// moving it alone fails there, and moving both fails on final_tag
		// first. That check is defence in depth behind a rule that
		// already covers it -- removing it breaks no test, which is the
		// honest state of it rather than a gap in this table.
		{
			// candidate_tag alone: its consistency rule compares only the
			// tag name, so the repository check is what catches this.
			name:    "candidate_tag",
			mutate:  func(e *imageledger.Entry) { e.CandidateTag = evilRepo + ":staging-v1.2.3-rust" },
			wantCtx: "candidate_tag must be under " + imageRepo,
		},
		{
			// base_ref is checked against a different repository, so a
			// single shared check would not do -- the base image lives
			// somewhere else by design.
			name:    "base_ref",
			mutate:  func(e *imageledger.Entry) { e.BaseRef = "codeberg.org/evil/gommitlint-base@" + ledgerSignDigest },
			wantCtx: "base_ref must be under " + baseRepo,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			entry := ledgerSignEntry(t)
			tc.mutate(&entry)

			signer := &recordingImageSigner{}

			err := appcontainer.SignLedgerImages(context.Background(), signer, &fakeLedgerSyft{}, fakeLedgerResolver{}, io.Discard, io.Discard, appcontainer.SignLedgerImagesInput{
				Entries:                 []imageledger.Entry{entry},
				ReleaseTag:              "v1.2.3",
				PredicatePath:           writeBasePredicate(t),
				Method:                  domainrelease.SignMethodKMS,
				KeyRef:                  "env://COSIGN_KEY",
				ExpectedImageRepository: imageRepo,
				ExpectedBaseRepository:  baseRepo,
			})
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}

			// The repository rule's own wording, so a consistency error
			// that merely mentions the same field name cannot stand in
			// for it.
			if !strings.Contains(err.Error(), tc.wantCtx) {
				t.Errorf("err = %v, want the repository constraint on %q", err, tc.wantCtx)
			}

			assertNothingPublished(t, signer)
		})
	}
}

func TestSignLedgerImages_RejectsStrictSBOMPatternMismatch(t *testing.T) {
	t.Chdir(t.TempDir())

	entry := ledgerSignEntry(t)
	entry.SBOM = "gommitlint-1.2.3-analyzed-container-sbom.cyclonedx.json"

	signer := &recordingImageSigner{}

	err := appcontainer.SignLedgerImages(context.Background(), signer, &fakeLedgerSyft{}, fakeLedgerResolver{}, io.Discard, io.Discard, appcontainer.SignLedgerImagesInput{
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

	assertNothingPublished(t, signer)
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

	signer := &recordingImageSigner{}

	err := appcontainer.SignLedgerImages(context.Background(), signer, &fakeLedgerSyft{}, fakeLedgerResolver{}, io.Discard, io.Discard, appcontainer.SignLedgerImagesInput{
		Entries:       []imageledger.Entry{entry},
		ReleaseTag:    "v1.2.3",
		PredicatePath: writeBasePredicate(t),
		Method:        domainrelease.SignMethodKMS,
		KeyRef:        "env://COSIGN_KEY",
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	assertNothingPublished(t, signer)
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

		if len(signer.attests) != 2 {
			t.Fatalf("attests = %+v, want the premade SBOM then provenance", signer.attests)
		}

		if got := signer.attests[0].input.PredicatePath; got != entry.SBOM {
			t.Errorf("attested predicate path = %q, want the pinned file %q", got, entry.SBOM)
		}

		// "As-is" has two halves and they fail differently: syft not running
		// says the file was not regenerated, and this says the bytes that
		// reached the attestation are the ones that were pinned.
		if got := signer.attests[0].predicate; !bytes.Equal(got, content) {
			t.Errorf("attested SBOM = %s, want the pinned content %s", got, content)
		}
	})

	// The image is signed before the pin is checked, so a mismatch aborts the
	// run with a signature already published and no SBOM attestation beside
	// it. That ordering is recorded here rather than asserted as desirable --
	// see docs/open-questions.md.
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

		// Signing happens first. Recorded so a change to that order is a
		// deliberate edit to this expectation rather than a silent one.
		if len(signer.signs) != 1 {
			t.Errorf("signs = %+v, want the image signed before the pin was checked", signer.signs)
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

	// Every key the engine computed is reserved, so a ledger entry cannot
	// rewrite an attested fact. Only "image" was covered, which demonstrated
	// the rule once rather than for each key it guards -- the same gap closed
	// on the release provenance path in 8a1e0ff8.
	for _, key := range []string{"image", "base", "ref", "source"} {
		t.Run("declaring "+key+" collides with a computed key", func(t *testing.T) {
			entry := ledgerSignEntry(t)
			entry.Provenance = map[string]any{key: "shadowed"}
			signer := &recordingImageSigner{}

			err := appcontainer.SignLedgerImages(context.Background(), signer, &fakeLedgerSyft{}, fakeLedgerResolver{entry.CandidateTag: entry.Digest}, &bytes.Buffer{}, io.Discard, signInput(t, entry))
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation for reserved key %q", err, key)
			}

			// The collision is a property of the ledger entry alone, but it
			// is found while building the provenance predicate -- by which
			// point the image is signed and its SBOM attested. Recorded as
			// it is, so the order cannot change unnoticed; see
			// docs/open-questions.md.
			if len(signer.signs) != 1 {
				t.Errorf("signs = %+v, want the image signed before the collision was found", signer.signs)
			}

			if len(signer.attests) != 1 || signer.attests[0].input.PredicateType != "cyclonedx" {
				t.Errorf("attests = %+v, want the SBOM attested and no provenance", signer.attests)
			}
		})
	}
}

// TestSignLedgerImages_BaseKindEntries is the signer-flip proof: a
// base-kind ledger entry — the exact shape nanolinter-ci's `base-images
// collect --format ledger` emits — feeds `container ledger sign` green
// with zero consumer schema. The base entry's content-addressed final tag
// needs no release scope, its premade SBOM pin is verified (never
// regenerated), and the attested predicate carries the retired
// base-lineage field names (externalParameters.base_input_id, .flavor).
// TestSignLedgerImages_BaseEntryRecordsItsLineageWithoutAParent covers what is
// particular about signing a base image: it is the bottom of the chain, so its
// provenance carries its own lineage -- the base input it was built from, and
// its flavor -- while naming no parent base of its own. Everything else that
// signs a base entry is shared with the ordinary path.
//
// The premade SBOM is attested by content, not merely by "syft did not run".
// Not regenerating is one half of attesting a pinned SBOM as-is; the other is
// that what reaches the attestation is the file that was pinned, and the two
// fail differently. TestSignLedgerImages_PremadeSBOMPin pins the path this
// comes from and rejects a hash that does not match.
func TestSignLedgerImages_BaseEntryRecordsItsLineageWithoutAParent(t *testing.T) {
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

	if len(signer.signs) != 1 {
		t.Fatalf("sign calls = %+v, want exactly one", signer.signs)
	}

	// Signed by digest through the staging tag: what was verified is what is
	// signed, rather than whatever the final tag points at by then.
	if got := signer.signs[0].ImageRef; got != candidateTag+"@"+digest {
		t.Errorf("signed %q, want the staged candidate %q", got, candidateTag+"@"+digest)
	}

	if len(syft.targets) != 0 {
		t.Errorf("syft ran %v; the pinned premade base SBOM must be attested as-is", syft.targets)
	}

	if len(signer.attests) != 2 {
		t.Fatalf("attests = %+v, want premade SBOM then provenance", signer.attests)
	}

	if got := signer.attests[0].input.PredicateType; got != "cyclonedx" {
		t.Errorf("first attestation type = %q, want cyclonedx", got)
	}

	// The half that "syft did not run" does not cover: the bytes attested are
	// the pinned file's. Attesting some other file, or an empty one, leaves
	// syft unused too.
	if got := signer.attests[0].predicate; !bytes.Equal(got, sbomContent) {
		t.Errorf("attested SBOM = %s, want the pinned file %s", got, sbomContent)
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

	signer := &recordingImageSigner{}

	err := appcontainer.SignLedgerImages(context.Background(), signer, &fakeLedgerSyft{}, fakeLedgerResolver{}, io.Discard, io.Discard, appcontainer.SignLedgerImagesInput{
		Entries:       []imageledger.Entry{entry},
		PredicatePath: writeBasePredicate(t),
		Method:        domainrelease.SignMethodKMS,
		KeyRef:        "env://COSIGN_KEY",
	})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "base entries must declare base_input_id") {
		t.Fatalf("err = %v, want base_input_id requirement", err)
	}

	assertNothingPublished(t, signer)
}

// TestSignLedgerImages_InputRefusals covers validateSignLedgerImagesInput,
// which had roughly half its branches exercised. Every case refuses before
// any signing happens.
func TestSignLedgerImages_InputRefusals(t *testing.T) {
	predicate := filepath.Join(t.TempDir(), "predicate.json")
	if err := os.WriteFile(predicate, []byte(`{"buildDefinition":{}}`), 0o600); err != nil { //nolint:gosec // test fixture.
		t.Fatal(err)
	}

	envelope := filepath.Join(t.TempDir(), "envelope.intoto.json")
	if err := os.WriteFile(envelope, []byte(`{"predicateType":"https://slsa.dev/provenance/v1","predicate":{}}`), 0o600); err != nil { //nolint:gosec // test fixture.
		t.Fatal(err)
	}

	base := func() appcontainer.SignLedgerImagesInput {
		return appcontainer.SignLedgerImagesInput{
			Entries:       []imageledger.Entry{ledgerSignEntry(t)},
			ReleaseTag:    "v1.2.3",
			PredicatePath: predicate,
			Method:        domainrelease.SignMethodKMS,
			KeyRef:        "env://COSIGN_KEY",
		}
	}

	// The collaborators are required, and their absence is a panic rather
	// than an error if unchecked -- a CLI wiring mistake would abort the
	// job with a stack trace instead of a usage message.
	t.Run("missing collaborators", func(t *testing.T) {
		t.Chdir(t.TempDir())

		for _, tc := range []struct {
			name string
			call func() error
		}{
			{
				name: "no cosign adapter",
				call: func() error {
					return appcontainer.SignLedgerImages(context.Background(), nil, &fakeLedgerSyft{}, fakeLedgerResolver{}, io.Discard, io.Discard, base())
				},
			},
			{
				name: "no syft adapter",
				call: func() error {
					return appcontainer.SignLedgerImages(context.Background(), &recordingImageSigner{}, nil, fakeLedgerResolver{}, io.Discard, io.Discard, base())
				},
			},
			{
				name: "no registry resolver",
				call: func() error {
					return appcontainer.SignLedgerImages(context.Background(), &recordingImageSigner{}, &fakeLedgerSyft{}, nil, io.Discard, io.Discard, base())
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if err := tc.call(); !errors.Is(err, errs.ErrUsage) {
					t.Fatalf("err = %v, want ErrUsage", err)
				}
			})
		}
	})

	for _, tc := range []struct {
		name    string
		mutate  func(*appcontainer.SignLedgerImagesInput)
		wantErr error
	}{
		{
			// Provenance is what the attestation asserts; with neither
			// source there is nothing to attest.
			name:    "neither predicate nor envelope",
			mutate:  func(in *appcontainer.SignLedgerImagesInput) { in.PredicatePath = "" },
			wantErr: errs.ErrMissingInput,
		},
		{
			// Both is ambiguous rather than redundant: the two can
			// disagree, and silently preferring one would attest
			// provenance the operator did not choose.
			name:    "both predicate and envelope",
			mutate:  func(in *appcontainer.SignLedgerImagesInput) { in.PredicateEnvelopePath = envelope },
			wantErr: errs.ErrUsage,
		},
		{
			name:    "empty ledger",
			mutate:  func(in *appcontainer.SignLedgerImagesInput) { in.Entries = nil },
			wantErr: errs.ErrValidation,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			in := base()
			tc.mutate(&in)

			signer := &recordingImageSigner{}

			err := appcontainer.SignLedgerImages(context.Background(), signer, &fakeLedgerSyft{}, fakeLedgerResolver{}, io.Discard, io.Discard, in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}

			assertNothingPublished(t, signer)
		})
	}
}
