// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package ghaenv sets up tempfile-backed $GITHUB_OUTPUT and
// $GITHUB_STEP_SUMMARY for tests that exercise GitHub-Actions-style
// outputs. Replaces the bats `common_setup_with_github_env` helper.
//
// The package also exposes readers for the emitted output, including
// support for the multiline heredoc protocol used by ci_output_multiline.
package ghaenv

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
)

// Env wraps the tempfile paths and provides typed readers.
type Env struct {
	t           *testing.T
	OutputPath  string
	SummaryPath string
}

// Setenv forwards to the underlying test within the already-isolated env.
func (e *Env) Setenv(key, value string) {
	e.t.Helper()
	e.t.Setenv(key, value)
}

// Setup writes empty $GITHUB_OUTPUT and $GITHUB_STEP_SUMMARY tempfiles
// inside t.TempDir() and exports the corresponding env vars.
//
// It also exports CI_OUTPUT/CI_SUMMARY pointing at the same files so the
// platform-portable ci helpers (in scripts/ci/output.sh today, in
// internal/adapters/ghaoutput later) work the same.
func Setup(t *testing.T) *Env {
	t.Helper()
	env := testenv.New(t)
	dir := env.MkdirAll("ghaenv")

	outPath := filepath.Join(dir, "output")
	sumPath := filepath.Join(dir, "summary")

	for _, p := range []string{outPath, sumPath} {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatalf("ghaenv: create %q: %v", p, err)
		}
	}

	env.Setenv("GITHUB_ACTIONS", "true")
	env.Setenv("GITHUB_OUTPUT", outPath)
	env.Setenv("GITHUB_STEP_SUMMARY", sumPath)
	env.Setenv("CI_OUTPUT", outPath)
	env.Setenv("CI_SUMMARY", sumPath)

	return &Env{t: t, OutputPath: outPath, SummaryPath: sumPath}
}

// Output reads the scalar value emitted under `key` (i.e. lines of
// the form `key=value`). Returns empty string if the key is not present.
// For multiline values written via the heredoc protocol, use Multiline.
func (e *Env) Output(key string) string {
	e.t.Helper()
	f, err := os.Open(e.OutputPath)
	if err != nil {
		e.t.Fatalf("ghaenv: open output: %v", err)
	}
	defer func() { _ = f.Close() }()

	prefix := key + "="
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	if err := sc.Err(); err != nil {
		e.t.Fatalf("ghaenv: scan output: %v", err)
	}
	return ""
}

// Multiline reads the heredoc-style multiline value for `key`. Returns
// the full block as a single string with embedded newlines, or empty
// string if `key` was not written as a multiline output.
//
// Heredoc shape (per GitHub Actions):
//
//	<key><<<delimiter>
//	<line>
//	<line>
//	<delimiter>
func (e *Env) Multiline(key string) string {
	e.t.Helper()
	f, err := os.Open(e.OutputPath)
	if err != nil {
		e.t.Fatalf("ghaenv: open output: %v", err)
	}
	defer func() { _ = f.Close() }()

	prefix := key + "<<"
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, prefix) {
			delim := strings.TrimPrefix(line, prefix)
			var lines []string
			for sc.Scan() {
				inner := sc.Text()
				if inner == delim {
					return strings.Join(lines, "\n")
				}
				lines = append(lines, inner)
			}
			e.t.Fatalf("ghaenv: heredoc for %q never closed (delim=%q)", key, delim)
		}
	}
	if err := sc.Err(); err != nil {
		e.t.Fatalf("ghaenv: scan output: %v", err)
	}
	return ""
}

// Summary returns the full step-summary content as a single string.
func (e *Env) Summary() string {
	e.t.Helper()
	data, err := os.ReadFile(e.SummaryPath)
	if err != nil {
		e.t.Fatalf("ghaenv: read summary: %v", err)
	}
	return string(data)
}
