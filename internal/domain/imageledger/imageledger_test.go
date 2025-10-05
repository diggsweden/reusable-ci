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
		Role:     "distroless",
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

	// The strict OCI parser accepts bracketed IPv6 registries, which the old
	// hand-written host regex could not represent.
	e = validEntry()
	e.Ref = "[::1]:5000/itiquette/gommitlint@" + goodDigest

	e.FinalTag = "[::1]:5000/itiquette/gommitlint:v1.2.3"
	if err := e.Validate("v1.2.3"); err != nil {
		t.Errorf("IPv6 registry entry rejected: %v", err)
	}

	// Promotion journals retain the source tag while the digest keeps the
	// reference immutable.
	e = validEntry()

	e.Ref = "codeberg.org/itiquette/gommitlint:staging-v1.2.3@" + goodDigest
	if err := e.Validate("v1.2.3"); err != nil {
		t.Errorf("tagged digest reference rejected: %v", err)
	}
}

// TestValidate_Rejects pins each rule to the message it produces, not just to
// "some validation error". Several of these mutations could plausibly trip a
// neighbouring rule -- a moving_tag that is also out of the repository, a
// candidate that is also unparsable -- and a bare errors.Is would accept that,
// leaving the rule the row is named for untested.
func TestValidate_Rejects(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		mutate func(*imageledger.Entry)
		want   string
	}{
		"bad_digest": {
			func(e *imageledger.Entry) { e.Digest = "sha256:short" },
			"digest must be sha256:",
		},
		"ref_not_pinned": {
			func(e *imageledger.Entry) { e.Ref = "codeberg.org/itiquette/gommitlint:v1.2.3" },
			"ref must be a registry path pinned by",
		},
		"ref_without_registry": {
			func(e *imageledger.Entry) { e.Ref = "itiquette/gommitlint@" + goodDigest },
			"ref must be a registry path pinned by",
		},
		"malformed_ref_path": {
			func(e *imageledger.Entry) { e.Ref = "codeberg.org/itiquette/BadName@" + goodDigest },
			"ref must be a registry path pinned by",
		},
		"bad_sbom_path": { // present but not *.cyclonedx.json
			func(e *imageledger.Entry) { e.SBOM = "dist/sbom.json" },
			"sbom must be a relative CycloneDX path",
		},
		"final_tag_no_tag": {
			func(e *imageledger.Entry) { e.FinalTag = "codeberg.org/itiquette/gommitlint@" + goodDigest },
			"final_tag must be a registry path with a tag",
		},
		"final_tag_scope": {
			func(e *imageledger.Entry) { e.FinalTag = "codeberg.org/itiquette/gommitlint:v9.9.9" },
			`must be scoped to release tag "v1.2.3"`,
		},
		"bad_candidate_ref": {
			func(e *imageledger.Entry) { e.CandidateTag = "not-a-registry-path:staging-v1.2.3" },
			"candidate_tag must be a registry path with a tag",
		},
		"candidate_scope": {
			func(e *imageledger.Entry) { e.CandidateTag = "codeberg.org/itiquette/gommitlint:v1.2.3" },
			"staging counterpart of final_tag",
		},
		"bad_image_kind": {
			func(e *imageledger.Entry) { e.ImageKind = "sidecar" },
			`image_kind must be "release" or "base"`,
		},
		"sbom_sha_not_hex": {
			func(e *imageledger.Entry) { e.SBOMSHA256 = "XYZ" },
			"sbom_sha256 must be 64 lowercase hex characters",
		},
		"sbom_sha_without_sbom": {
			func(e *imageledger.Entry) {
				e.SBOM = ""
				e.SBOMSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			},
			"sbom_sha256 requires sbom to name the file it pins",
		},
		"provenance_empty_key": {
			func(e *imageledger.Entry) { e.Provenance = map[string]any{"": "x"} },
			"provenance keys must be non-empty",
		},
		"bad_moving_ref": {
			func(e *imageledger.Entry) { e.MovingTag = "not-a-tag" },
			"moving_tag must be a registry path with a tag",
		},
		"malformed_final_path": {
			func(e *imageledger.Entry) { e.FinalTag = "codeberg.org/itiquette/BadName:v1.2.3" },
			"final_tag must be a registry path with a tag",
		},
		"moving_staging": {
			func(e *imageledger.Entry) { e.MovingTag = "codeberg.org/itiquette/gommitlint:staging-v1.2.3" },
			"must not be a staging or immutable release tag",
		},
		"moving_release": {
			func(e *imageledger.Entry) { e.MovingTag = "codeberg.org/itiquette/gommitlint:v1.2.3-rust" },
			"must not be a staging or immutable release tag",
		},
		"moving_foreign_repo": {
			func(e *imageledger.Entry) { e.MovingTag = "ghcr.io/other/repo:rust" },
			`must share final_tag's repository`,
		},
		// A dash separates the release tag from a suffix; a longer version is
		// another release, not a suffixed spelling of this one.
		"final_tag_longer_version": {
			func(e *imageledger.Entry) { e.FinalTag = "codeberg.org/itiquette/gommitlint:v1.2.30" },
			`must be scoped to release tag "v1.2.3"`,
		},
		"moving_equals_release": {
			func(e *imageledger.Entry) { e.MovingTag = "codeberg.org/itiquette/gommitlint:v1.2.3" },
			"must not be a staging or immutable release tag",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e := validEntry()
			tc.mutate(&e)

			err := e.Validate("v1.2.3")
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v\nwant it to name the rule: %q", err, tc.want)
			}
		})
	}
}

