// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/domain/release"
)

func TestIsPrereleaseTag_KnownIdentifiers(t *testing.T) {
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
		{"non_canonical_suffix_is_not", "v1.0.0-custom", false},
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
