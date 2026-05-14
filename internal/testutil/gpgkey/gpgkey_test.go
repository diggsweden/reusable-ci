//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package gpgkey_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/testutil/gpgkey"
)

func TestNew_GeneratesValidKey(t *testing.T) {
	k := gpgkey.New(t)

	if got := len(k.Fingerprint); got != 40 {
		t.Errorf("Fingerprint length = %d, want 40 (hex chars): %q", got, k.Fingerprint)
	}
	if k.Name == "" || k.Email == "" {
		t.Errorf("Name/Email empty: %q / %q", k.Name, k.Email)
	}
	if got := len(k.KeyID()); got != 16 {
		t.Errorf("KeyID length = %d, want 16: %q", got, k.KeyID())
	}
	if !strings.HasSuffix(k.Fingerprint, k.KeyID()) {
		t.Errorf("KeyID %q is not a suffix of fingerprint %q", k.KeyID(), k.Fingerprint)
	}
}

func TestArmoredKeys_ContainHeaders(t *testing.T) {
	k := gpgkey.New(t)
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "private",
			body: k.ArmoredPrivateKey(),
			want: []string{
				"-----BEGIN PGP PRIVATE KEY BLOCK-----",
				"-----END PGP PRIVATE KEY BLOCK-----",
			},
		},
		{
			name: "public",
			body: k.ArmoredPublicKey(),
			want: []string{
				"-----BEGIN PGP PUBLIC KEY BLOCK-----",
				"-----END PGP PUBLIC KEY BLOCK-----",
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			for _, want := range testCase.want {
				if !strings.Contains(testCase.body, want) {
					t.Errorf("armored %s key missing %q", testCase.name, want)
				}
			}
		})
	}
}

func TestKeys_AreIsolated(t *testing.T) {
	a := gpgkey.New(t)
	b := gpgkey.New(t)
	if a.Fingerprint == b.Fingerprint {
		t.Errorf("two New() calls returned the same fingerprint: %q", a.Fingerprint)
	}
	if a.GNUPGHOME == b.GNUPGHOME {
		t.Errorf("two New() calls returned the same GNUPGHOME: %q", a.GNUPGHOME)
	}
}
