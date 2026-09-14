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
		{name: "encrypted sigstore", body: "-----BEGIN ENCRYPTED SIGSTORE PRIVATE KEY-----\n...payload...\n"},
		{name: "sigstore", body: "-----BEGIN SIGSTORE PRIVATE KEY-----\n...payload...\n"},
		{name: "encrypted cosign", body: "-----BEGIN ENCRYPTED COSIGN PRIVATE KEY-----\n...payload...\n"},
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

// TestRedactKeyMaterial_RedactsAMinimalHeaderJWT covers the smallest token
// the spec allows, which is the one the pattern used to miss.
//
// A header of just {"alg":"HS256"} is valid — "typ" is optional in RFC 7519 —
// and encodes to 17 characters after the leading "eyJ". The first segment's
// floor was 20, so a token carrying such a header did not match and reached
// the CI log in full. Tokens whose header also carries "typ" or "kid" cleared
// the floor and were redacted, which is why the gap survived: GitHub's OIDC
// tokens are the ones most likely to appear here, and they carry "typ".
func TestRedactKeyMaterial_RedactsAMinimalHeaderJWT(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		token string
	}{
		// base64url({"alg":"HS256"}) = eyJhbGciOiJIUzI1NiJ9 — 17 after "eyJ".
		{name: "minimal header", token: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"},
		{name: "header with typ", token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"},
		{name: "minimal payload", token: "eyJhbGciOiJIUzI1NiJ9.e30.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"},
		{name: "unsecured", token: "eyJhbGciOiJub25lIn0.e30."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := string(safeexec.RedactKeyMaterial([]byte("auth failed: " + tc.token)))
			if !strings.Contains(got, "JWT-shaped token") {
				t.Errorf("token was not redacted; got: %s", got)
			}

			if strings.Contains(got, tc.token) {
				t.Errorf("redacted output still contains the token: %s", got)
			}
		})
	}
}

func TestRedactKeyMaterial_DoesNotMatchShortDotSeparatedStrings(t *testing.T) {
	t.Parallel()

	// "foo.bar.baz" / filenames-with-dots / version strings must NOT
	// trigger JWT redaction: a JSON algorithm header is required.
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

// TestRedactKeyMaterial_MentionsTheMatchingMarker pins the notice the operator
// actually reads, and pins it as a WHOLE.
//
// Asking only whether the marker name appears in the output is a question the
// unredacted body answers too: the marker is a substring of the key block, so a
// redactor that returned the key verbatim satisfied it. That is not a
// hypothetical — returning body unchanged left this test passing while every
// byte of the private key came back. The marker name is the one part of the
// notice that is copied from the input, so it is the one part that cannot carry
// the assertion on its own.
//
// The whole notice is compared instead. It is a fixed string with one
// substituted marker name, so there is nothing to approximate, and an exact
// comparison also states what the redactor must NOT do: keep a prefix, append
// the original, or preserve surrounding context.
func TestRedactKeyMaterial_MentionsTheMatchingMarker(t *testing.T) {
	t.Parallel()

	const secret = "c2VjcmV0LWtleS1ieXRlcw"

	body := []byte("oops\n-----BEGIN OPENSSH PRIVATE KEY-----\n" + secret + "\n-----END OPENSSH PRIVATE KEY-----\n")

	got := string(safeexec.RedactKeyMaterial(body))

	const want = "<output redacted: contained a private-key marker (BEGIN OPENSSH PRIVATE KEY)>"
	if got != want {
		t.Errorf("notice = %q, want exactly %q", got, want)
	}

	// Stated separately from the equality above so a future change to the
	// notice's wording cannot quietly turn this into a leak: whatever the
	// notice says, the key body and the surrounding output are not in it.
	for _, leak := range []string{secret, "oops", "-----END OPENSSH PRIVATE KEY-----"} {
		if strings.Contains(got, leak) {
			t.Errorf("redacted output still contains %q: %s", leak, got)
		}
	}
}
