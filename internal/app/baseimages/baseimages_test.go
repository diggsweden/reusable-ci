// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package baseimages

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
)

func TestVerifyExistingBaseImagesReportsVerifiedAndMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := "codeberg.org/itiquette/nanolinter-base"
	baseID := strings.Repeat("a", 64)
	digest := "sha256:" + strings.Repeat("1", 64)
	registry := &fakeBaseImageRegistry{digests: map[string]string{
		repo + ":" + baseID + "-rust": digest,
	}}
	verifier := &fakeBaseImageVerifier{payload: baseLineagePayload(t,
		lineageFields{source: "https://codeberg.org/itiquette/nanolinter", workflow: "container-bases.yml", flavor: "rust", baseInputID: baseID},
	)}

	result, err := VerifyExistingBaseImages(ctx, registry, verifier, io.Discard, BaseImageVerifyExistingInput{
		Flavors:            []string{"rust", "go"},
		BaseInputID:        baseID,
		ExpectedRepository: repo,
		ExpectedSource:     "https://codeberg.org/itiquette/nanolinter",
		ExpectedWorkflow:   "container-bases.yml",
		CosignPublicKey:    "cosign.pub",
	})
	if err != nil {
		t.Fatalf("VerifyExistingBaseImages() error = %v", err)
	}

	if result.AllFound {
		t.Fatal("AllFound = true, want false")
	}

	wantImages := `[{"flavor":"rust","tag":"codeberg.org/itiquette/nanolinter-base:` + baseID + `-rust","ref":"codeberg.org/itiquette/nanolinter-base@` + digest + `","candidate_tag":"","candidate_ref":"","base_input_id":"` + baseID + `"}]`
	if result.AllImagesJSON != wantImages {
		t.Errorf("AllImagesJSON = %s, want %s", result.AllImagesJSON, wantImages)
	}

	if result.MissingFlavorsJSON != `["go"]` {
		t.Errorf("MissingFlavorsJSON = %s, want [\"go\"]", result.MissingFlavorsJSON)
	}
}

// TestBaseLineageSourceMatchesBothSignerGenerations pins the signer-flip
// compatibility contract: one EXPECTED_SOURCE (the bare repository URL)
// must verify base images signed by the retired `base-images sign` engine
// (externalParameters.source = URL) AND images signed via `container
// ledger sign` over a `release provenance` base predicate
// (externalParameters.source = "git+" + URL, the SLSA VCS URI). Any other
// source, and the git+ form of a DIFFERENT repository, still fail.
func TestBaseLineageSourceMatchesBothSignerGenerations(t *testing.T) {
	t.Parallel()

	baseID := strings.Repeat("a", 64)
	expected := baseImageLineageExpectation{
		Source:      "https://codeberg.org/itiquette/nanolinter",
		Workflow:    "container-bases.yml",
		Flavor:      "rust",
		BaseInputID: baseID,
	}

	cases := []struct {
		name   string
		source string
		wantOK bool
	}{
		{"retired signer bare URL", "https://codeberg.org/itiquette/nanolinter", true},
		{"ledger sign SLSA git+ URI", "git+https://codeberg.org/itiquette/nanolinter", true},
		{"different repository", "https://codeberg.org/attacker/nanolinter", false},
		{"git+ different repository", "git+https://codeberg.org/attacker/nanolinter", false},
		{"double git+ prefix", "git+git+https://codeberg.org/itiquette/nanolinter", false},
		{"empty source", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			payload := baseLineagePayload(t, lineageFields{
				source: tc.source, workflow: "container-bases.yml", flavor: "rust", baseInputID: baseID,
			})

			err := baseLineageAttestationMatches(payload, expected)
			if tc.wantOK && err != nil {
				t.Fatalf("baseLineageAttestationMatches(source=%q) = %v, want match", tc.source, err)
			}

			if !tc.wantOK && err == nil {
				t.Fatalf("baseLineageAttestationMatches(source=%q) matched, want mismatch", tc.source)
			}
		})
	}
}

