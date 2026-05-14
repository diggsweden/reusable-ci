// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/domain/validate"
)

func TestIsSnapshot_RecognisesSuffix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		given string
		want  bool
	}{
		{"plain_release_is_not", "v1.0.0", false},
		{"snapshot_uppercase", "v1.0.0-SNAPSHOT", true},
		{"snapshot_lowercase", "v1.0.0-snapshot", true},
		{"snapshot_titlecase", "v1.0.0-Snapshot", true},
		{"suffix_only_check", "v2.0-snapshot", true},
		{"bare_snapshot_is_not", "snapshot", false},
		{"empty_is_not", "", false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, validate.IsSnapshot(testCase.given))
		})
	}
}
