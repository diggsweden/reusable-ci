// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package lexiconguard

import "testing"

// prefixedSHA256DigestPattern is the canonical OCI content digest regex — the
// `sha256:` prefix anchored directly against 64 hex characters. The caret sits
// before the prefix, which the composed digest-pinned-ref patterns
// (`…@sha256:[0-9a-f]{64}$`) never do, so this literal is a precise sentinel for
// the standalone digest form alone.
const prefixedSHA256DigestPattern = "^sha256:[0-9a-f]{64}$"

// TestPrefixedSHA256DigestLiteralIsSingleSourced rejects direct regexp calls
// restating the canonical prefixed digest pattern, with the same syntax-aware
// scope and limitations as the bare-hex guard. It does not grade inert examples
// as validators or prove that every runtime validator uses container.ValidDigest.
func TestPrefixedSHA256DigestLiteralIsSingleSourced(t *testing.T) {
	t.Parallel()

	singleSource{
		patterns:    []string{prefixedSHA256DigestPattern},
		regexpCalls: true,
		owners: []string{
			"internal/domain/container/ref.go",
		},
	}.requireSingleSourced(t,
		"canonical sha256-digest regex re-declared outside domain/container; call container.ValidDigest instead")
}
