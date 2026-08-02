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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestBaseArchMetadataJSONMatchesShellShape(t *testing.T) {
	t.Parallel()

	repo := "codeberg.org/itiquette/nanolinter-base"
	baseID := strings.Repeat("a", 64)
	contentID := strings.Repeat("b", 64)
	digest := "sha256:" + strings.Repeat("c", 64)

	got, err := BaseArchMetadataJSON(BaseArchMetadataInput{
		Flavor:      "runtime-core-base",
		Arch:        "amd64",
		Repository:  repo,
		Tag:         repo + ":" + baseID + "-runtime-core-base",
		ArchTag:     repo + ":staging-" + contentID + "-runtime-core-base-amd64",
		ArchDigest:  digest,
		BaseInputID: baseID,
		ContentID:   contentID,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := `{
  "flavor": "runtime-core-base",
  "arch": "amd64",
  "tag": "codeberg.org/itiquette/nanolinter-base:` + baseID + `-runtime-core-base",
  "arch_tag": "codeberg.org/itiquette/nanolinter-base:staging-` + contentID + `-runtime-core-base-amd64",
  "arch_ref": "codeberg.org/itiquette/nanolinter-base@` + digest + `",
  "base_input_id": "` + baseID + `",
  "content_id": "` + contentID + `"
}
`
	if got != want {
		t.Fatalf("metadata JSON = %s, want %s", got, want)
	}
}

func TestBaseArchMetadataJSONRejectsInvalidContentID(t *testing.T) {
	t.Parallel()

	baseID := strings.Repeat("a", 64)

	_, err := BaseArchMetadataJSON(BaseArchMetadataInput{
		Flavor:      "rust",
		Arch:        "amd64",
		Repository:  "codeberg.org/itiquette/nanolinter-base",
		Tag:         "codeberg.org/itiquette/nanolinter-base:" + baseID + "-rust",
		ArchTag:     "codeberg.org/itiquette/nanolinter-base:staging-" + strings.Repeat("b", 64) + "-rust-amd64",
		ArchDigest:  "sha256:" + strings.Repeat("c", 64),
		BaseInputID: baseID,
		ContentID:   "not-a-digest",
	})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "content-id") {
		t.Fatalf("err = %v, want content-id validation", err)
	}
}

func TestBaseCandidateMetadataJSONMatchesShellShape(t *testing.T) {
	t.Parallel()

	repo := "codeberg.org/itiquette/nanolinter-base"
	baseID := strings.Repeat("a", 64)
	contentID := strings.Repeat("b", 64)
	digest := "sha256:" + strings.Repeat("c", 64)
	sbomSHA := strings.Repeat("d", 64)

	got, err := BaseCandidateMetadataJSON(BaseCandidateMetadataInput{
		Flavor:       "rust",
		Repository:   repo,
		Tag:          repo + ":" + baseID + "-rust",
		Ref:          repo + "@" + digest,
		CandidateTag: repo + ":staging-" + baseID + "-rust",
		CandidateRef: repo + "@" + digest,
		BaseInputID:  baseID,
		ContentID:    contentID,
		SBOMSHA256:   sbomSHA,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := `{
  "flavor": "rust",
  "tag": "codeberg.org/itiquette/nanolinter-base:` + baseID + `-rust",
  "ref": "codeberg.org/itiquette/nanolinter-base@` + digest + `",
  "candidate_tag": "codeberg.org/itiquette/nanolinter-base:staging-` + baseID + `-rust",
  "candidate_ref": "codeberg.org/itiquette/nanolinter-base@` + digest + `",
  "base_input_id": "` + baseID + `",
  "content_id": "` + contentID + `",
  "sbom_sha256": "` + sbomSHA + `"
}
`
	if got != want {
		t.Fatalf("candidate metadata JSON = %s, want %s", got, want)
	}
}

func TestBaseCandidateMetadataJSONOmitsEmptySBOMAndDefaultsCandidateRef(t *testing.T) {
	t.Parallel()

	repo := "codeberg.org/itiquette/nanolinter-base"
	baseID := strings.Repeat("a", 64)
	contentID := strings.Repeat("b", 64)
	digest := "sha256:" + strings.Repeat("c", 64)

	got, err := BaseCandidateMetadataJSON(BaseCandidateMetadataInput{
		Flavor:       "runtime-core-base",
		Repository:   repo,
		Tag:          repo + ":" + baseID + "-runtime-core-base",
		Ref:          repo + "@" + digest,
		CandidateTag: repo + ":staging-" + baseID + "-runtime-core-base",
		BaseInputID:  baseID,
		ContentID:    contentID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(got, "sbom_sha256") {
		t.Fatalf("candidate metadata includes empty sbom_sha256: %s", got)
	}

	if !strings.Contains(got, `"candidate_ref": "`+repo+"@"+digest+`"`) {
		t.Fatalf("candidate_ref was not defaulted from ref: %s", got)
	}
}

func TestBaseCandidateMetadataJSONRejectsInvalidSBOMSHA(t *testing.T) {
	t.Parallel()

	baseID := strings.Repeat("a", 64)
	contentID := strings.Repeat("b", 64)

	_, err := BaseCandidateMetadataJSON(BaseCandidateMetadataInput{
		Flavor:       "rust",
		Repository:   "codeberg.org/itiquette/nanolinter-base",
		Tag:          "codeberg.org/itiquette/nanolinter-base:" + baseID + "-rust",
		Ref:          "codeberg.org/itiquette/nanolinter-base@sha256:" + strings.Repeat("c", 64),
		CandidateTag: "codeberg.org/itiquette/nanolinter-base:staging-" + baseID + "-rust",
		BaseInputID:  baseID,
		ContentID:    contentID,
		SBOMSHA256:   "nope",
	})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "sbom-sha256") {
		t.Fatalf("err = %v, want sbom-sha256 validation", err)
	}
}

func TestBaseInputFieldSelectsRequestedField(t *testing.T) {
	t.Parallel()

	baseID := strings.Repeat("a", 64)
	contentID := strings.Repeat("b", 64)

	got, err := BaseInputField(BaseInputFieldInput{
		Inputs: []BaseInput{{Flavor: "rust", ContentID: contentID, BaseInputID: baseID}},
		Flavor: "rust",
		Field:  "content-id",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got != contentID {
		t.Fatalf("content-id = %s, want %s", got, contentID)
	}
}

func TestBaseInputFieldRejectsMissingFlavor(t *testing.T) {
	t.Parallel()

	_, err := BaseInputField(BaseInputFieldInput{
		Inputs: []BaseInput{{Flavor: "rust", ContentID: strings.Repeat("b", 64), BaseInputID: strings.Repeat("a", 64)}},
		Flavor: "go",
		Field:  "base-input-id",
	})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "flavor not found") {
		t.Fatalf("err = %v, want missing flavor validation", err)
	}
}

func TestBaseArchRefSelectsVerifiedRef(t *testing.T) {
	t.Parallel()

	repo := "codeberg.org/itiquette/nanolinter-base"
	contentID := strings.Repeat("b", 64)
	ref := repo + "@sha256:" + strings.Repeat("c", 64)

	got, err := BaseArchRef(BaseArchRefInput{
		Metadata:  BaseArchMetadata{Flavor: "rust", Arch: "amd64", ContentID: contentID, ArchRef: ref},
		Flavor:    "rust",
		Arch:      "amd64",
		ContentID: contentID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got != ref {
		t.Fatalf("arch ref = %s, want %s", got, ref)
	}
}

func TestBaseArchRefRejectsContentMismatch(t *testing.T) {
	t.Parallel()

	repo := "codeberg.org/itiquette/nanolinter-base"

	_, err := BaseArchRef(BaseArchRefInput{
		Metadata:  BaseArchMetadata{Flavor: "rust", Arch: "amd64", ContentID: strings.Repeat("b", 64), ArchRef: repo + "@sha256:" + strings.Repeat("c", 64)},
		Flavor:    "rust",
		Arch:      "amd64",
		ContentID: strings.Repeat("d", 64),
	})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "content_id mismatch") {
		t.Fatalf("err = %v, want content mismatch validation", err)
	}
}

func TestCollectBaseImagesMergesVerifiedBuiltAndDecision(t *testing.T) {
	t.Parallel()

	repo := "codeberg.org/itiquette/nanolinter-base"
	coreID := strings.Repeat("a", 64)
	rustID := strings.Repeat("b", 64)
	rustContentID := strings.Repeat("c", 64)
	sbomSHA := strings.Repeat("d", 64)
	inputSetID := strings.Repeat("e", 64)
	sourceSHA := strings.Repeat("f", 64)
	coreRef := repo + "@sha256:" + strings.Repeat("1", 64)
	rustRef := repo + "@sha256:" + strings.Repeat("2", 64)

	result, err := CollectBaseImages(BaseImageCollectInput{
		VerifiedImages: []BaseImageCollectImage{{
			Flavor: "core", Tag: repo + ":" + coreID + "-core", Ref: coreRef, BaseInputID: coreID,
		}},
		BuiltImages: []BaseImageCollectImage{{
			Flavor:       "rust",
			Tag:          repo + ":" + rustID + "-rust",
			Ref:          rustRef,
			CandidateTag: repo + ":staging-" + rustID + "-rust",
			CandidateRef: rustRef,
			BaseInputID:  rustID,
			ContentID:    rustContentID,
			SBOMSHA256:   sbomSHA,
		}},
		MissingFlavors:           []string{"rust", "full"},
		IncludeSigningSBOMSHA256: true,
		EmitDecision:             true,
		BaseInputSetID:           inputSetID,
		BaseInputs: []BaseInput{
			{Flavor: "core", ContentID: strings.Repeat("0", 64), BaseInputID: coreID},
			{Flavor: "rust", ContentID: rustContentID, BaseInputID: rustID},
		},
		SourceSHA: sourceSHA,
	})
	if err != nil {
		t.Fatal(err)
	}

	wantAll := `[{"flavor":"core","tag":"` + repo + `:` + coreID + `-core","ref":"` + coreRef + `","candidate_tag":"","candidate_ref":"","base_input_id":"` + coreID + `"},{"flavor":"rust","tag":"` + repo + `:` + rustID + `-rust","ref":"` + rustRef + `","candidate_tag":"` + repo + `:staging-` + rustID + `-rust","candidate_ref":"` + rustRef + `","base_input_id":"` + rustID + `","content_id":"` + rustContentID + `","sbom_sha256":"` + sbomSHA + `"}]`
	if result.AllImagesJSON != wantAll {
		t.Fatalf("all images JSON = %s, want %s", result.AllImagesJSON, wantAll)
	}

	wantSigning := `[{"flavor":"rust","ref":"` + rustRef + `","tag":"` + repo + `:staging-` + rustID + `-rust","base_input_id":"` + rustID + `","sbom_sha256":"` + sbomSHA + `"}]`
	if result.ImagesJSON != wantSigning {
		t.Fatalf("signing images JSON = %s, want %s", result.ImagesJSON, wantSigning)
	}

	if result.UnresolvedFlavorsJSON != `["full"]` {
		t.Fatalf("unresolved flavors JSON = %s, want [\"full\"]", result.UnresolvedFlavorsJSON)
	}

	wantDecision := `{"base_input_set_id":"` + inputSetID + `","base_inputs":[{"flavor":"core","content_id":"` + strings.Repeat("0", 64) + `","base_input_id":"` + coreID + `"},{"flavor":"rust","content_id":"` + rustContentID + `","base_input_id":"` + rustID + `"}],"source_sha":"` + sourceSHA + `","all_found":false,"missing_flavors":["rust","full"],"unresolved_flavors":["full"],"reused_refs":[{"flavor":"core","tag":"` + repo + `:` + coreID + `-core","ref":"` + coreRef + `","base_input_id":"` + coreID + `"}],"built_refs":[{"flavor":"rust","tag":"` + repo + `:staging-` + rustID + `-rust","ref":"` + rustRef + `","base_input_id":"` + rustID + `"}],"promoted_refs":[{"flavor":"core","tag":"` + repo + `:` + coreID + `-core","ref":"` + coreRef + `","base_input_id":"` + coreID + `"},{"flavor":"rust","tag":"` + repo + `:` + rustID + `-rust","ref":"` + rustRef + `","base_input_id":"` + rustID + `"}]}`
	if result.DecisionJSON != wantDecision {
		t.Fatalf("decision JSON = %s, want %s", result.DecisionJSON, wantDecision)
	}
}

func TestCollectBaseImagesRejectsCandidatePairMismatch(t *testing.T) {
	t.Parallel()

	repo := "codeberg.org/itiquette/nanolinter-base"

	_, err := CollectBaseImages(BaseImageCollectInput{
		BuiltImages: []BaseImageCollectImage{{
			Flavor:       "rust",
			Tag:          repo + ":" + strings.Repeat("a", 64) + "-rust",
			Ref:          repo + "@sha256:" + strings.Repeat("b", 64),
			CandidateTag: repo + ":staging-" + strings.Repeat("a", 64) + "-rust",
			BaseInputID:  strings.Repeat("a", 64),
		}},
	})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "candidate_tag and candidate_ref together") {
		t.Fatalf("err = %v, want candidate pair validation", err)
	}
}

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

func TestSignBaseImagesSignsAttestsAndGeneratesSBOM(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := "codeberg.org/itiquette/nanolinter-base"
	baseID := strings.Repeat("c", 64)
	digest := "sha256:" + strings.Repeat("4", 64)
	registry := &fakeBaseImageRegistry{digests: map[string]string{repo + ":" + baseID + "-rust": digest}}
	signer := &fakeBaseImageSigner{}
	verifier := &fakeBaseImageVerifier{payload: baseLineagePayload(t,
		lineageFields{source: "https://codeberg.org/itiquette/nanolinter", workflow: "container-bases.yml", flavor: "rust", baseInputID: baseID},
	)}
	archive := &fakeBaseImageArchive{}
	sbom := &fakeBaseImageSBOM{}

	err := SignBaseImages(ctx, signer, verifier, sbom, archive, registry, io.Discard, io.Discard, BaseImageSignInput{
		Images:             []BaseImageMetadata{{Flavor: "rust", Tag: repo + ":" + baseID + "-rust", Ref: repo + "@" + digest, BaseInputID: baseID}},
		ExpectedRepository: repo,
		ExpectedSource:     "https://codeberg.org/itiquette/nanolinter",
		ExpectedWorkflow:   "container-bases.yml",
		SourceSHA:          strings.Repeat("d", 40),
		BuildType:          "https://codeberg.org/itiquette/forgejo-ci/container-build/v1",
		KeyRef:             "env://COSIGN_KEY",
	})
	if err != nil {
		t.Fatalf("SignBaseImages() error = %v", err)
	}

	if got, want := strings.Join(signer.ops, ","), "public-key,sign:codeberg.org/itiquette/nanolinter-base@"+digest+",attest:cyclonedx,attest:slsaprovenance1"; got != want {
		t.Errorf("signer ops = %s, want %s", got, want)
	}

	if archive.ref != repo+"@"+digest {
		t.Errorf("archive source = %q, want digest ref", archive.ref)
	}

	if !strings.HasPrefix(sbom.target, "oci-archive:") {
		t.Errorf("SBOM target = %q, want oci-archive", sbom.target)
	}
}

func TestSignBaseImagesRejectsTamperedPremadeSBOM(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "base-sbom-rust.cyclonedx.json"), []byte(`{"bomFormat":"CycloneDX"}`), 0o600); err != nil {
		t.Fatalf("write premade SBOM: %v", err)
	}

	repo := "codeberg.org/itiquette/nanolinter-base"
	baseID := strings.Repeat("e", 64)
	digest := "sha256:" + strings.Repeat("5", 64)
	registry := &fakeBaseImageRegistry{digests: map[string]string{repo + ":" + baseID + "-rust": digest}}
	signer := &fakeBaseImageSigner{}
	verifier := &fakeBaseImageVerifier{payload: baseLineagePayload(t,
		lineageFields{source: "https://codeberg.org/itiquette/nanolinter", workflow: "container-bases.yml", flavor: "rust", baseInputID: baseID},
	)}

	err := SignBaseImages(context.Background(), signer, verifier, nil, nil, registry, io.Discard, io.Discard, BaseImageSignInput{
		Images:             []BaseImageMetadata{{Flavor: "rust", Tag: repo + ":" + baseID + "-rust", Ref: repo + "@" + digest, BaseInputID: baseID, SBOMSHA256: strings.Repeat("0", 64)}},
		ExpectedRepository: repo,
		ExpectedSource:     "https://codeberg.org/itiquette/nanolinter",
		ExpectedWorkflow:   "container-bases.yml",
		SourceSHA:          strings.Repeat("f", 40),
		BuildType:          "https://codeberg.org/itiquette/forgejo-ci/container-build/v1",
		KeyRef:             "env://COSIGN_KEY",
		PremadeSBOMDir:     dir,
	})
	if err == nil || !strings.Contains(err.Error(), "pre-built SBOM for rust fails integrity check") {
		t.Fatalf("SignBaseImages() error = %v, want tamper error", err)
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
	cleaner := &fakeBaseImageStagingCleaner{versions: []string{
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
	cleaner := &fakeBaseImageStagingCleaner{versions: []string{"staging-" + contentID + "-go-arm64"}}

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
	cleaner := &fakeBaseImageStagingCleaner{versions: []string{"staging-not-a-content-id-rust"}}

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
	cleaner := &fakeBaseImageStagingCleaner{}

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

type fakeBaseImageStagingCleaner struct {
	versions  []string
	deleted   []string
	listOwner string
	listName  string
	deleteErr error
}

func (f *fakeBaseImageStagingCleaner) DeleteTag(_ context.Context, ref string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}

	f.deleted = append(f.deleted, ref)

	return nil
}

func (f *fakeBaseImageStagingCleaner) ListContainerPackageVersions(_ context.Context, owner, name string) ([]string, error) {
	f.listOwner = owner
	f.listName = name

	return append([]string{}, f.versions...), nil
}

type fakeBaseImageVerifier struct {
	payload []byte
}

type fakeBaseImageSigner struct {
	ops []string
}

func (f *fakeBaseImageSigner) PublicKey(_ context.Context, _ string, out, _ io.Writer) error {
	f.ops = append(f.ops, "public-key")
	_, err := out.Write([]byte("public key"))

	return err
}

func (f *fakeBaseImageSigner) SignImage(_ context.Context, in cosign.SignImageInput, _ io.Writer) error {
	f.ops = append(f.ops, "sign:"+in.ImageRef)

	return nil
}

func (f *fakeBaseImageSigner) AttestImage(_ context.Context, in cosign.AttestImageInput, _ io.Writer) error {
	f.ops = append(f.ops, "attest:"+in.PredicateType)

	return nil
}

type fakeBaseImageArchive struct {
	ref string
}

func (f *fakeBaseImageArchive) CopyDockerToOCIArchive(_ context.Context, ref, archive string, _ io.Writer) error {
	f.ref = ref

	return os.WriteFile(archive, []byte("oci archive"), 0o600)
}

type fakeBaseImageSBOM struct {
	target string
}

func (f *fakeBaseImageSBOM) Generate(_ context.Context, target string, outputs map[string]string, _ io.Writer) error {
	f.target = target

	return os.WriteFile(outputs["cyclonedx-json"], []byte(`{"bomFormat":"CycloneDX"}`), 0o600)
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
			"predicateType": slsaProvenanceV1PredicateType,
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
