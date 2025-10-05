// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package lexiconguard

import "testing"

// commitSHAPattern is the full-length git-commit-hash regex (40 or 64 lowercase
// hex). Before consolidation this shape had drifted into strict (`40|64`) and
// loose (`40,64`) spellings across the provenance, sign-and-publish, and
// signer-image paths.
const commitSHAPattern = "^([0-9a-f]{40}|[0-9a-f]{64})$"

// TestCommitSHARegexIsSingleSourced keeps the commit-SHA shape check
// single-sourced in domain/git (git.ValidCommitSHA), the way the bare
// sha256-hex check is single-sourced in domain/container. Callers validate a
// commit SHA through git.ValidCommitSHA rather than re-compiling the pattern.
func TestCommitSHALiteralIsSingleSourced(t *testing.T) {
	t.Parallel()

	singleSource{
		patterns: []string{commitSHAPattern},
		owners: []string{
			"internal/lexiconguard/commitsha_guard_test.go",
			"internal/domain/git/sha.go",
		},
	}.requireSingleSourced(t,
		"commit-SHA regex re-declared outside domain/git; call git.ValidCommitSHA instead")
}
