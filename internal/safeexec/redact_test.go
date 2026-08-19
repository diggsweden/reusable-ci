// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package safeexec_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

func TestRedactKeyMaterial_PassesThroughCleanOutput(t *testing.T) {
	t.Parallel()

	clean := []byte("gpg: keyring `/tmp/.gnupg/pubring.kbx' created\ngpg: key imported\n")

	got := safeexec.RedactKeyMaterial(clean)
	if string(got) != string(clean) {
		t.Errorf("clean output should pass through unchanged; got %q", got)
	}
}

func TestRedactKeyMaterial_RedactsKnownMarkers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{
			name: "pgp",
			body: "-----BEGIN PGP PRIVATE KEY BLOCK-----\n...payload...\n-----END PGP PRIVATE KEY BLOCK-----",
		},
		{
			name: "openssh",
			body: "-----BEGIN OPENSSH PRIVATE KEY-----\n...payload...\n-----END OPENSSH PRIVATE KEY-----",
		},
		{
			name: "rsa",
			body: "gpg fail\n-----BEGIN RSA PRIVATE KEY-----\nMIIE...etc...\n",
		},
		{
			name: "ec",
			body: "-----BEGIN EC PRIVATE KEY-----\nMHcCAQEE...\n",
		},
		{
			name: "encrypted",
			body: "-----BEGIN ENCRYPTED PRIVATE KEY-----\nMIIBuwIBADANBg...\n",
		},
		{
			name: "generic",
			body: "-----BEGIN PRIVATE KEY-----\nMIIEvQIB...\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := safeexec.RedactKeyMaterial([]byte(tc.body))

			if !strings.Contains(string(got), "redacted") {
				t.Errorf("expected redaction notice; got %q", got)
			}

			// Ensure the original payload bytes are GONE from the
			// returned slice. The redactor replaces the whole body.
			for _, leak := range []string{"...payload...", "MIIE", "MHcCAQEE", "MIIBuwIBADANBg", "MIIEvQIB"} {
				if strings.Contains(string(got), leak) && strings.Contains(tc.body, leak) {
					t.Errorf("redacted output still contains %q; full output: %s", leak, got)
				}
			}
		})
	}
}

func TestRedactKeyMaterial_RedactsJWTShapedToken(t *testing.T) {
	t.Parallel()

	// A plausible JWT shape: header.payload.signature, each segment ≥20
	// base64url chars. Real tokens are longer; the test fixture stays
	// minimally above threshold.
	body := []byte("gh api failed: token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c (401)")

	got := string(safeexec.RedactKeyMaterial(body))
	if want := "JWT-shaped token"; !strings.Contains(got, want) {
		t.Errorf("expected redaction notice mentioning %q; got: %s", want, got)
	}

	// Original token segments must be absent.
	for _, leak := range []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9", "SflKxwRJSMeKKF2QT4fwpMeJ"} {
		if strings.Contains(got, leak) {
			t.Errorf("redacted output still contains token segment %q; output: %s", leak, got)
		}
	}
}

// TestRedactKeyMaterial_MinimalHeaderJWTIsNotRedacted records a gap.
//
// The pattern requires 20 base64url characters after the leading "eyJ" in
// every segment. A JWT header of just {"alg":"HS256"} -- valid, and the
// smallest one RFC 7519 permits, since "typ" is optional -- encodes to
// exactly 20 characters, which is 17 after the "eyJ". It therefore does
// not match, and such a token is propagated into the error and the CI
// log unredacted.
//
// Tokens whose header also carries "typ" or "kid" clear the floor and are
// redacted; GitHub's OIDC tokens do. See docs/open-questions.md.
func TestRedactKeyMaterial_MinimalHeaderJWTIsNotRedacted(t *testing.T) {
	t.Parallel()

	// base64url({"alg":"HS256"}) = eyJhbGciOiJIUzI1NiJ9
	minimal := []byte("auth failed: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk")

	if got := safeexec.RedactKeyMaterial(minimal); !bytes.Equal(got, minimal) {
		t.Errorf("minimal-header JWT is now redacted -- good; update this test and docs/open-questions.md: %s", got)
	}

	// The same token with "typ" in the header is caught, which is what
	// makes the threshold rather than the shape the deciding factor.
	withTyp := []byte("auth failed: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	if got := string(safeexec.RedactKeyMaterial(withTyp)); !strings.Contains(got, "JWT-shaped token") {
		t.Errorf("a typ-carrying header should still be redacted: %s", got)
	}
}

func TestRedactKeyMaterial_DoesNotMatchShortDotSeparatedStrings(t *testing.T) {
	t.Parallel()

	// "foo.bar.baz" / filenames-with-dots / version strings must NOT
	// trigger JWT redaction — the segment-length floor in the pattern
	// is what guards against false positives.
	for _, ok := range []string{
		"reading file foo.bar.baz",
		"version 1.2.3 released",
		"key.pgp imported",
		"signature.asc verified",
	} {
		if !bytes.Equal(safeexec.RedactKeyMaterial([]byte(ok)), []byte(ok)) {
			t.Errorf("benign dotted string was redacted: %q", ok)
		}
	}
}

func TestRedactKeyMaterial_MentionsTheMatchingMarker(t *testing.T) {
	t.Parallel()

	body := []byte("oops\n-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n-----END\n")

	got := string(safeexec.RedactKeyMaterial(body))
	if !strings.Contains(got, "BEGIN OPENSSH PRIVATE KEY") {
		t.Errorf("redaction notice should name the marker so operators can debug; got: %s", got)
	}
}
