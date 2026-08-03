// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
)

const (
	goodDigest = "sha256:" + sixtyfour
	sixtyfour  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// validEntry is a minimal schema-valid entry scoped to release tag v1.2.3.
func validEntry() imageledger.Entry {
	return imageledger.Entry{
		Kind:     "distroless",
		Ref:      "codeberg.org/itiquette/gommitlint@" + goodDigest,
		Digest:   goodDigest,
		SBOM:     "dist/image-sbom-amd64.cyclonedx.json",
		FinalTag: "codeberg.org/itiquette/gommitlint:v1.2.3",
	}
}

func TestValidate_Accepts(t *testing.T) {
	t.Parallel()

	if err := validEntry().Validate("v1.2.3"); err != nil {
		t.Fatalf("valid entry rejected: %v", err)
	}

	// final_tag may be a suffixed scope of the release tag.
	e := validEntry()

	e.FinalTag = "codeberg.org/itiquette/gommitlint:v1.2.3-amd64"
	if err := e.Validate("v1.2.3"); err != nil {
		t.Errorf("suffixed final_tag rejected: %v", err)
	}

	// candidate_tag scoped to staging-<release> is accepted.
	e = validEntry()

	e.CandidateTag = "codeberg.org/itiquette/gommitlint:staging-v1.2.3"
	if err := e.Validate("v1.2.3"); err != nil {
		t.Errorf("valid candidate_tag rejected: %v", err)
	}

	// moving_tag is accepted when it is a stable pointer, not a staging or
	// immutable release tag.
	e = validEntry()

	e.MovingTag = "codeberg.org/itiquette/gommitlint:rust"
	if err := e.Validate("v1.2.3"); err != nil {
		t.Errorf("valid moving_tag rejected: %v", err)
	}
}

func TestValidate_Rejects(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*imageledger.Entry){
		"bad_digest":       func(e *imageledger.Entry) { e.Digest = "sha256:short" },
		"ref_not_pinned":   func(e *imageledger.Entry) { e.Ref = "codeberg.org/itiquette/gommitlint:v1.2.3" },
		"bad_sbom_path":    func(e *imageledger.Entry) { e.SBOM = "dist/sbom.json" }, // present but not *.cyclonedx.json
		"final_tag_no_tag": func(e *imageledger.Entry) { e.FinalTag = "codeberg.org/itiquette/gommitlint@" + goodDigest },
		"final_tag_scope":  func(e *imageledger.Entry) { e.FinalTag = "codeberg.org/itiquette/gommitlint:v9.9.9" },
		"bad_candidate_ref": func(e *imageledger.Entry) {
			e.CandidateTag = "not-a-registry-path:staging-v1.2.3"
		},
		"candidate_scope":  func(e *imageledger.Entry) { e.CandidateTag = "codeberg.org/itiquette/gommitlint:v1.2.3" },
		"bad_image_kind":   func(e *imageledger.Entry) { e.ImageKind = "sidecar" },
		"sbom_sha_not_hex": func(e *imageledger.Entry) { e.SBOMSHA256 = "XYZ" },
		"sbom_sha_without_sbom": func(e *imageledger.Entry) {
			e.SBOM = ""
			e.SBOMSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		},
		"provenance_empty_key": func(e *imageledger.Entry) { e.Provenance = map[string]any{"": "x"} },
		"bad_moving_ref":       func(e *imageledger.Entry) { e.MovingTag = "not-a-tag" },
		"moving_staging":       func(e *imageledger.Entry) { e.MovingTag = "codeberg.org/itiquette/gommitlint:staging-v1.2.3" },
		"moving_release":       func(e *imageledger.Entry) { e.MovingTag = "codeberg.org/itiquette/gommitlint:v1.2.3-rust" },
		"moving_foreign_repo":  func(e *imageledger.Entry) { e.MovingTag = "ghcr.io/other/repo:rust" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e := validEntry()
			mutate(&e)

			if err := e.Validate("v1.2.3"); !errors.Is(err, errs.ErrValidation) {
				t.Errorf("expected validation error, got %v", err)
			}
		})
	}
}

