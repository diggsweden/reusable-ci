// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"fmt"
	"strings"
)

// SwiftLintOutcome is the result of one Swift linter invocation.
type SwiftLintOutcome string

const (
	// SwiftLintPassed indicates the linter exited 0.
	SwiftLintPassed SwiftLintOutcome = "passed"
	// SwiftLintWarned indicates the linter exited non-zero AND the workflow
	// allows warnings (FAIL_ON_WARNING=false). Only used by SwiftLint —
	// swift-format has no concept of "warning that's not a failure".
	SwiftLintWarned SwiftLintOutcome = "warned"
	// SwiftLintFailed indicates the linter exited non-zero AND must fail the step.
	SwiftLintFailed SwiftLintOutcome = "failed"
	// SwiftLintNoFiles indicates swift-format found no Swift files matching
	// the configured pattern. Treated as success.
	SwiftLintNoFiles SwiftLintOutcome = "no_files"
)

// RenderSwiftFormatBlock returns the markdown block written to
// $GITHUB_STEP_SUMMARY for a swift-format invocation.
func RenderSwiftFormatBlock(outcome SwiftLintOutcome, output string) string {
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	switch outcome {
	case SwiftLintPassed:
		_, _ = fmt.Fprintln(&b, "## Swift Format ✅")
		_, _ = fmt.Fprintln(&b, "")
		_, _ = fmt.Fprintln(&b, "All Swift files are properly formatted.")
	case SwiftLintFailed:
		_, _ = fmt.Fprintln(&b, "## Swift Format Issues 🔴")
		_, _ = fmt.Fprintln(&b, "")
		_, _ = fmt.Fprintln(&b, "```")
		writeStripped(&b, output)
		_, _ = fmt.Fprintln(&b, "```")
	case SwiftLintNoFiles:
		_, _ = fmt.Fprintln(&b, "## Swift Format ⊘")
		_, _ = fmt.Fprintln(&b, "")
		_, _ = fmt.Fprintln(&b, "No Swift files matched the configured pattern; nothing to lint.")
	case SwiftLintWarned:
		// swift-format has no warning level — never produced for this
		// outcome. Block left empty so the caller emits nothing.
	}

	return b.String()
}

// RenderSwiftLintBlock returns the markdown block written to
// $GITHUB_STEP_SUMMARY for a SwiftLint invocation.
func RenderSwiftLintBlock(outcome SwiftLintOutcome, output string) string {
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	switch outcome {
	case SwiftLintPassed:
		_, _ = fmt.Fprintln(&b, "## SwiftLint ✅")
		_, _ = fmt.Fprintln(&b, "")
		_, _ = fmt.Fprintln(&b, "No SwiftLint violations found.")
	case SwiftLintFailed:
		_, _ = fmt.Fprintln(&b, "## SwiftLint Issues 🔴")
		_, _ = fmt.Fprintln(&b, "")
		_, _ = fmt.Fprintln(&b, "```")
		writeStripped(&b, output)
		_, _ = fmt.Fprintln(&b, "```")
	case SwiftLintWarned:
		_, _ = fmt.Fprintln(&b, "## SwiftLint Warnings ⚠️")
		_, _ = fmt.Fprintln(&b, "")
		_, _ = fmt.Fprintln(&b, "```")
		writeStripped(&b, output)
		_, _ = fmt.Fprintln(&b, "```")
	case SwiftLintNoFiles:
		// SwiftLint walks the directory tree itself — there is no
		// caller-side "no files" path. Block left empty so the caller
		// emits nothing.
	}

	return b.String()
}

// SwiftLintSummaryInput drives RenderSwiftLintSummary.
type SwiftLintSummaryInput struct {
	SwiftFormatEnabled bool
	SwiftFormatResult  string // GHA needs result string: success | failure | skipped | cancelled
	SwiftLintEnabled   bool
	SwiftLintResult    string
}

// RenderSwiftLintSummary returns the aggregate table written by the
// summary job. Byte-for-byte compatible with the bash output so any
// pinned screenshots / changelog excerpts still match.
func RenderSwiftLintSummary(in SwiftLintSummaryInput) string {
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintln(&b, "## Swift Linting Summary")
	_, _ = fmt.Fprintln(&b, "")
	_, _ = fmt.Fprintln(&b, "| Linter | Status |")
	_, _ = fmt.Fprintln(&b, "|--------|--------|")
	_, _ = fmt.Fprintf(&b, "| swift-format | %s |\n", swiftLinterRow(in.SwiftFormatEnabled, in.SwiftFormatResult))
	_, _ = fmt.Fprintf(&b, "| SwiftLint | %s |\n", swiftLinterRow(in.SwiftLintEnabled, in.SwiftLintResult))

	if SwiftLintAggregateFailed(in) {
		_, _ = fmt.Fprintln(&b, "")
		_, _ = fmt.Fprintln(&b, "### ✗ Linting failed")
		_, _ = fmt.Fprintln(&b, "Please fix the issues above.")
	}

	return b.String()
}

// SwiftLintAggregateFailed reports whether the summary should signal a
// failed run — at least one enabled linter has result "failure".
func SwiftLintAggregateFailed(in SwiftLintSummaryInput) bool {
	return (in.SwiftFormatEnabled && in.SwiftFormatResult == "failure") ||
		(in.SwiftLintEnabled && in.SwiftLintResult == "failure")
}

func swiftLinterRow(enabled bool, result string) string {
	if !enabled {
		return "🔸 Disabled"
	}

	switch result {
	case "success":
		return "✓ Pass"
	case "skipped":
		return "− Skipped"
	default:
		return "✗ Fail"
	}
}

// writeStripped writes s to b, ensuring it ends with exactly one newline.
// Mirrors `cat output.txt` behavior where the file already ends
// in a newline; without this every Markdown fence picks up a stray blank
// line.
func writeStripped(b *strings.Builder, s string) {
	trimmed := strings.TrimRight(s, "\n")
	b.WriteString(trimmed)
	b.WriteByte('\n')
}
