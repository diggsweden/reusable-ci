// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package gpg_test

import (
	"encoding/base64"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/gpg"
)

func FuzzDecodeKey(f *testing.F) {
	seeds := []string{
		"",
		"-----BEGIN PGP PRIVATE KEY BLOCK-----\nbody\n-----END PGP PRIVATE KEY BLOCK-----",
		base64.StdEncoding.EncodeToString([]byte("-----BEGIN PGP PRIVATE KEY BLOCK-----\nbody\n-----END PGP PRIVATE KEY BLOCK-----")),
		"not valid base64 ===///===",
		"Zm9v",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		out, err := gpg.DecodeKey(input)
		if err != nil {
			return
		}

		if gpg.IsArmored(input) && string(out) != input {
			t.Fatalf("armored input modified: %q -> %q", input, out)
		}
	})
}

func FuzzParseKeygrips(f *testing.F) {
	for _, seed := range []string{
		sampleColons,
		`grp:::::::::FEDCBA0987654321FEDCBA0987654321FEDCBA09:`,
		"",
		"not:colons:format",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		for _, grip := range gpg.ParseKeygrips(input) {
			if grip == "" {
				t.Fatalf("empty keygrip parsed from %q", input)
			}
		}
	})
}
