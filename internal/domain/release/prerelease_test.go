// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// TestIsPrereleaseTag_AnySuffixMarksAPrerelease was named KnownIdentifiers,
// which implied the function checks the suffix against the canonical list. It
// does not, deliberately: any valid SemVer prerelease must be published as a
// prerelease, and whether the suffix is canonical is a separate warning-level
// policy (IsCanonicalPrerelease). The custom-suffix row is the one the old name
// contradicted.
func TestIsPrereleaseTag_AnySuffixMarksAPrerelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		given string
		want  bool
	}{
		{"stable_release_is_not", "v1.0.0", false},
		{"alpha_is_pre", "v1.0.0-alpha", true},
		{"beta_with_number_is_pre", "v1.0.0-beta.1", true},
		{"rc_with_number_is_pre", "v1.0.0-rc.2", true},
		{"dev_suffix_is_pre", "v1.0.0-dev", true},
		{"snapshot_uppercase_is_pre", "v1.0.0-SNAPSHOT", true},
		{"snapshot_lowercase_is_pre", "v1.0.0-snapshot", true},
		{"non_canonical_suffix_is_still_pre", "v1.0.0-custom", true},
		{"partial_version_is_not", "v2.0", false},
		{"empty_is_not", "", false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, release.IsPrereleaseTag(testCase.given))
		})
	}
}

// TestIsCanonicalPrerelease_OneIdentifierAndAnOptionalNumber pins the policy
// the tag check defers to, which had no test of its own. A canonical
// prerelease is one listed identifier, optionally followed by one purely
// numeric component; anything else is valid SemVer that earns a naming warning.
func TestIsCanonicalPrerelease_OneIdentifierAndAnOptionalNumber(t *testing.T) {
	t.Parallel()

	for prerelease, want := range map[string]bool{
		"alpha":      true,
		"rc.2":       true,
		"SNAPSHOT":   true,
		"beta.10":    true,
		"custom":     false,
		"rc.":        false,
		"rc.1.2":     false,
		"rc.x":       false,
		"rc.-1":      false,
		"Alpha":      false,
		"":           false,
		"alpha-beta": false,
	} {
		if got := release.IsCanonicalPrerelease(prerelease); got != want {
			t.Errorf("IsCanonicalPrerelease(%q) = %v, want %v", prerelease, got, want)
		}
	}
}
