// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package ghaenv sets up tempfile-backed $GITHUB_OUTPUT and
// $GITHUB_STEP_SUMMARY for tests that exercise GitHub-Actions-style
// outputs. Replaces the bats `common_setup_with_github_env` helper.
//
// The package also exposes readers for the emitted output, including
// support for the multiline heredoc protocol used by ci_output_multiline.
package ghaenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
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

// Setup writes empty $GITHUB_OUTPUT and $GITHUB_STEP_SUMMARY tempfiles inside
// t.TempDir() and exports the corresponding GitHub Actions env vars.
func Setup(t *testing.T) *Env {
	t.Helper()
	env := testenv.New(t)
	dir := env.MkdirAll("ghaenv")

	outPath := filepath.Join(dir, "output")
	sumPath := filepath.Join(dir, "summary")

	for _, p := range []string{outPath, sumPath} {
		if err := os.WriteFile(p, nil, 0o644); err != nil { //nolint:gosec // test infra; p is t.TempDir()-based.
			t.Fatalf("ghaenv: create %q: %v", p, err)
		}
	}

	env.Setenv("GITHUB_ACTIONS", "true")
	env.Setenv("GITHUB_OUTPUT", outPath)
	env.Setenv("GITHUB_STEP_SUMMARY", sumPath)

	return &Env{t: t, OutputPath: outPath, SummaryPath: sumPath}
}

// Output reads the latest value emitted under `key` if it is scalar (key=value).
// Returns empty string if the key is absent or its latest declaration is multiline.
// For multiline values written via the heredoc protocol, use Multiline.
func (e *Env) Output(key string) string {
	e.t.Helper()

	return e.readOutput(key, false)
}

// Multiline reads the latest value for `key` if it is a heredoc. Returns
// the full block as a single string with embedded newlines, or empty
// string if `key` is absent or its latest declaration is scalar.
//
// Heredoc shape (per GitHub Actions):
//
//	<key><<<delimiter>
//	<line>
//	<line>
//	<delimiter>
func (e *Env) Multiline(key string) string {
	e.t.Helper()

	return e.readOutput(key, true)
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

//nolint:cyclop // Walk both output forms without treating heredoc payload as declarations.
func (e *Env) readOutput(key string, multiline bool) string {
	e.t.Helper()

	data, err := os.ReadFile(e.OutputPath)
	if err != nil {
		e.t.Fatalf("ghaenv: read output: %v", err)
	}

	var value string

	lines := strings.SplitAfter(string(data), "\n")
	for index := 0; index < len(lines); index++ {
		line := outputLine(lines[index])
		equals := strings.Index(line, "=")
		heredoc := strings.Index(line, "<<")
		// The first separator determines the declaration type; separators in
		// scalar values and heredoc payloads are just data.
		if equals >= 0 && (heredoc < 0 || equals < heredoc) {
			if line[:equals] == key {
				value = ""
				if !multiline {
					value = line[equals+1:]
				}
			}

			continue
		}

		if heredoc < 0 {
			continue
		}

		name, delim := line[:heredoc], line[heredoc+2:]
		if name == "" || delim == "" {
			e.t.Fatalf("ghaenv: invalid heredoc declaration %q", line)
		}

		start := index + 1

		index = start
		for index < len(lines) && outputLine(lines[index]) != delim {
			index++
		}

		if index == len(lines) {
			e.t.Fatalf("ghaenv: heredoc for %q never closed (delim=%q)", name, delim)
		}

		if name == key {
			value = ""
			if multiline {
				value = outputLine(strings.Join(lines[start:index], ""))
			}
		}
	}

	return value
}

// Strip only a terminating LF or CRLF; a lone terminal CR is payload data.
func outputLine(line string) string {
	if before, ok := strings.CutSuffix(line, "\n"); ok {
		return strings.TrimSuffix(before, "\r")
	}

	return line
}
