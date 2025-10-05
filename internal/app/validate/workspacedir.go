// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
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

	root, err := pathsafe.OpenRoot(clean)
	if err != nil {
		return "", fmt.Errorf("open prerequisite working directory: %w: %w", err, errs.ErrInvalidConfig)
	}

	_ = root.Close()

	return clean, nil
}

// plannedWorkingDirs returns each artifact's working directory once, in plan
// order, after every one has passed the safety check.
func plannedWorkingDirs(artifacts []pipeline.PlannedArtifact) ([]string, error) {
	var dirs []string

	for _, artifact := range artifacts {
		dir, err := safeWorkingDir(artifact.WorkingDirectory)
		if err != nil {
			return nil, err
		}

		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}

	return dirs, nil
}

// fileExists returns true when path is a regular file (not a directory)
// and is readable. Errors are swallowed — callers treat "doesn't exist"
// and "permission denied" identically (the validator's contract is "did
// the project ship the expected file?", not "could we open it?").
func fileExists(path string) bool {
	_, err := readWorkspaceFile(path)

	return err == nil
}

func readWorkspaceFile(path string) ([]byte, error) {
	root, err := pathsafe.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	info, err := root.Lstat(filepath.Base(path))
	if err != nil {
		return nil, err
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("prerequisite manifest must be a nonlinked regular file: %w", errs.ErrValidation)
	}

	return cliio.ReadFileInRoot(root, filepath.Base(path))
}

func workflowReadError(path string, err error) error {
	kind := errs.ErrMalformedInput
	if errors.Is(err, os.ErrNotExist) {
		kind = errs.ErrMissingInput
	} else if errors.Is(err, os.ErrPermission) {
		kind = errs.ErrPermissionDenied
	}

	return fmt.Errorf("read workflow %s: %w: %w", path, err, kind)
}

// displayDir maps the empty / `.` directory to "repo root" for human-
// readable error messages. Any other value is returned unchanged.
func displayDir(dir string) string {
	if dir == "" || dir == "." {
		return "repo root"
	}

	return dir
}
