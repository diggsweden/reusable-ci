// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
)

func TestRenderSwiftFormatBlock_RendersPassFailAndNoFilesBlocks(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		outcome build.SwiftLintOutcome
		body    string
		want    []string
	}{
		{name: "passed", outcome: build.SwiftLintPassed, want: []string{"## Swift Format ✓", "properly formatted"}},
		{name: "failed", outcome: build.SwiftLintFailed, body: "main.swift:3:1: warning: line is too long", want: []string{"## Swift Format Issues 🔴", "```", "main.swift:3:1"}},
		{name: "no files", outcome: build.SwiftLintNoFiles, want: []string{"## Swift Format ⊘", "nothing to lint"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := build.RenderSwiftFormatBlock(tc.outcome, tc.body)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in:\n%s", want, got)
				}
			}
		})
	}
}

func TestRenderSwiftLintBlock_RendersPassFailAndWarnBlocks(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		outcome build.SwiftLintOutcome
		body    string
		want    []string
	}{
		{name: "passed", outcome: build.SwiftLintPassed, want: []string{"## SwiftLint ✓", "No SwiftLint violations"}},
		{name: "failed", outcome: build.SwiftLintFailed, body: "violation 1", want: []string{"## SwiftLint Issues 🔴", "violation 1"}},
		{name: "warned", outcome: build.SwiftLintWarned, body: "warning 1", want: []string{"## SwiftLint Warnings ⚠️", "warning 1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := build.RenderSwiftLintBlock(tc.outcome, tc.body)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in:\n%s", want, got)
				}
			}
		})
	}
}

func TestRenderSwiftLintSummary_TabulatesBothToolsAndFlagsFailure(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		in     build.SwiftLintSummaryInput
		want   []string
		failed bool
	}{
		{
			name: "both pass",
			in:   build.SwiftLintSummaryInput{SwiftFormatEnabled: true, SwiftFormatResult: "success", SwiftLintEnabled: true, SwiftLintResult: "success"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			want: []string{"## Swift Linting Summary", "| swift-format | ✓ Pass |", "| SwiftLint | ✓ Pass |"},
		},
		{
			name:   "swift-format fails — final block fires",
			in:     build.SwiftLintSummaryInput{SwiftFormatEnabled: true, SwiftFormatResult: "failure", SwiftLintEnabled: true, SwiftLintResult: "success"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			want:   []string{"| swift-format | ✗ Fail |", "### ✗ Linting failed"},
			failed: true,
		},
		{name: "swiftlint fails", in: build.SwiftLintSummaryInput{SwiftFormatEnabled: true, SwiftFormatResult: "success", SwiftLintEnabled: true, SwiftLintResult: "failure"}, want: []string{"| swift-format | ✓ Pass |", "| SwiftLint | ✗ Fail |"}, failed: true},
		{name: "format disabled failure", in: build.SwiftLintSummaryInput{SwiftFormatResult: "failure", SwiftLintEnabled: true, SwiftLintResult: "success"}, want: []string{"| swift-format | 🔸 Disabled |", "| SwiftLint | ✓ Pass |"}},
		{name: "swiftlint disabled failure", in: build.SwiftLintSummaryInput{SwiftFormatEnabled: true, SwiftFormatResult: "success", SwiftLintResult: "failure"}, want: []string{"| swift-format | ✓ Pass |", "| SwiftLint | 🔸 Disabled |"}},
		{
			name: "swift-format disabled",
			in:   build.SwiftLintSummaryInput{SwiftFormatEnabled: false, SwiftLintEnabled: true, SwiftLintResult: "skipped"},
			want: []string{"| swift-format | 🔸 Disabled |", "| SwiftLint | − Skipped |"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := build.RenderSwiftLintSummary(tc.in)
			if strings.Contains(got, "### ✗ Linting failed") != tc.failed || strings.Contains(got, "Please fix the issues above.") != tc.failed {
				t.Errorf("incorrect aggregate block:\n%s", got)
			}

			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in:\n%s", want, got)
				}
			}
		})
	}
}

