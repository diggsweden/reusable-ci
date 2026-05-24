// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package version holds pure version-related helpers: branch-name
// sanitisation, dev-version composition, latest-semver-tag selection.
//
// File mutation per project type (Maven `versions:set`, npm `version`,
// gradle/cargo sed) lives in app/version, not here — those need the
// real toolchain or filesystem and aren't pure.
package version

import "strings"

// SanitizePathToken maps any character outside [a-zA-Z0-9._-] to '-' and
// strips leading/trailing dashes. The result is safe for filesystem paths,
// Docker/OCI tags, and artefact basenames.
//
// Idempotent: sanitising an already-clean token returns it unchanged.
func SanitizePathToken(in string) string {
	if in == "" {
		return ""
	}

	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	b.Grow(len(in))

	for i := range len(in) {
		c := in[i] //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '.', c == '_', c == '-':
			b.WriteByte(c)
		default:
			b.WriteByte('-')
		}
	}

	out := b.String()
	out = strings.TrimLeft(out, "-")
	out = strings.TrimRight(out, "-")

	return out
}