func TestPromoteBaseImagesCopiesMissingFinalTagAndEmitsOutputs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := "codeberg.org/itiquette/nanolinter-base"
	baseID := strings.Repeat("b", 64)
	rustDigest := "sha256:" + strings.Repeat("2", 64)
	goDigest := "sha256:" + strings.Repeat("3", 64)
	rustFinal := repo + ":" + baseID + "-rust"
	rustCandidate := repo + ":candidate-" + baseID + "-rust"
	goFinal := repo + ":" + baseID + "-go"
	registry := &fakeBaseImageRegistry{digests: map[string]string{
		rustCandidate: rustDigest,
		goFinal:       goDigest,
	}}
	verifier := &fakeBaseImageVerifier{payload: baseLineagePayload(t,
		lineageFields{source: "https://codeberg.org/itiquette/nanolinter", workflow: "container-bases.yml", flavor: "rust", baseInputID: baseID},
		lineageFields{source: "https://codeberg.org/itiquette/nanolinter", workflow: "container-bases.yml", flavor: "go", baseInputID: baseID},
	)}

	result, err := PromoteBaseImages(ctx, registry, verifier, io.Discard, BaseImagePromoteInput{
		Images: []BaseImageMetadata{
			{Flavor: "rust", Tag: rustFinal, Ref: repo + "@" + rustDigest, CandidateTag: rustCandidate, CandidateRef: repo + "@" + rustDigest, BaseInputID: baseID},
			{Flavor: "go", Tag: goFinal, Ref: repo + "@" + goDigest, BaseInputID: baseID},
		},
		ExpectedRepository: repo,
		ExpectedSource:     "https://codeberg.org/itiquette/nanolinter",
		ExpectedWorkflow:   "container-bases.yml",
		CosignPublicKey:    "cosign.pub",
	})
	if err != nil {
		t.Fatalf("PromoteBaseImages() error = %v", err)
	}

	if got := registry.digests[rustFinal]; got != rustDigest {
		t.Fatalf("promoted final digest = %q, want %q", got, rustDigest)
	}

	if result.BaseInputID != baseID {
		t.Errorf("BaseInputID = %q, want %q", result.BaseInputID, baseID)
	}

	wantImages := `[{"flavor":"go","ref":"codeberg.org/itiquette/nanolinter-base@` + goDigest + `","base_input_id":"` + baseID + `"},{"flavor":"rust","ref":"codeberg.org/itiquette/nanolinter-base@` + rustDigest + `","base_input_id":"` + baseID + `"}]`
	if result.BaseImagesJSON != wantImages {
		t.Errorf("BaseImagesJSON = %s, want %s", result.BaseImagesJSON, wantImages)
	}

	wantInputs := `[{"flavor":"go","base_input_id":"` + baseID + `"},{"flavor":"rust","base_input_id":"` + baseID + `"}]`
	if result.BaseInputIDsJSON != wantInputs {
		t.Errorf("BaseInputIDsJSON = %s, want %s", result.BaseInputIDsJSON, wantInputs)
	}
}

