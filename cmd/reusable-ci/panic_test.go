// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestFormatPanic_IncludesValueStackContextAndURL(t *testing.T) {
	var buf bytes.Buffer

	stack := []byte("goroutine 1 [running]:\nmain.run(...)\n\t/src/cmd/reusable-ci/main.go:99\n")

	formatPanic(&buf, "nil pointer dereference",
		stack,
		"1.2.3", "abc1234",
		[]string{"reusable-ci", "version", "bump", "--project-type=npm", "--version=1.0.0"}) //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.

	out := buf.String()

	for _, want := range []string{
		"reusable-ci: internal error: nil pointer dereference",
		"goroutine 1 [running]:",
		"Context: version=1.2.3  commit=abc1234  command=reusable-ci version bump --project-type=npm --version=1.0.0",
		"This is a bug in reusable-ci — please report it at:",
		bugReportURL,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("formatPanic output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestFormatPanic_URLIsLast(t *testing.T) {
	// clig.dev §Errors: "Consider where the user will look first. Put
	// the most important information at the end of the output." The
	// issue-tracker URL is the action we want operators to take after a
	// panic, so it must be the last non-empty line.
	var buf bytes.Buffer
	formatPanic(&buf, "boom", []byte("stack"), "v", "c", []string{"reusable-ci"})

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")

	last := lines[len(lines)-1]
	if !strings.Contains(last, bugReportURL) {
		t.Errorf("last line should contain the bug-report URL, got %q", last)
	}
}

func TestFormatPanic_HandlesNonStringPanicValue(t *testing.T) {
	// %v handles any panic payload (error, struct, int, …). Smoke-test
	// that an error value flows through the same path.
	var buf bytes.Buffer
	formatPanic(&buf, sentinelError{msg: "structured failure"}, []byte("stack"), "v", "c", []string{"reusable-ci"})

	if !strings.Contains(buf.String(), "structured failure") {
		t.Errorf("formatPanic should render the panic value via %%v; got:\n%s", buf.String())
	}
}

// sentinelError is a tiny error type used only by the panic-value smoke test.
type sentinelError struct{ msg string }

func (e sentinelError) Error() string { return e.msg }
