// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
)

// TestUnsafeCosignErrorLine covers the filter that decides which cosign
// stderr lines may be echoed into a CI log when image verification fails.
// It had no test at all.
func TestUnsafeCosignErrorLine(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		line   string
		unsafe bool
	}{
		// Held back: the five markers, matched anywhere in the line and
		// case-insensitively, since cosign's wording is not ours to
		// depend on.
		{name: "authorization header", line: `GET /v2/ HTTP/1.1 Authorization: Basic dXNlcjpwYXNz`, unsafe: true},
		{name: "uppercase marker", line: "AUTHORIZATION denied", unsafe: true},
		{name: "bearer scheme", line: "unexpected status: Bearer realm=https://ghcr.io/token", unsafe: true},
		{name: "the word token", line: "failed to exchange token for scope", unsafe: true},
		{name: "password", line: "invalid password supplied", unsafe: true},
		{name: "secret", line: "reading secret from env", unsafe: true},
		{name: "marker mid-word", line: "x-access-token=abc", unsafe: true},

		// Let through: the ordinary diagnostics an operator needs.
		{name: "signature mismatch", line: "error: no matching signatures", unsafe: false},
		{name: "missing attestation", line: "no matching attestations found for cyclonedx", unsafe: false},
		{name: "identity mismatch", line: `certificate identity "x" does not match ^https://github.com/org/`, unsafe: false},
		{name: "empty", line: "", unsafe: false},
		{name: "plain digest", line: "verifying sha256:0123456789abcdef", unsafe: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := container.UnsafeCosignErrorLine(tc.line); got != tc.unsafe {
				t.Errorf("UnsafeCosignErrorLine(%q) = %v, want %v", tc.line, got, tc.unsafe)
			}
		})
	}
}

// TestUnsafeCosignErrorLine_DenylistMisses records what a five-word
// denylist cannot see. These lines carry credential material and are
// echoed verbatim, because none of them contains "authorization",
// "bearer", "token", "password" or "secret".
//
// The filter is a last line of defence on an error path, not the primary
// control, so this is a limit rather than a defect -- see
// docs/open-questions.md.
func TestUnsafeCosignErrorLine_DenylistMisses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, line string }{
		{name: "credentials embedded in a URL", line: "GET https://alice:ghp_ABCDEFGHIJKLMNOP@ghcr.io/v2/org/app/manifests/1.0"}, //nolint:gosec // G101: a fake credential in a fixture; the point of the test is that this line is not filtered.
		{name: "basic-auth blob without the header name", line: "auth failed for dXNlcjpodW50ZXIy"},
		{name: "raw JWT", line: "rejected eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIn0.sig"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if container.UnsafeCosignErrorLine(tc.line) {
				t.Errorf("line is now filtered — the denylist has been widened; update docs/open-questions.md: %q", tc.line)
			}
		})
	}
}
