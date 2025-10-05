// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package lexiconguard

import "testing"

// TestStrictSemverTagRegexIsSingleSourced keeps runtime semantic-version
// validation on domain/version's x/mod parser. Regexes may locate version-like
// text, but must not become a second strict release-tag validator.
//
// The two patterns are legacy notations of the strict v-prefixed release-tag
// shape. Runtime callers now use ParseSemver, ParseSemverTag, or
// IsStableSemverTag; composed regexes may still locate candidate text but must
// not redeclare either complete anchored validator.
func TestStrictSemverTagLiteralIsSingleSourced(t *testing.T) {
	t.Parallel()

	singleSource{
		patterns: []string{
			`^v\d+\.\d+\.\d+$`,
			`^v[0-9]+[.][0-9]+[.][0-9]+$`,
		},
		owners: []string{"internal/lexiconguard/semvertag_guard_test.go"},
	}.requireSingleSourced(t,
		"strict semver-tag regex re-declared; use domain/version's x/mod-backed parser")
}
