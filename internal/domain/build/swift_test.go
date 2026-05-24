// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
)

func TestRenderSwiftFormatBlock(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		outcome build.SwiftLintOutcome
		body    string
		want    []string
		notWant []string
	}{
		{name: "passed", outcome: build.SwiftLintPassed, want: []string{"## Swift Format ✅", "properly formatted"}},
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

func TestRenderSwiftLintBlock(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		outcome build.SwiftLintOutcome
		body    string
		want    []string
	}{
		{name: "passed", outcome: build.SwiftLintPassed, want: []string{"## SwiftLint ✅", "No SwiftLint violations"}},
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

func TestRenderSwiftLintSummary(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   build.SwiftLintSummaryInput
		want []string
	}{
		{
			name: "both pass",
			in:   build.SwiftLintSummaryInput{SwiftFormatEnabled: true, SwiftFormatResult: "success", SwiftLintEnabled: true, SwiftLintResult: "success"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			want: []string{"## Swift Linting Summary", "| swift-format | ✓ Pass |", "| SwiftLint | ✓ Pass |"},
		},
		{
			name: "swift-format fails — final block fires",
			in:   build.SwiftLintSummaryInput{SwiftFormatEnabled: true, SwiftFormatResult: "failure", SwiftLintEnabled: true, SwiftLintResult: "success"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			want: []string{"| swift-format | ✗ Fail |", "### ✗ Linting failed"},
		},
		{
			name: "swift-format disabled",
			in:   build.SwiftLintSummaryInput{SwiftFormatEnabled: false, SwiftLintEnabled: true, SwiftLintResult: "skipped"},
			want: []string{"| swift-format | 🔸 Disabled |", "| SwiftLint | − Skipped |"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := build.RenderSwiftLintSummary(tc.in)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in:\n%s", want, got)
				}
			}
		})
	}
}

func TestSwiftLintAggregateFailed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   build.SwiftLintSummaryInput
		want bool
	}{
		{name: "neither enabled", in: build.SwiftLintSummaryInput{}, want: false},
		{name: "swift-format failed enabled", in: build.SwiftLintSummaryInput{SwiftFormatEnabled: true, SwiftFormatResult: "failure"}, want: true},
		{name: "swiftlint failed enabled", in: build.SwiftLintSummaryInput{SwiftLintEnabled: true, SwiftLintResult: "failure"}, want: true},
		{name: "failed but disabled", in: build.SwiftLintSummaryInput{SwiftFormatEnabled: false, SwiftFormatResult: "failure"}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := build.SwiftLintAggregateFailed(tc.in); got != tc.want {
				t.Errorf("got = %v, want %v", got, tc.want)
			}
		})
	}
}