func TestCleanupStagingBaseImagesDeletesPromotedAndStaleTags(t *testing.T) {
	t.Parallel()

	repo := "codeberg.org/itiquette/nanolinter-base"
	baseID := strings.Repeat("1", 64)
	contentID := strings.Repeat("2", 64)
	digest := "sha256:" + strings.Repeat("6", 64)
	registry := &fakeBaseImageRegistry{digests: map[string]string{
		repo + ":" + baseID + "-rust":                        digest,
		repo + ":staging-" + baseID + "-rust":                digest,
		repo + ":staging-" + contentID + "-rust-amd64":       "sha256:" + strings.Repeat("7", 64),
		repo + ":staging-" + strings.Repeat("3", 64) + "-go": "sha256:" + strings.Repeat("8", 64),
	}}
	cleaner := &fakeBaseImagePackageAPI{versions: []string{
		"1.0.0",
		"staging-" + contentID + "-rust-amd64",
		"staging-" + strings.Repeat("3", 64) + "-go",
	}}

	var log bytes.Buffer

	err := CleanupStagingBaseImages(context.Background(), registry, cleaner, &log, BaseImageCleanupStagingInput{
		Images: []BaseImageMetadata{{
			Flavor:       "rust",
			Tag:          repo + ":" + baseID + "-rust",
			Ref:          repo + "@" + digest,
			CandidateTag: repo + ":staging-" + baseID + "-rust",
			CandidateRef: repo + "@" + digest,
			BaseInputID:  baseID,
		}},
		BaseInputs:         []BaseInput{{Flavor: "rust", ContentID: contentID, BaseInputID: baseID}},
		ExpectedRepository: repo,
	})
	if err != nil {
		t.Fatalf("CleanupStagingBaseImages() error = %v", err)
	}

	deleted := strings.Join(cleaner.deleted, ",")
	for _, want := range []string{
		repo + ":staging-" + baseID + "-rust",
		repo + ":staging-" + contentID + "-rust-amd64",
		repo + ":staging-" + strings.Repeat("3", 64) + "-go",
	} {
		if !strings.Contains(deleted, want) {
			t.Fatalf("deleted tags = %s, missing %s", deleted, want)
		}
	}

	if cleaner.listOwner != "itiquette" || cleaner.listName != "nanolinter-base" {
		t.Fatalf("list package owner/name = %s/%s", cleaner.listOwner, cleaner.listName)
	}

	if !strings.Contains(log.String(), "Preserving base signature artifacts") {
		t.Fatalf("log missing signature preservation message: %s", log.String())
	}
}

func TestCleanupStagingBaseImagesPreservesReusableArchUntilFinalExists(t *testing.T) {
	t.Parallel()

	repo := "codeberg.org/itiquette/nanolinter-base"
	baseID := strings.Repeat("4", 64)
	contentID := strings.Repeat("5", 64)
	cleaner := &fakeBaseImagePackageAPI{versions: []string{"staging-" + contentID + "-go-arm64"}}

	var log bytes.Buffer

	err := CleanupStagingBaseImages(context.Background(), &fakeBaseImageRegistry{digests: map[string]string{}}, cleaner, &log, BaseImageCleanupStagingInput{
		BaseInputs:         []BaseInput{{Flavor: "go", ContentID: contentID, BaseInputID: baseID}},
		ExpectedRepository: repo,
	})
	if err != nil {
		t.Fatalf("CleanupStagingBaseImages() error = %v", err)
	}

	if len(cleaner.deleted) != 0 {
		t.Fatalf("deleted arch tag before final existed: %v", cleaner.deleted)
	}

	if !strings.Contains(log.String(), "Preserving reusable architecture base tag until final exists") {
		t.Fatalf("log missing preserve message: %s", log.String())
	}
}

func TestCleanupStagingBaseImagesRejectsUnexpectedStagingVersion(t *testing.T) {
	t.Parallel()

	repo := "codeberg.org/itiquette/nanolinter-base"
	cleaner := &fakeBaseImagePackageAPI{versions: []string{"staging-not-a-content-id-rust"}}

	err := CleanupStagingBaseImages(context.Background(), &fakeBaseImageRegistry{digests: map[string]string{}}, cleaner, io.Discard, BaseImageCleanupStagingInput{
		ExpectedRepository: repo,
	})
	if err == nil || !strings.Contains(err.Error(), "refusing to delete unexpected staging version") {
		t.Fatalf("err = %v, want unexpected staging version", err)
	}
}

