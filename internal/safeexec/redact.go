// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package safeexec

import (
	"bytes"

	"github.com/diggsweden/reusable-ci/v3/internal/secrettext"
)

// privateKeyMarkers are the PEM-style preamble lines that signal a
// private-key block. If any of these appear in subprocess output that
// we'd otherwise propagate into an error message or CI log, the body
// is redacted wholesale. Future-proofs against a subprocess (gpg,
// ssh-keygen, openssl) echoing input key material on stderr — current
// versions don't, but we're not paying the cost of "trust them
// forever" for the small gain of preserving an unredacted error.
//
// Markers are matched as substrings, case-sensitive. Listed in
// canonical RFC/spec form.
//
//nolint:gochecknoglobals // immutable lookup table.
var privateKeyMarkers = [][]byte{
	[]byte("BEGIN PGP PRIVATE KEY"),
	[]byte("BEGIN OPENSSH PRIVATE KEY"),
	[]byte("BEGIN RSA PRIVATE KEY"),
	[]byte("BEGIN EC PRIVATE KEY"),
	[]byte("BEGIN ENCRYPTED PRIVATE KEY"),
	[]byte("BEGIN PRIVATE KEY"),
	// cosign's own formats. Its keys are in none of the PEM shapes above:
	// a key from cosign 3.x opens
	// "-----BEGIN ENCRYPTED SIGSTORE PRIVATE KEY-----", which contains
	// "BEGIN ENCRYPTED PRIVATE KEY" only if you skip the word between.
	// This is the format reusable-ci's own signing material is stored in,
	// so it belongs here more than most.
	[]byte("BEGIN ENCRYPTED SIGSTORE PRIVATE KEY"),
	[]byte("BEGIN SIGSTORE PRIVATE KEY"),
	[]byte("BEGIN ENCRYPTED COSIGN PRIVATE KEY"),
}

// RedactKeyMaterial scans body for any private-key marker or a
// JWT-shaped token and, when found, replaces the entire body with a
// redaction notice. The whole body is replaced (rather than just the
// matching block) because determining the block boundary safely is
// more error-prone than dropping everything — and the caller wants
// the error to point at the failure, not preserve diagnostic detail
// at the cost of leaking what we just refused to log.
//
// Returns body unchanged when no marker is found, so callers can use
// it unconditionally.
func RedactKeyMaterial(body []byte) []byte {
	for _, marker := range privateKeyMarkers {
		if bytes.Contains(body, marker) {
			return []byte("<output redacted: contained a private-key marker (" + string(marker) + ")>")
		}
	}

	if secrettext.ContainsJWT(body) {
		return []byte("<output redacted: contained a JWT-shaped token>")
	}

	return body
}
