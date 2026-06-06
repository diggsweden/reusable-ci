// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// Shared helpers used by the per-ecosystem prerequisite validators
// (`validate cargo`, `validate jvm-reproducibility`, and any future
// per-ecosystem check that needs to walk an arbitrary working
// directory). Kept in their own file so they read as utilities, not
// as a single validator's internals.

// safeWorkingDir is the run-time defensive sibling of
// config.validateWorkingDirectory — the parse-time check runs once when
// the artifacts.yml is loaded, this one runs every time a validate-*
// command is invoked with an arbitrary plan-json (direct CLI callers
// don't necessarily route through config.Validate first). Returns the
// cleaned path so callers can pass it straight to filepath.Join.
//
// Ecosystem-neutral error wording so JVM + Cargo callers surface
// consistent messages.
func safeWorkingDir(dir string) (string, error) {
	if dir == "" {
		dir = "."
	}

	if filepath.IsAbs(dir) {
		return "", fmt.Errorf("working directory %q must be relative: %w", dir, errs.ErrInvalidConfig)
	}

	clean := filepath.Clean(dir)

	cleanSlash := filepath.ToSlash(clean)
	if cleanSlash == ".." || strings.HasPrefix(cleanSlash, "../") {
		return "", fmt.Errorf("working directory %q escapes the workspace: %w", dir, errs.ErrInvalidConfig)
	}

	return clean, nil
}

// fileExists returns true when path is a regular file (not a directory)
// and is readable. Errors are swallowed — callers treat "doesn't exist"
// and "permission denied" identically (the validator's contract is "did
// the project ship the expected file?", not "could we open it?").
func fileExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && !info.IsDir()
}

// displayDir maps the empty / `.` directory to "repo root" for human-
// readable error messages. Any other value is returned unchanged.
func displayDir(dir string) string {
	if dir == "" || dir == "." {
		return "repo root"
	}

	return dir
}
