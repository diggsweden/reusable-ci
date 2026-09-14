// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// TestSwiftLintSummary_ErrorsWhenAnEnabledLinterFailed covers the app-layer wrapper, which had no test.
// Both halves it composes are covered in domain/build; what is only true
// here is the ordering — the table is appended before the failure is
// returned, so an operator still sees which linter failed on a run that
// exits non-zero.
func TestSwiftLintSummary_ErrorsWhenAnEnabledLinterFailed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		in       appsummary.SwiftLintSummaryInput
		wantErr  bool
		wantRows []string
	}{
		{
			name:     "both pass",
			in:       appsummary.SwiftLintSummaryInput{SwiftFormatEnabled: true, SwiftFormatResult: "success", SwiftLintEnabled: true, SwiftLintResult: "success"},
			wantRows: []string{"| swift-format | ✓ Pass |", "| SwiftLint | ✓ Pass |"},
		},
		{
			name:     "an enabled linter failed",
			in:       appsummary.SwiftLintSummaryInput{SwiftFormatEnabled: true, SwiftFormatResult: "failure", SwiftLintEnabled: true, SwiftLintResult: "success"},
			wantErr:  true,
			wantRows: []string{"| swift-format | ✗ Fail |", "| SwiftLint | ✓ Pass |", "### ✗ Linting failed"},
		},
		{
			name:     "the other enabled linter failed",
			in:       appsummary.SwiftLintSummaryInput{SwiftLintEnabled: true, SwiftLintResult: "failure"},
			wantErr:  true,
			wantRows: []string{"| swift-format | 🔸 Disabled |", "| SwiftLint | ✗ Fail |", "### ✗ Linting failed"},
		},
		{
			// A failure from a linter nobody switched on is not this
			// release's problem -- and the row says Disabled, not Fail.
			name:     "failure from a disabled linter",
			in:       appsummary.SwiftLintSummaryInput{SwiftFormatEnabled: false, SwiftFormatResult: "failure"},
			wantRows: []string{"| swift-format | 🔸 Disabled |", "| SwiftLint | 🔸 Disabled |"},
		},
		{
			name:     "neither enabled",
			in:       appsummary.SwiftLintSummaryInput{},
			wantRows: []string{"| swift-format | 🔸 Disabled |", "| SwiftLint | 🔸 Disabled |"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := &fakeSummarySink{}

			err := appsummary.SwiftLintSummary(context.Background(), sink, tc.in)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}

			if tc.wantErr && !errors.Is(err, errs.ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}

			// The table is written either way. On the failing runs this
			// is the point: the summary explains the non-zero exit, so the
			// assertion is on the heading and a real row -- a bare "Swift"
			// was already satisfied by the heading alone, whatever the
			// table below it said.
			body := sink.buf.String()
			if !strings.Contains(body, "## Swift Linting Summary") {
				t.Errorf("no summary written: %q", body)
			}

			for _, want := range tc.wantRows {
				if !strings.Contains(body, want) {
					t.Errorf("missing row %q in:\n%s", want, body)
				}
			}
		})
	}
}

// TestSwiftLintSummary_RowAndExitAgreeForEveryResult pins the policy the
// table and the exit share. An enabled linter whose result is cancelled,
// missing or unrecognised used to render "✗ Fail" while the command exited
// zero, so the summary and the gate contradicted each other. Every enabled
// result now renders exactly one of Pass, Skipped or Fail, and the command
// fails, with the failure heading, exactly when a row reads Fail. Disabled
// linters stay neutral whatever their result.
func TestSwiftLintSummary_RowAndExitAgreeForEveryResult(t *testing.T) {
	t.Parallel()

	for result, row := range map[string]string{
		"success": "✓ Pass", "skipped": "− Skipped", "failure": "✗ Fail", "cancelled": "✗ Fail", "": "✗ Fail", "timed_out": "✗ Fail",
	} {
		for _, enabled := range []bool{true, false} {
			sink := &fakeSummarySink{}
			err := appsummary.SwiftLintSummary(t.Context(), sink, appsummary.SwiftLintSummaryInput{
				SwiftFormatEnabled: true, SwiftFormatResult: "success", SwiftLintEnabled: enabled, SwiftLintResult: result,
			})

			wantRow, wantFail := row, row == "✗ Fail"
			if !enabled {
				wantRow, wantFail = "🔸 Disabled", false
			}

			body := sink.buf.String()
			if !strings.Contains(body, "| SwiftLint | "+wantRow+" |\n") {
				t.Errorf("result %q enabled=%v: row missing %q in:\n%s", result, enabled, wantRow, body)
			}

			if (err != nil) != wantFail || strings.Contains(body, "### ✗ Linting failed") != wantFail {
				t.Errorf("result %q enabled=%v: err = %v, heading = %v, want failure %v", result, enabled, err, strings.Contains(body, "### ✗ Linting failed"), wantFail)
			}

			if wantFail && !errors.Is(err, errs.ErrValidation) {
				t.Errorf("result %q: err = %v, want ErrValidation", result, err)
			}
		}
	}
}
