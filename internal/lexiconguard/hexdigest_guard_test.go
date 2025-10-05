// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package lexiconguard

import "testing"

// bareSHA256HexPattern is the bare "64 lowercase hex" regex — a sha256 with no
// `sha256:` prefix. The caret sits directly before the hex class, which the
// digest-pinned-ref patterns (`…:[0-9a-f]{64}$`, `^sha256:[0-9a-f]{64}$`) never
// do, so this literal is a precise sentinel for the bare form alone.
const bareSHA256HexPattern = "^[0-9a-f]{64}$"

// TestBareSHA256HexLiteralIsSingleSourced rejects direct regexp calls restating
// the canonical bare digest pattern. It ignores comments and inert string data;
// see singleSource.regexpCalls for the bounded constant-resolution scope. This
// source guard is not proof that all runtime digest checks use the canonical API.
func TestBareSHA256HexLiteralIsSingleSourced(t *testing.T) {
	t.Parallel()

	singleSource{
		patterns:    []string{bareSHA256HexPattern},
		regexpCalls: true,
		owners: []string{
			"internal/domain/container/ref.go",
		},
	}.requireSingleSourced(t,
		"bare sha256-hex regex re-declared outside domain/container; call container.ValidSHA256Hex instead")
}
