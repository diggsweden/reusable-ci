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

// TestSwiftLintSummary covers the app-layer wrapper, which had no test.
// Both halves it composes are covered in domain/build; what is only true
// here is the ordering — the table is appended before the failure is
// returned, so an operator still sees which linter failed on a run that
// exits non-zero.
func TestSwiftLintSummary(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		in      appsummary.SwiftLintSummaryInput
		wantErr bool
	}{
		{
			name: "both pass",
			in:   appsummary.SwiftLintSummaryInput{SwiftFormatEnabled: true, SwiftFormatResult: "success", SwiftLintEnabled: true, SwiftLintResult: "success"},
		},
		{
			name:    "an enabled linter failed",
			in:      appsummary.SwiftLintSummaryInput{SwiftFormatEnabled: true, SwiftFormatResult: "failure", SwiftLintEnabled: true, SwiftLintResult: "success"},
			wantErr: true,
		},
		{
			name:    "the other enabled linter failed",
			in:      appsummary.SwiftLintSummaryInput{SwiftLintEnabled: true, SwiftLintResult: "failure"},
			wantErr: true,
		},
		{
			// A failure from a linter nobody switched on is not this
			// release's problem.
			name: "failure from a disabled linter",
			in:   appsummary.SwiftLintSummaryInput{SwiftFormatEnabled: false, SwiftFormatResult: "failure"},
		},
		{
			name: "neither enabled",
			in:   appsummary.SwiftLintSummaryInput{},
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
			// is the point: the summary explains the non-zero exit.
			if !strings.Contains(sink.buf.String(), "Swift") {
				t.Errorf("no summary written: %q", sink.buf.String())
			}
		})
	}
}
