// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package swiftformat_test

import (
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/swiftformat"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
)

// TestLint_ArgvShape pins the invocation. -s is strict mode, which is
// what makes swift-format exit non-zero on a finding instead of merely
// printing it, and the files are passed individually rather than as a
// directory so only the tracked Swift sources are read.
func TestLint_ArgvShape(t *testing.T) {
	m := mockbinary.New(t) //nolint:varnamelen // idiomatic short name.
	m.Add("swift-format", `printf '%s\n' "$*"`)

	a := &swiftformat.Adapter{Bin: m.Path("swift-format")}

	out, code, err := a.Lint(context.Background(), t.TempDir(), []string{"a.swift", "b.swift"})
	if err != nil || code != 0 {
		t.Fatalf("err = %v, code = %d", err, code)
	}

	if got, want := strings.TrimSpace(out), "lint -s a.swift b.swift"; got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

// TestLint_ExitCodeIsReturnedNotRaised mirrors the swiftlint adapter: a
// non-zero exit means findings and comes back as a code with a nil error;
// only a binary that cannot run is an error. The two adapters feed the
// same app-layer logic, so they have to agree on this.
func TestLint_ExitCodeIsReturnedNotRaised(t *testing.T) {
	m := mockbinary.New(t)
	m.Add("swift-format", `printf 'a.swift:1:1: error: bad\n'; exit 1`)

	a := &swiftformat.Adapter{Bin: m.Path("swift-format")}

	out, code, err := a.Lint(context.Background(), t.TempDir(), []string{"a.swift"})
	if err != nil {
		t.Fatalf("findings must not be an error: %v", err)
	}

	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}

	if !strings.Contains(out, "error: bad") {
		t.Errorf("output not captured: %q", out)
	}
}

func TestLint_MissingBinaryIsAnError(t *testing.T) {
	a := &swiftformat.Adapter{Bin: t.TempDir() + "/does-not-exist"}

	_, code, err := a.Lint(context.Background(), t.TempDir(), []string{"a.swift"})
	if err == nil {
		t.Fatal("a missing binary must be an error, not a lint result")
	}

	if code != -1 {
		t.Errorf("code = %d, want -1", code)
	}
}