func TestValidate_AcceptsOptionalManifestMetadata(t *testing.T) {
	t.Parallel()

	// Role and SBOM are optional release-manifest metadata; a promotion
	// entry without them is valid.
	bare := validEntry()
	bare.Role = ""
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
		Role:         "base",
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

	// A moving tag outside final_tag's repository would be written into a
	// repository the entry never named, or rehomed by a cross-registry
	// promotion; base entries follow the release rule.
	e = baseEntry()
	e.MovingTag = "ghcr.io/other/project:latest"

	if err := e.Validate(""); !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "must share final_tag's repository") {
		t.Errorf("foreign moving tag on a base entry should fail, got %v", err)
	}

	// The shared format rules still bind base entries.
	e = baseEntry()
	e.Digest = "sha256:short"

	if err := e.Validate(""); !errors.Is(err, errs.ErrValidation) {
		t.Errorf("bad digest on base entry should fail, got %v", err)
	}
}

func TestValidateEntryRepository_AcceptsTagsWithinTheRepository(t *testing.T) {
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
	second.Role = "alpine"

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

	if len(entries) != 2 || entries[0].Role != "distroless" || entries[1].Role != "alpine" {
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
		Role: "distroless", Ref: "codeberg.org/o/r@sha256:" + strings.Repeat("a", 64),
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

// TestParse_RejectsDataAfterTheArray pins that a ledger is exactly one JSON
// array. "[]" followed by "[entry]" used to parse as an empty ledger, and
// "[entry] {...}" as one entry, so a concatenated or corrupted file was read
// as fewer images than it named.
func TestParse_RejectsDataAfterTheArray(t *testing.T) {
	t.Parallel()

	entry := `{"ref":"ghcr.io/o/a@` + goodDigest + `","digest":"` + goodDigest + `","final_tag":"ghcr.io/o/a:v1.2.3"}`

	for name, doc := range map[string]string{
		"second array":    "[]\n[" + entry + "]",
		"trailing object": "[" + entry + "] {\"x\":1}",
		"trailing text":   "[" + entry + "] not json",
	} {
		entries, err := imageledger.Parse([]byte(doc))
		if !errors.Is(err, errs.ErrMalformedInput) || entries != nil {
			t.Errorf("%s: Parse = %v, %v; want ErrMalformedInput and no entries", name, entries, err)
		}

		if _, mergeErr := imageledger.Merge([][]byte{[]byte(doc)}); !errors.Is(mergeErr, errs.ErrMalformedInput) {
			t.Errorf("%s: Merge = %v, want ErrMalformedInput", name, mergeErr)
		}
	}

	// Trailing whitespace is not trailing data.
	if entries, err := imageledger.Parse([]byte("[" + entry + "]\n\n")); err != nil || len(entries) != 1 {
		t.Errorf("trailing newlines: Parse = %v, %v; want one entry", entries, err)
	}
}

// TestParse_RejectsUnknownFields pins that a ledger carrying a field this
// build does not know is refused as malformed rather than read with the field
// silently dropped: the ledger is engine-written evidence read at the trust
// boundary, so an unknown field means a stale or foreign document.
func TestParse_RejectsUnknownFields(t *testing.T) {
	t.Parallel()

	_, err := imageledger.Parse([]byte(`[{"role":"app","not_a_ledger_field":true}]`))
	if !errors.Is(err, errs.ErrMalformedInput) {
		t.Fatalf("err = %v, want ErrMalformedInput", err)
	}

	if !strings.Contains(err.Error(), "not_a_ledger_field") {
		t.Errorf("err = %v, want it to name the unknown field", err)
	}
}

// TestParse_RejectsRepeatedMembers pins that a member named twice is refused
// rather than read last-wins: a second "digest" would replace the recorded
// digest with nothing to show for it. Nested repeats in provenance count too.
func TestParse_RejectsRepeatedMembers(t *testing.T) {
	t.Parallel()

	base := `"ref":"ghcr.io/o/a@` + goodDigest + `","digest":"` + goodDigest + `","final_tag":"ghcr.io/o/a:v1.2.3"`

	for name, doc := range map[string]string{
		"repeated digest":         `[{` + base + `,"digest":"sha256:` + strings.Repeat("f", 64) + `"}]`,
		"repeated provenance key": `[{` + base + `,"provenance":{"team":"a","team":"b"}}]`,
	} {
		if _, err := imageledger.Parse([]byte(doc)); !errors.Is(err, errs.ErrMalformedInput) || !strings.Contains(err.Error(), "appears twice") {
			t.Errorf("%s: err = %v, want the repeated member refused", name, err)
		}
	}

	// The same member in two different objects is not a repeat.
	two := `[{` + base + `},{` + strings.Replace(base, "ghcr.io/o/a", "ghcr.io/o/b", 2) + `}]`
	if entries, err := imageledger.Parse([]byte(two)); err != nil || len(entries) != 2 {
		t.Errorf("two entries: %v, %v", entries, err)
	}
}
