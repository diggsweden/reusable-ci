// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/summary"
)

func TestNormalizeResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want summary.Result
	}{
		{"success", summary.ResultSuccess}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"failure", summary.ResultFailure},
		{"cancelled", summary.ResultCancelled},
		{"skipped", summary.ResultSkipped}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"", summary.ResultSkipped},
		{"unknown", summary.ResultSkipped},
		{"SUCCESS", summary.ResultSkipped}, // case-sensitive — bash matches lowercase only
		{"weird-thing", summary.ResultSkipped},
	}
	for _, tc := range tests {
		t.Run(tc.in+"-->"+string(tc.want), func(t *testing.T) {
			t.Parallel()

			if got := summary.NormalizeResult(tc.in); got != tc.want {
				t.Errorf("NormalizeResult(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestStatusIcon(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"success":   "✓",
		"skipped":   "−",
		"failure":   "✗",
		"cancelled": "✗",
		"":          "✗",
		"weird":     "✗",
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			t.Parallel()

			if got := summary.StatusIcon(in); got != want {
				t.Errorf("StatusIcon(%q) = %q, want %q", in, got, want)
			}
		})
	}
}
