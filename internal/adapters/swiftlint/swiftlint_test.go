// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package swiftlint_test

import (
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/swiftlint"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// TestLint_ExitCodeIsReturnedNotRaised covers the contract the app layer
// depends on: a non-zero exit means swiftlint found something, and comes
// back as a code with a nil error. Only a failure to run at all is an
// error. Conflating the two turns "the linter found three warnings" into
// "the linter crashed".
func TestLint_ExitCodeIsReturnedNotRaised(t *testing.T) {
	for _, tc := range []struct {
		name     string
		script   string
		wantCode int
		wantErr  bool
	}{
		{
			name:     "clean run",
			script:   `printf 'no violations\n'; exit 0`,
			wantCode: 0,
		},
		{
			// swiftlint exits 2 when it finds serious violations.
			name:     "violations found",
			script:   `printf 'file.swift:1: warning: bad\n'; exit 2`,
			wantCode: 2,
		},
		{
			name:     "some other non-zero",
			script:   `printf 'boom\n'; exit 70`,
			wantCode: 70,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name.
			m.Add("swiftlint", tc.script)

			a := &swiftlint.Adapter{Bin: m.Path("swiftlint")}

			out, code, err := a.Lint(context.Background(), swiftlint.LintInput{Dir: t.TempDir()})
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}

			if code != tc.wantCode {
				t.Errorf("code = %d, want %d", code, tc.wantCode)
			}

			// Output is captured either way, so the app layer can show
			// the findings alongside the code.
			if out == "" {
				t.Error("output was not captured")
			}
		})
	}
}

// TestLint_MissingBinaryIsAnError separates the other half: a binary that
// cannot run at all yields -1 and an error, not a lint verdict.
func TestLint_MissingBinaryIsAnError(t *testing.T) {
	a := &swiftlint.Adapter{Bin: t.TempDir() + "/does-not-exist"}

	_, code, err := a.Lint(context.Background(), swiftlint.LintInput{Dir: t.TempDir()})
	if err == nil {
		t.Fatal("a missing binary must be an error, not a lint result")
	}

	if code != -1 {
		t.Errorf("code = %d, want -1", code)
	}
}

// TestLint_ArgvShape pins the reporter and the optional config. The
// github-actions-logging reporter is what makes findings appear as
// annotations rather than plain log lines.
func TestLint_ArgvShape(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		want   string
	}{
		{name: "no config", want: "lint --reporter github-actions-logging"},
		{name: "with config", config: ".swiftlint.yml", want: "lint --config .swiftlint.yml --reporter github-actions-logging"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mockbinary.New(t)
			m.Add("swiftlint", `printf '%s\n' "$*"`)

			a := &swiftlint.Adapter{Bin: m.Path("swiftlint")}

			out, _, err := a.Lint(context.Background(), swiftlint.LintInput{Dir: t.TempDir(), ConfigPath: tc.config})
			if err != nil {
				t.Fatal(err)
			}

			if got := strings.TrimSpace(out); got != tc.want {
				t.Errorf("argv = %q, want %q", got, tc.want)
			}
		})
	}
}
