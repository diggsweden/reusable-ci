// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
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

func TestNormalizeJobStatus_FailClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want summary.Result
	}{
		// GitHub Actions job.status spellings.
		{"success", summary.ResultSuccess},
		{"failure", summary.ResultFailure},
		{"cancelled", summary.ResultCancelled},
		{"skipped", summary.ResultSkipped},
		// GitLab CI_JOB_STATUS spellings.
		{"failed", summary.ResultFailure},
		{"canceled", summary.ResultCancelled},
		// Fail-closed: unknown / empty must NOT become skipped (which would
		// hide a failed job from the stage gate) — they become failure.
		{"", summary.ResultFailure},
		{"unknown", summary.ResultFailure},
		{"running", summary.ResultFailure},
	}
	for _, tc := range tests {
		t.Run(tc.in+"-->"+string(tc.want), func(t *testing.T) {
			t.Parallel()

			if got := summary.NormalizeJobStatus(tc.in); got != tc.want {
				t.Errorf("NormalizeJobStatus(%q) = %q, want %q", tc.in, got, tc.want)
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
