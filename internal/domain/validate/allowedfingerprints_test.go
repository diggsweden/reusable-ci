// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/validate"
)

// canonicalFP is a valid 40-char hex fingerprint used across the
// happy-path cases.
const canonicalFP = "ABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD"

func TestParseAllowedFingerprints_HappyPath(t *testing.T) {
	body := []byte(`# Release-authorised GPG signers
` + canonicalFP + `
` + // GPG-rendered form (spaced):
		`ABCD EFAB CDEF ABCD EFAB CDEF ABCD EFAB CDEF DCBA
`)

	set, err := validate.ParseAllowedFingerprints(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if set.Len() != 2 {
		t.Errorf("Len = %d, want 2", set.Len())
	}

	if !set.Has(canonicalFP) {
		t.Errorf("set missing canonical fingerprint")
	}

	// Round-trip the GPG-spaced form: lookup should normalise.
	if !set.Has("abcd efab cdef abcd efab cdef abcd efab cdef dcba") {
		t.Errorf("case-insensitive + whitespace-tolerant lookup failed")
	}
}

func TestParseAllowedFingerprints_RejectsShortKeyID(t *testing.T) {
	body := []byte("ABCDEF1234567890\n")

	_, err := validate.ParseAllowedFingerprints(body)
	if err == nil {
		t.Fatalf("expected rejection of 16-hex short key ID")
	}

	if !strings.Contains(err.Error(), "line 1") {
		t.Errorf("error should cite the line number; got: %v", err)
	}
}

func TestParseAllowedFingerprints_RejectsNonHex(t *testing.T) {
	body := []byte(`# comment
ABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD
ZZZZEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD
`)

	_, err := validate.ParseAllowedFingerprints(body)
	if err == nil {
		t.Fatalf("expected rejection of non-hex character")
	}

	if !strings.Contains(err.Error(), "line 3") {
		t.Errorf("error should cite line 3 (after comment + valid entry); got: %v", err)
	}
}

func TestParseAllowedFingerprints_IgnoresCommentsAndBlanks(t *testing.T) {
	body := []byte(`
# header comment

# another comment

` + canonicalFP + `

# trailing comment
`)

	set, err := validate.ParseAllowedFingerprints(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if set.Len() != 1 {
		t.Errorf("Len = %d, want 1 (comments + blanks should not count)", set.Len())
	}
}

func TestParseAllowedFingerprints_EmptyFileIsEmptySet(t *testing.T) {
	set, err := validate.ParseAllowedFingerprints(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if set.Len() != 0 {
		t.Errorf("empty file should produce empty set; got Len = %d", set.Len())
	}

	if set.Has(canonicalFP) {
		t.Errorf("empty set must not match any fingerprint")
	}
}

func TestAllowedFingerprintSet_HasIsCaseInsensitive(t *testing.T) {
	set, _ := validate.ParseAllowedFingerprints([]byte(canonicalFP + "\n"))

	for _, query := range []string{
		canonicalFP,
		strings.ToLower(canonicalFP),
		"abcd efab cdef abcd efab cdef abcd efab cdef abcd",
		"ABCD EFAB CDEF ABCD EFAB CDEF ABCD EFAB CDEF ABCD",
	} {
		if !set.Has(query) {
			t.Errorf("Has(%q) = false, want true", query)
		}
	}
}

func TestAllowedFingerprintSet_HasOnZeroValueReturnsFalse(t *testing.T) {
	var s validate.AllowedFingerprintSet
	if s.Has(canonicalFP) {
		t.Errorf("zero-value set must report Has=false (no nil panic)")
	}
}
