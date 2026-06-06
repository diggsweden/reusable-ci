// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package safeexec

import (
	"bytes"
	"regexp"
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
}

// jwtPattern matches a three-segment base64url-without-padding token —
// the compact JWS (JWT) shape. Detects OAuth bearer tokens that future
// subprocesses might echo on error. The minimum-length floor (≥20
// base64url chars per segment) avoids matching short coincidental
// dot-separated strings like file paths or version numbers.
//
//nolint:gochecknoglobals // immutable compiled regexp.
var jwtPattern = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}`)

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

	if jwtPattern.Match(body) {
		return []byte("<output redacted: contained a JWT-shaped token>")
	}

	return body
}