func TestValidate_AcceptsOptionalManifestMetadata(t *testing.T) {
	t.Parallel()

	// Kind and SBOM are optional release-manifest metadata; a promotion
	// entry without them is valid.
	bare := validEntry()
	bare.Kind = ""
	bare.SBOM = ""

	if err := bare.Validate("v1.2.3"); err != nil {
		t.Errorf("entry without kind/sbom should be valid, got %v", err)
	}

	// When present, SBOM accepts the convention domain/sbom/filenames.go
	// emits (e.g. <name>-<version>-analyzed-container-sbom.cyclonedx.json),
	// not just the legacy dist/image-sbom* name.
	convention := validEntry()
	convention.SBOM = "gommitlint-1.2.3-analyzed-container-sbom.cyclonedx.json"

	if err := convention.Validate("v1.2.3"); err != nil {
		t.Errorf("codebase SBOM filename should be accepted, got %v", err)
	}
}

func TestValidateAll_AcceptsEveryImageKindAndLegacyAbsence(t *testing.T) {
	t.Parallel()

	// Each engine-known scope validates; the empty string is a legacy entry
	// (recorded before image_kind existed, treated as "release") and must
	// not fail either.
	kinds := []imageledger.ImageKind{
		"",
		imageledger.ImageKindRelease,
		imageledger.ImageKindBase,
	}

	entries := make([]imageledger.Entry, 0, len(kinds))

	for _, kind := range kinds {
		e := validEntry()
		e.ImageKind = kind
		entries = append(entries, e)
	}

	if err := imageledger.ValidateAll(entries, "v1.2.3"); err != nil {
		t.Errorf("image kinds %q should all validate, got %v", kinds, err)
	}
}

func TestValidate_RequiresReleaseTag(t *testing.T) {
	t.Parallel()

	if err := validEntry().Validate(""); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("empty release tag should be usage error, got %v", err)
	}
}

// baseEntry is a schema-valid image_kind=base entry: a content-addressed
// final tag (<base-input-id>-<flavor>) with its staging candidate.
func baseEntry() imageledger.Entry {
	repo := "codeberg.org/itiquette/nanolinter-base"
	baseID := strings.Repeat("b", 64)

	return imageledger.Entry{
		Kind:         "base",
		ImageKind:    imageledger.ImageKindBase,
		Flavor:       "go",
		Ref:          repo + "@" + goodDigest,
		Digest:       goodDigest,
		SBOM:         "dist/base-sboms/base-sbom-go.cyclonedx.json",
		SBOMSHA256:   sixtyfour,
		FinalTag:     repo + ":" + baseID + "-go",
		CandidateTag: repo + ":staging-" + baseID + "-go",
		BaseInputID:  baseID,
	}
}

func TestValidate_BaseEntriesAreNotReleaseScoped(t *testing.T) {
	t.Parallel()

	// A base image's final tag is content-addressed and exists outside any
	// release, so the entry validates with and without a release tag.
	if err := baseEntry().Validate(""); err != nil {
		t.Errorf("base entry without release tag rejected: %v", err)
	}

	if err := baseEntry().Validate("v1.2.3"); err != nil {
		t.Errorf("base entry with release tag rejected: %v", err)
	}
}

func TestValidate_BaseEntryRejections(t *testing.T) {
	t.Parallel()

	// The candidate discipline survives in base terms: the candidate must
	// be the exact staging counterpart of the content-addressed final tag.
	e := baseEntry()
	e.CandidateTag = "codeberg.org/itiquette/nanolinter-base:staging-" + strings.Repeat("f", 64) + "-go"

	if err := e.Validate(""); !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "staging counterpart") {
		t.Errorf("mismatched base candidate should fail, got %v", err)
	}

	e = baseEntry()
	e.MovingTag = "codeberg.org/itiquette/nanolinter-base:staging-latest"

	if err := e.Validate(""); !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "moving_tag") {
		t.Errorf("staging moving tag should fail, got %v", err)
	}

	// The shared format rules still bind base entries.
	e = baseEntry()
	e.Digest = "sha256:short"

	if err := e.Validate(""); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("bad digest on base entry should fail, got %v", err)
	}
}

func TestValidateEntryRepository(t *testing.T) {
	t.Parallel()

	entry := validEntry()
	entry.MovingTag = "codeberg.org/itiquette/gommitlint:latest"
	entry.CandidateTag = "codeberg.org/itiquette/gommitlint:staging-v1.2.3"

	if err := imageledger.ValidateEntryRepository(entry, "codeberg.org/itiquette/gommitlint"); err != nil {
		t.Fatalf("expected repository accepted: %v", err)
	}

	entry.CandidateTag = "codeberg.org/evil/gommitlint:staging-v1.2.3"
	if err := imageledger.ValidateEntryRepository(entry, "codeberg.org/itiquette/gommitlint"); !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("unexpected repository should be validation error, got %v", err)
	}
}

