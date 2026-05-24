// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"fmt"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// SwiftLintSummaryInput drives SwiftLintSummary.
type SwiftLintSummaryInput struct {
	SwiftFormatEnabled bool
	SwiftFormatResult  string // success | failure | skipped | cancelled
	SwiftLintEnabled   bool
	SwiftLintResult    string
}

// SwiftLintSummary writes the aggregate Swift lint table to the step
// summary and returns ErrValidation when at least one enabled linter
// reports "failure". Mirrors summary job in lint-swift.yml,
// including the exit-1 behavior.
func SwiftLintSummary(ctx context.Context, sink ci.SummarySink, in SwiftLintSummaryInput) error {
	body := build.RenderSwiftLintSummary(build.SwiftLintSummaryInput{
		SwiftFormatEnabled: in.SwiftFormatEnabled,
		SwiftFormatResult:  in.SwiftFormatResult,
		SwiftLintEnabled:   in.SwiftLintEnabled,
		SwiftLintResult:    in.SwiftLintResult,
	})
	if err := sink.Append(ctx, body); err != nil {
		return fmt.Errorf("append swift-lint summary: %w", err)
	}

	if build.SwiftLintAggregateFailed(build.SwiftLintSummaryInput{
		SwiftFormatEnabled: in.SwiftFormatEnabled,
		SwiftFormatResult:  in.SwiftFormatResult,
		SwiftLintEnabled:   in.SwiftLintEnabled,
		SwiftLintResult:    in.SwiftLintResult,
	}) {
		return fmt.Errorf("swift linting failed: %w", errs.ErrValidation)
	}

	return nil
}
