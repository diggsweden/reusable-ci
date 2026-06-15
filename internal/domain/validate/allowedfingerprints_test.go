// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/validate"
)

// canonicalFP is a valid 40-char hex fingerprint used across the
// happy-path cases.
const canonicalFP = "ABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD"

func TestAllowedFingerprintSet_AddAndHas(t *testing.T) {
	set := validate.NewAllowedFingerprintSet()

	if !set.Add(canonicalFP) {
		t.Fatalf("Add(%q) = false, want true", canonicalFP)
	}

	if set.Len() != 1 {
		t.Errorf("Len = %d, want 1", set.Len())
	}

	if !set.Has(canonicalFP) {
		t.Errorf("set missing the added fingerprint")
	}
}

func TestAllowedFingerprintSet_AddNormalisesSpacedAndLowercase(t *testing.T) {
	set := validate.NewAllowedFingerprintSet()

	// The GPG-rendered spaced form must be accepted and normalised.
	if !set.Add("abcd efab cdef abcd efab cdef abcd efab cdef dcba") {
		t.Fatal("Add of spaced/lowercase fingerprint rejected")
	}

	for _, query := range []string{
		"ABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFDCBA",
		"abcdefabcdefabcdefabcdefabcdefabcdefdcba",
		"ABCD EFAB CDEF ABCD EFAB CDEF ABCD EFAB CDEF DCBA",
	} {
		if !set.Has(query) {
			t.Errorf("Has(%q) = false, want true (case/whitespace tolerant)", query)
		}
	}
}

func TestAllowedFingerprintSet_AddRejectsShortKeyID(t *testing.T) {
	set := validate.NewAllowedFingerprintSet()

	if set.Add("ABCDEF1234567890") { // 16-hex short key ID
		t.Fatal("Add accepted a 16-hex short key ID; must reject")
	}

	if set.Len() != 0 {
		t.Errorf("Len = %d, want 0 (rejected value must not widen the set)", set.Len())
	}
}

func TestAllowedFingerprintSet_AddRejectsNonHex(t *testing.T) {
	set := validate.NewAllowedFingerprintSet()

	if set.Add("ZZZZEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD") {
		t.Fatal("Add accepted a non-hex fingerprint; must reject")
	}

	if set.Len() != 0 {
		t.Errorf("Len = %d, want 0", set.Len())
	}
}

func TestAllowedFingerprintSet_AddIsIdempotent(t *testing.T) {
	set := validate.NewAllowedFingerprintSet()

	set.Add(canonicalFP)
	set.Add(strings.ToLower(canonicalFP))

	if set.Len() != 1 {
		t.Errorf("Len = %d, want 1 (same fingerprint added twice)", set.Len())
	}
}

func TestAllowedFingerprintSet_HasOnZeroValueReturnsFalse(t *testing.T) {
	var s validate.AllowedFingerprintSet
	if s.Has(canonicalFP) {
		t.Errorf("zero-value set must report Has=false (no nil panic)")
	}
}

func TestNewAllowedFingerprintSet_IsEmpty(t *testing.T) {
	set := validate.NewAllowedFingerprintSet()

	if set.Len() != 0 {
		t.Errorf("new set Len = %d, want 0", set.Len())
	}

	if set.Has(canonicalFP) {
		t.Errorf("new set must not match any fingerprint")
	}
}