func TestAppend_RoundTrip(t *testing.T) {
	t.Parallel()

	// Append to an empty ledger, then to the result.
	out, added, err := imageledger.Append(nil, validEntry(), "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	if !added {
		t.Error("appending a new entry should report added=true")
	}

	second := validEntry()
	second.Kind = "alpine"

	out, added, err = imageledger.Append(out, second, "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	if !added {
		t.Error("appending a distinct entry should report added=true")
	}

	entries, err := imageledger.Parse(out)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 2 || entries[0].Kind != "distroless" || entries[1].Kind != "alpine" {
		t.Errorf("round-trip entries = %+v", entries)
	}
}

func TestAppend_RejectsInvalidEntryBeforeWriting(t *testing.T) {
	t.Parallel()

	bad := validEntry()
	bad.Digest = "nope"

	if _, _, err := imageledger.Append(nil, bad, "v1.2.3"); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("Append should validate before appending, got %v", err)
	}
}

func TestParse_RejectsNonArray(t *testing.T) {
	t.Parallel()

	// Unparsable / wrong-shape JSON is malformed input (EX_DATAERR), not
	// a failed validation rule.
	if _, err := imageledger.Parse([]byte(`{"not":"an array"}`)); !errors.Is(err, errs.ErrMalformedInput) {
		t.Errorf("non-array should be a malformed-input error, got %v", err)
	}

	// Empty input → empty ledger, no error.
	entries, err := imageledger.Parse([]byte("  \n"))
	if err != nil || len(entries) != 0 {
		t.Errorf("empty input: entries=%v err=%v", entries, err)
	}
}

func TestMarshal_EmptyIsArray(t *testing.T) {
	t.Parallel()

	out, err := imageledger.Marshal(nil)
	if err != nil {
		t.Fatal(err)
	}

	if strings.TrimSpace(string(out)) != "[]" {
		t.Errorf("empty ledger = %q, want []", out)
	}
}

func TestEntry_DigestSource(t *testing.T) {
	t.Parallel()

	if got := (imageledger.Entry{CandidateTag: "reg/repo:staging-v1", FinalTag: "reg/repo:v1"}).DigestSource(); got != "reg/repo:staging-v1" {
		t.Errorf("candidate present: DigestSource()=%q, want the candidate tag", got)
	}

	if got := (imageledger.Entry{FinalTag: "reg/repo:v1"}).DigestSource(); got != "reg/repo:v1" {
		t.Errorf("no candidate: DigestSource()=%q, want the final tag", got)
	}

	if got := (imageledger.Entry{}).DigestSource(); got != "" {
		t.Errorf("neither set: DigestSource()=%q, want empty", got)
	}
}

func TestEntry_PinDigest(t *testing.T) {
	t.Parallel()

	const dig = "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	// Derives the canonical <registry>/<path>@<digest> ref from the
	// digest source, stripping the source tag (and leaving a host:port).
	e := imageledger.Entry{CandidateTag: "codeberg.org:443/owner/repo:staging-v1.2.3"}
	e.PinDigest(dig)

	if e.Digest != dig {
		t.Errorf("Digest=%q, want %q", e.Digest, dig)
	}

	if want := "codeberg.org:443/owner/repo@" + dig; e.Ref != want {
		t.Errorf("Ref=%q, want %q (tag stripped, host:port kept)", e.Ref, want)
	}
}

func TestAppend_IdempotentOnRetry(t *testing.T) {
	t.Parallel()

	const tag = "v1.2.3"

	entry := imageledger.Entry{
		Kind: "distroless", Ref: "codeberg.org/o/r@sha256:" + strings.Repeat("a", 64),
		Digest: "sha256:" + strings.Repeat("a", 64), SBOM: "dist/image-sbom.cyclonedx.json",
		FinalTag: "codeberg.org/o/r:v1.2.3",
	}

	first, added, err := imageledger.Append(nil, entry, tag)
	if err != nil {
		t.Fatalf("first append: %v", err)
	}

	if !added {
		t.Error("first append should report added=true")
	}

	// Re-running ledger add with the same entry (retried build job) must
	// not grow the ledger, and must report added=false so the CLI can say
	// "already recorded" instead of falsely claiming an append.
	second, added, err := imageledger.Append(first, entry, tag)
	if err != nil {
		t.Fatalf("retry append: %v", err)
	}

	if added {
		t.Error("retrying an already-recorded entry should report added=false")
	}

	got, err := imageledger.Parse(second)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 {
		t.Errorf("retry appended a duplicate: ledger has %d entries, want 1", len(got))
	}
}