func TestSwiftLintAggregateFailed_IsTrueForAnEnabledToolThatNeitherPassedNorSkipped(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   build.SwiftLintSummaryInput
		want bool
	}{
		{name: "neither enabled", in: build.SwiftLintSummaryInput{}, want: false},
		{name: "format successful enabled", in: build.SwiftLintSummaryInput{SwiftFormatEnabled: true, SwiftFormatResult: "success"}, want: false},
		{name: "swiftlint successful enabled", in: build.SwiftLintSummaryInput{SwiftLintEnabled: true, SwiftLintResult: "success"}, want: false},
		{name: "swift-format failed enabled", in: build.SwiftLintSummaryInput{SwiftFormatEnabled: true, SwiftFormatResult: "failure"}, want: true},
		{name: "swiftlint failed enabled", in: build.SwiftLintSummaryInput{SwiftLintEnabled: true, SwiftLintResult: "failure"}, want: true},
		{name: "failed but disabled", in: build.SwiftLintSummaryInput{SwiftFormatEnabled: false, SwiftFormatResult: "failure"}, want: false},
		{name: "swiftlint failed but disabled", in: build.SwiftLintSummaryInput{SwiftLintResult: "failure"}, want: false},
		{name: "swift-format skipped enabled", in: build.SwiftLintSummaryInput{SwiftFormatEnabled: true, SwiftFormatResult: "skipped"}, want: false},
		{name: "swift-format cancelled enabled", in: build.SwiftLintSummaryInput{SwiftFormatEnabled: true, SwiftFormatResult: "cancelled"}, want: true},
		{name: "swiftlint result missing enabled", in: build.SwiftLintSummaryInput{SwiftLintEnabled: true}, want: true},
		{name: "swiftlint result unrecognised enabled", in: build.SwiftLintSummaryInput{SwiftLintEnabled: true, SwiftLintResult: "Success"}, want: true},
		{name: "cancelled but disabled", in: build.SwiftLintSummaryInput{SwiftFormatResult: "cancelled", SwiftLintResult: ""}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := build.SwiftLintAggregateFailed(tc.in); got != tc.want {
				t.Errorf("got = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSwiftDiagnosticBlocks_ExactFraming(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ body, want string }{{"", "\n"}, {"one", "one\n"}, {"one\n\n", "one\n"}, {"one\nsecond\n\n", "one\nsecond\n"}} {
		for _, block := range []struct{ got, heading string }{
			{build.RenderSwiftFormatBlock(build.SwiftLintFailed, tc.body), "## Swift Format Issues 🔴"},
			{build.RenderSwiftLintBlock(build.SwiftLintFailed, tc.body), "## SwiftLint Issues 🔴"},
			{build.RenderSwiftLintBlock(build.SwiftLintWarned, tc.body), "## SwiftLint Warnings ⚠️"},
		} {
			want := block.heading + "\n\n```\n" + tc.want + "```\n"
			if block.got != want {
				t.Errorf("body=%q got=%q want=%q", tc.body, block.got, want)
			}
		}
	}
}

// TestRenderSwiftBlocks_OutputCannotCloseTheFence feeds linter output that
// contains backtick runs. The fence used to be a fixed "```", so a line of
// three backticks in the output -- a Swift string literal or doc comment is
// enough -- closed the block, and the canary heading after it rendered as part
// of the step summary. The output must survive byte for byte inside one block.
func TestRenderSwiftBlocks_OutputCannotCloseTheFence(t *testing.T) {
	t.Parallel()

	output := "Sources/A.swift:3: warning\n```\n## B8-INJECTED\n````\ntrailing"

	for name, block := range map[string]string{
		"format failure": build.RenderSwiftFormatBlock(build.SwiftLintFailed, output),
		"lint failure":   build.RenderSwiftLintBlock(build.SwiftLintFailed, output),
		"lint warnings":  build.RenderSwiftLintBlock(build.SwiftLintWarned, output),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// The longest run inside is four, so the fence must be five.
			const fence = "`````\n"

			start := strings.Index(block, fence)
			end := strings.LastIndex(block, fence)

			if start < 0 || end <= start {
				t.Fatalf("no enclosing fence longer than the output's backtick runs:\n%s", block)
			}

			if got := block[start+len(fence) : end]; got != output+"\n" {
				t.Errorf("fenced content = %q, want the output verbatim", got)
			}

			if strings.Count(block, fence) != 2 {
				t.Errorf("expected exactly one opening and one closing fence:\n%s", block)
			}
		})
	}

	// Ordinary output keeps the ordinary three-backtick fence.
	if got := build.RenderSwiftLintBlock(build.SwiftLintFailed, "plain\n"); !strings.Contains(got, "```\nplain\n```\n") {
		t.Errorf("plain output changed its fence: %q", got)
	}
}
