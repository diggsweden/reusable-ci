// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpg_test

import (
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/gpg"
)

const sampleColons = `sec:u:255:22:ABCDEF1234567890:1700000000:::u:::scESC:::+:::ed25519:::0:
fpr:::::::::FEEDFACEDEADBEEF1234567890ABCDEF12345678:
grp:::::::::1234567890ABCDEF1234567890ABCDEF12345678:
uid:u::::1700000000::HASH123::Reusable CI Test (test) <ci-test@example.invalid>::::::::::0:
ssb:u:255:22:0123456789ABCDEF:1700000000:::::s:::+:::ed25519::
fpr:::::::::AABBCCDDEEFF11223344556677889900AABBCCDD:
grp:::::::::FEDCBA0987654321FEDCBA0987654321FEDCBA09:`

func TestParseKeygrips_ExtractsEveryKeygripFromColonOutput(t *testing.T) {
	t.Parallel()

	// Both grips, in file order: the primary key's and the subkey's. Order
	// matters because callers delete them in sequence.
	want := []string{
		"1234567890ABCDEF1234567890ABCDEF12345678",
		"FEDCBA0987654321FEDCBA0987654321FEDCBA09",
	}
	if got := gpg.ParseKeygrips(sampleColons); !slices.Equal(got, want) {
		t.Errorf("ParseKeygrips = %v, want %v", got, want)
	}
}

func TestParseKeygrips_Empty(t *testing.T) {
	t.Parallel()

	if got := gpg.ParseKeygrips("no grips"); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}
