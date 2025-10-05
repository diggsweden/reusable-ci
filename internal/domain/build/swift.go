// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
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
		_, _ = fmt.Fprintln(&b, "## Swift Format ✓")
		_, _ = fmt.Fprintln(&b, "")
		_, _ = fmt.Fprintln(&b, "All Swift files are properly formatted.")
	case SwiftLintFailed:
		_, _ = fmt.Fprintln(&b, "## Swift Format Issues 🔴")
		_, _ = fmt.Fprintln(&b, "")
		writeFenced(&b, output)
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
		_, _ = fmt.Fprintln(&b, "## SwiftLint ✓")
		_, _ = fmt.Fprintln(&b, "")
		_, _ = fmt.Fprintln(&b, "No SwiftLint violations found.")
	case SwiftLintFailed:
		_, _ = fmt.Fprintln(&b, "## SwiftLint Issues 🔴")
		_, _ = fmt.Fprintln(&b, "")
		writeFenced(&b, output)
	case SwiftLintWarned:
		_, _ = fmt.Fprintln(&b, "## SwiftLint Warnings ⚠️")
		_, _ = fmt.Fprintln(&b, "")
		writeFenced(&b, output)
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
// failed run: at least one enabled linter's row reads Fail. That is every
// result other than success and skipped, so a cancelled or unrecognised
// result fails the run as its row says, rather than showing Fail and exiting
// zero.
func SwiftLintAggregateFailed(in SwiftLintSummaryInput) bool {
	return swiftLinterFailed(in.SwiftFormatEnabled, in.SwiftFormatResult) ||
		swiftLinterFailed(in.SwiftLintEnabled, in.SwiftLintResult)
}

func swiftLinterFailed(enabled bool, result string) bool {
	return enabled && result != "success" && result != "skipped"
}

func swiftLinterRow(enabled bool, result string) string {
	switch {
	case !enabled:
		return "🔸 Disabled"
	case swiftLinterFailed(enabled, result):
		return "✗ Fail"
	case result == "success":
		return "✓ Pass"
	default:
		return "− Skipped"
	}
}

// writeStripped writes s to b, ensuring it ends with exactly one newline.
// Mirrors `cat output.txt` behavior where the file already ends
// in a newline; without this every Markdown fence picks up a stray blank
// line.
// writeFenced writes linter output as a fenced code block whose fence is
// longer than any run of backticks inside it.
//
// The fence used to be a fixed "```", so output containing a line of three
// backticks -- a Swift string literal, a doc comment, a file path chosen by
// whoever opened the pull request -- closed the block early and everything
// after it rendered as Markdown in the step summary. CommonMark closes a fence
// only on a run at least as long as the opener, so a longer opener keeps the
// output literal without altering a byte of it.
func writeFenced(b *strings.Builder, output string) { //nolint:varnamelen // b is the builder, as in every renderer in this package.
	fence := summary.CodeFence(output)

	b.WriteString(fence)
	b.WriteByte('\n')
	b.WriteString(strings.TrimRight(output, "\n"))
	b.WriteByte('\n')
	b.WriteString(fence)
	b.WriteByte('\n')
}
