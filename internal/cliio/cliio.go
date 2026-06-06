// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package cliio bridges file paths and stdin/stdout for CLI flags.
// CLI conventions (see clig.dev) say user-provided file paths should
// accept `-` to mean stdin (read) or stdout (write), so a pipe like
//
//	curl https://… | reusable-ci config validate --file -
//	reusable-ci publish npm write-npmrc --output - > ~/.npmrc
//
// works without a tempfile. This package exposes the minimal helpers
// to honour that convention.
//
// Use these for paths that come from a CLI flag or positional arg.
// Internal fixed paths (package.json, gradle.properties, etc.) should
// keep using os.ReadFile / os.WriteFile directly.
package cliio

import (
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// StdSentinel is the conventional "-" filename meaning stdin (when reading)
// or stdout (when writing).
const StdSentinel = "-"

// ReadFile returns the bytes at path, or — when path is exactly "-" —
// the contents of stdin read to EOF. Mirrors os.ReadFile's interface
// so call sites migrate by a single-line change.
//
// When path is "-" and stdin is a TTY (or /dev/null), ReadFile fails
// fast with errs.ErrUsage instead of hanging on terminal input — the
// clig.dev "don't hang on a TTY" rule.
func ReadFile(path string) ([]byte, error) {
	if path == StdSentinel {
		if StdinIsCharDevice() {
			return nil, fmt.Errorf("%q expects piped or redirected input, not a terminal: %w", path, errs.ErrUsage)
		}

		body, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read from stdin: %w", err)
		}

		return body, nil
	}

	return os.ReadFile(path) //nolint:gosec // path is a CLI-flag value under operator control.
}

// WriteFile writes data to path, or — when path is exactly "-" —
// writes to stdout. Mirrors os.WriteFile's interface. The perm argument
// is ignored in the stdout case.
func WriteFile(path string, data []byte, perm fs.FileMode) error {
	if path == StdSentinel {
		if _, err := os.Stdout.Write(data); err != nil {
			return fmt.Errorf("write to stdout: %w", err)
		}

		return nil
	}

	return os.WriteFile(path, data, perm)
}

// CreateWriter opens path for writing (O_CREATE|O_WRONLY|O_TRUNC), or —
// when path is exactly "-" — returns a writer that targets stdout. The
// returned io.WriteCloser must be Closed by the caller; the stdout-bound
// closer is a no-op, so a single `defer w.Close()` is correct in both
// modes.
func CreateWriter(path string, perm fs.FileMode) (io.WriteCloser, error) {
	if path == StdSentinel {
		return stdoutWriter{}, nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm) //nolint:gosec // path is a CLI-flag value; perm is caller-supplied.
	if err != nil {
		return nil, err
	}

	return f, nil
}

// stdoutWriter is a no-op-closing writer that targets os.Stdout. Using
// a typed wrapper (rather than an inline anonymous struct) keeps the
// CreateWriter return value reflectable in tests.
type stdoutWriter struct{}

func (stdoutWriter) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdoutWriter) Close() error                { return nil }
