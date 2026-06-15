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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

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

	body, err := os.ReadFile(path) //nolint:gosec // path is a CLI-flag value under operator control.
	if err != nil {
		return nil, classifyReadError(path, err)
	}

	return body, nil
}

// classifyReadError maps a filesystem read failure onto the project's
// typed sentinels so main()'s exit-code ladder reports the right sysexits
// code: a path the user pointed at that doesn't exist is their input
// error (EX_NOINPUT), an unreadable path is a permission error
// (EX_NOPERM), and a path that's a directory rather than a file is also
// an operator path mistake (EX_NOINPUT) — none is the internal-bug
// default (EX_SOFTWARE) the unclassified error would otherwise fall
// through to. The original os error message is preserved so the operator
// still sees the path.
func classifyReadError(path string, err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %w", err, errs.ErrMissingInput)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%w: %w", err, errs.ErrPermissionDenied)
	}

	// A path that exists but isn't a regular file (a directory, most
	// commonly) is the operator pointing the flag at the wrong thing, not
	// an internal bug.
	if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
		return fmt.Errorf("%q is a directory, not a file: %w", path, errs.ErrMissingInput)
	}

	return err
}

// WriteFile writes data to path, or — when path is exactly "-" —
// writes to stdout. Mirrors os.WriteFile's interface. The perm argument
// is ignored in the stdout case.
//
// File writes are atomic (see writeFileAtomic): a crash or kill mid-write
// never leaves a truncated file, so a retried CI step always finds either
// the previous complete artifact or the new one.
func WriteFile(path string, data []byte, perm fs.FileMode) error {
	if path == StdSentinel {
		if _, err := os.Stdout.Write(data); err != nil {
			return fmt.Errorf("write to stdout: %w", err)
		}

		return nil
	}

	return writeFileAtomic(path, data, perm)
}

// writeFileAtomic writes data to path crash-safely: it writes a sibling
// temp file, flushes it, and renames it over path. rename(2) is atomic on
// POSIX, so a crash, kill, or ENOSPC mid-write leaves either the previous
// complete file or the new complete file — never a half-written one that
// would block a re-run (e.g. a truncated ledger that no longer parses).
func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}

	// The temp must share the destination's directory (hence filesystem)
	// for the rename to be atomic.
	tmp, err := os.CreateTemp(dir, base+".tmp-*") //nolint:varnamelen // 'tmp' is the idiomatic name for the temp file.
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", path, err)
	}

	committed := false

	defer func() {
		if !committed {
			_ = os.Remove(tmp.Name())
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("write temp for %s: %w", path, err)
	}

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("chmod temp for %s: %w", path, err)
	}

	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("sync temp for %s: %w", path, err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp for %s: %w", path, err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("rename temp to %s: %w", path, err)
	}

	committed = true

	return nil
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
