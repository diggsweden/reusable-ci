// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package gpg_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/gpg"
)

const sampleColons = `sec:u:255:22:ABCDEF1234567890:1700000000:::u:::scESC:::+:::ed25519:::0:
fpr:::::::::FEEDFACEDEADBEEF1234567890ABCDEF12345678:
grp:::::::::1234567890ABCDEF1234567890ABCDEF12345678:
uid:u::::1700000000::HASH123::Reusable CI Test (test) <ci-test@example.invalid>::::::::::0:
ssb:u:255:22:0123456789ABCDEF:1700000000:::::s:::+:::ed25519::
fpr:::::::::AABBCCDDEEFF11223344556677889900AABBCCDD:
grp:::::::::FEDCBA0987654321FEDCBA0987654321FEDCBA09:`

func TestParseColonsOutput(t *testing.T) {
	t.Parallel()
	md := gpg.ParseColonsOutput(sampleColons)

	if md.Fingerprint != "FEEDFACEDEADBEEF1234567890ABCDEF12345678" {
		t.Errorf("Fingerprint = %q", md.Fingerprint)
	}
	if md.KeyID != "ABCDEF1234567890" {
		t.Errorf("KeyID = %q", md.KeyID)
	}
	if md.Name != "Reusable CI Test" {
		t.Errorf("Name = %q, want %q", md.Name, "Reusable CI Test")
	}
	if md.Email != "ci-test@example.invalid" {
		t.Errorf("Email = %q", md.Email)
	}
}

func TestParseColonsOutput_UIDWithoutComment(t *testing.T) {
	t.Parallel()
	md := gpg.ParseColonsOutput(`sec:u:::KEYID12345678901:
fpr:::::::::FPR:
uid:::::::HASH::Some Person <person@example.com>:`)
	if md.Name != "Some Person" || md.Email != "person@example.com" {
		t.Errorf("got name=%q email=%q", md.Name, md.Email)
	}
}

func TestParseColonsOutput_UIDWithoutEmail(t *testing.T) {
	t.Parallel()
	md := gpg.ParseColonsOutput(`uid:::::::HASH::No Email Person:`)
	if md.Name != "No Email Person" || md.Email != "" {
		t.Errorf("got name=%q email=%q", md.Name, md.Email)
	}
}

func TestParseColonsOutput_PicksFirstFingerprint(t *testing.T) {
	t.Parallel()
	// Multiple subkeys: each ssb has its own fpr line. Want the first
	// (primary), not subkeys.
	md := gpg.ParseColonsOutput(sampleColons)
	if md.Fingerprint != "FEEDFACEDEADBEEF1234567890ABCDEF12345678" {
		t.Errorf("did not pick primary fpr: %q", md.Fingerprint)
	}
}

func TestParseFingerprint(t *testing.T) {
	t.Parallel()
	if got := gpg.ParseFingerprint(sampleColons); got != "FEEDFACEDEADBEEF1234567890ABCDEF12345678" {
		t.Errorf("ParseFingerprint = %q", got)
	}
}

func TestParseFingerprint_NoMatch(t *testing.T) {
	t.Parallel()
	if got := gpg.ParseFingerprint("no fpr lines here"); got != "" {
		t.Errorf("ParseFingerprint on no-match = %q", got)
	}
}

func TestParseKeygrips(t *testing.T) {
	t.Parallel()
	grips := gpg.ParseKeygrips(sampleColons)
	want := []string{
		"1234567890ABCDEF1234567890ABCDEF12345678",
		"FEDCBA0987654321FEDCBA0987654321FEDCBA09",
	}
	if len(grips) != 2 {
		t.Fatalf("got %d grips, want 2: %v", len(grips), grips)
	}
	for i, g := range grips {
		if g != want[i] {
			t.Errorf("grips[%d] = %q, want %q", i, g, want[i])
		}
	}
}

func TestParseKeygrips_Empty(t *testing.T) {
	t.Parallel()
	if got := gpg.ParseKeygrips("no grips"); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}
