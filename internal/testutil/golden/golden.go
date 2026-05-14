// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package golden compares test output against checked-in fixtures.
//
// Tests call golden.Equal(t, "name", got) which reads testdata/golden/<name>.
// Run `go test -update ./...` to refresh golden files when the change is
// intentional. CI runs without -update and fails on diff.
//
// Each test that uses goldens should put its own testdata/golden/ directory
// next to the test file, following Go convention for testdata.
package golden

import (
	"flag"
	"os"
	"path/filepath"
)

var update = flag.Bool("update", false, "refresh golden files (test-only)")

// T is the slice of *testing.T that golden uses. Defining it as an interface
// lets package tests substitute a probe; production tests pass a real *testing.T.
type T interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
}

// Equal compares got with the contents of testdata/golden/<name>.
// On mismatch, t.Errorf is called. With -update, the golden file is rewritten.
//
// name is treated as a path relative to testdata/golden/ in the package's
// test directory.
func Equal(t T, name string, got []byte) {
	t.Helper()

	path := filepath.Join("testdata", "golden", name)

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("golden: mkdir %q: %v", filepath.Dir(path), err)
			return
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("golden: write %q: %v", path, err)
			return
		}
		t.Logf("golden: updated %q (%d bytes)", path, len(got))
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("golden: read %q: %v (run 'go test -update ./...' to create it)", path, err)
		return
	}

	if string(got) != string(want) {
		t.Errorf("golden mismatch for %q\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

// EqualString is a convenience wrapper for string outputs.
func EqualString(t T, name, got string) {
	t.Helper()
	Equal(t, name, []byte(got))
}
