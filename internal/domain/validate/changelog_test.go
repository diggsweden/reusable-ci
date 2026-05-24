// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/domain/validate"
)

func TestCountLines_Cases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		given string
		want  int
	}{
		{"empty", "", 0},
		{"single_no_trailing_newline", "single", 1},
		{"single_with_trailing_newline", "single\n", 1},
		{"three_no_trailing_newline", "a\nb\nc", 3},
		{"three_with_trailing_newline", "a\nb\nc\n", 3},
		{"two_blank_lines", "\n\n", 2},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, validate.CountLines([]byte(testCase.given)))
		})
	}
}
