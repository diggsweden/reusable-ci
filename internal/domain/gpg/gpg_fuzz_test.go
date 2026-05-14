// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package gpg_test

import (
	"encoding/base64"
	"testing"
	"unicode/utf8"

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

func FuzzParseColonsOutput(f *testing.F) {
	for _, seed := range []string{
		sampleColons,
		`uid:::::::HASH::Some Person <person@example.com>:`,
		`fpr:::::::::FPR:\nuid:::::::HASH::No Email Person:`,
		"",
		"not:colons:format",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		md := gpg.ParseColonsOutput(input)
		for _, field := range []string{md.Fingerprint, md.KeyID, md.Name, md.Email} {
			if field != "" && !utf8.ValidString(field) {
				t.Fatalf("invalid UTF-8 field %q from input %q", field, input)
			}
		}
		for _, grip := range gpg.ParseKeygrips(input) {
			if grip == "" {
				t.Fatalf("empty keygrip parsed from %q", input)
			}
		}
	})
}