func TestCleanupStagingBaseImagesFailsWhenFinalDigestDiffers(t *testing.T) {
	t.Parallel()

	repo := "codeberg.org/itiquette/nanolinter-base"
	baseID := strings.Repeat("9", 64)
	digest := "sha256:" + strings.Repeat("a", 64)
	registry := &fakeBaseImageRegistry{digests: map[string]string{repo + ":" + baseID + "-rust": "sha256:" + strings.Repeat("b", 64)}}
	cleaner := &fakeBaseImagePackageAPI{}

	err := CleanupStagingBaseImages(context.Background(), registry, cleaner, io.Discard, BaseImageCleanupStagingInput{
		Images: []BaseImageMetadata{{
			Flavor:       "rust",
			Tag:          repo + ":" + baseID + "-rust",
			Ref:          repo + "@" + digest,
			CandidateTag: repo + ":staging-" + baseID + "-rust",
			CandidateRef: repo + "@" + digest,
			BaseInputID:  baseID,
		}},
		ExpectedRepository: repo,
	})
	if err == nil || !strings.Contains(err.Error(), "staging cleanup safety check failed") {
		t.Fatalf("err = %v, want cleanup safety failure", err)
	}

	if len(cleaner.deleted) != 0 {
		t.Fatalf("deleted despite final digest mismatch: %v", cleaner.deleted)
	}
}

type fakeBaseImageRegistry struct {
	digests map[string]string
}

func (f *fakeBaseImageRegistry) ResolveDigest(_ context.Context, ref string) (string, error) {
	digest, ok := f.digests[ref]
	if !ok {
		return "", fmt.Errorf("missing %s: %w", ref, errs.ErrMissingInput)
	}

	return digest, nil
}

func (f *fakeBaseImageRegistry) CopyTag(_ context.Context, source, dest string) error {
	f.digests[dest] = digestFromRef(source)

	return nil
}

type fakeBaseImagePackageAPI struct {
	versions  []string
	deleted   []string
	listOwner string
	listName  string
	deleteErr error
}

func (f *fakeBaseImagePackageAPI) DeleteTag(_ context.Context, ref string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}

	f.deleted = append(f.deleted, ref)

	return nil
}

func (f *fakeBaseImagePackageAPI) ListContainerPackageVersions(_ context.Context, owner, name string) ([]string, error) {
	f.listOwner = owner
	f.listName = name

	return append([]string{}, f.versions...), nil
}

type fakeBaseImageVerifier struct {
	payload []byte
}

func (f *fakeBaseImageVerifier) VerifyImage(context.Context, cosign.VerifyImageInput, io.Writer) error {
	return nil
}

func (f *fakeBaseImageVerifier) VerifyAttestation(context.Context, cosign.VerifyAttestationInput, io.Writer) error {
	return nil
}

func (f *fakeBaseImageVerifier) VerifyAttestationOutput(_ context.Context, _ cosign.VerifyAttestationInput, out, _ io.Writer) error {
	_, err := out.Write(f.payload)

	return err
}

type lineageFields struct {
	source      string
	workflow    string
	flavor      string
	baseInputID string
}

func baseLineagePayload(t *testing.T, fields ...lineageFields) []byte {
	t.Helper()

	envelopes := make([]map[string]string, 0, len(fields))
	for _, field := range fields {
		statement := map[string]any{
			"predicateType": provenance.PredicateTypeV1,
			"predicate": map[string]any{
				"buildDefinition": map[string]any{
					"externalParameters": map[string]any{
						"source":        field.source,
						"workflow":      field.workflow,
						"flavor":        field.flavor,
						"base_input_id": field.baseInputID,
					},
				},
			},
		}

		statementBody, err := json.Marshal(statement)
		if err != nil {
			t.Fatalf("marshal statement: %v", err)
		}

		envelopes = append(envelopes, map[string]string{"payload": base64.StdEncoding.EncodeToString(statementBody)})
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(envelopes); err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}

	return body.Bytes()
}
