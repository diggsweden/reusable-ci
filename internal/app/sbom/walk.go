// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom

// Filesystem walk primitives used by the per-project artifact-discovery
// helpers in discovery.go. Each walker mirrors a bash `find …` expression.

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// walkMatching returns every regular file under root where predicate
// returns true. Returns nil when root doesn't exist.
func walkMatching(ws workspace, root string, predicate func(basename string) bool) ([]string, error) {
	if err := validateWalkRoot(ws, root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}

		return nil, err
	}

	var out []string

	err := fs.WalkDir(ws.fsys, cleanFSPath(root), func(path string, d fs.DirEntry, err error) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if info.Mode().IsRegular() && predicate(d.Name()) {
			out = append(out, path)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk SBOM inputs under %s: %w", root, err)
	}

	return out, nil
}

// walkExecutable returns regular files whose owner-executable bit is set.
func walkExecutable(ws workspace, root string) ([]string, error) {
	return walkExecutableWithFilter(ws, root, nil)
}

// listExecutableNoDebugInfo returns the executable regular files directly
// under root, excluding *.d files (cargo's debug-info outputs). It does not
// recurse: cargo puts the release binaries directly under target/release,
// while the executables further down — build/*/build-script-build, deps/* —
// are cargo's own build scripts and intermediates, which a recursive walk
// scanned as release artifacts.
func listExecutableNoDebugInfo(ws workspace, root string) ([]string, error) {
	if err := validateWalkRoot(ws, root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}

		return nil, err
	}

	entries, err := fs.ReadDir(ws.fsys, cleanFSPath(root))
	if err != nil {
		return nil, fmt.Errorf("list executable SBOM inputs under %s: %w", root, err)
	}

	var out []string

	for _, entry := range entries {
		keep, entryErr := keepExecutableEntry(entry, func(name string) bool { return !strings.HasSuffix(name, ".d") })
		if entryErr != nil {
			return nil, entryErr
		}

		if keep {
			out = append(out, path.Join(cleanFSPath(root), entry.Name()))
		}
	}

	return out, nil
}

// keepExecutableEntry reports whether a walked entry is an executable regular
// file the SBOM scanner should read. Split out of the walk closure so the
// "which files count" rule reads on its own, and so the walk itself is only
// about walking.
func keepExecutableEntry(d fs.DirEntry, filter func(name string) bool) (bool, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if d.IsDir() {
		return false, nil
	}

	info, err := d.Info()
	if err != nil {
		return false, err //nolint:wrapcheck // surfaced verbatim by the walker.
	}

	// 0o100 = owner-executable bit, like find -executable.
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o100 == 0 {
		return false, nil
	}

	return filter == nil || filter(d.Name()), nil
}

func walkExecutableWithFilter(ws workspace, root string, filter func(name string) bool) ([]string, error) {
	if err := validateWalkRoot(ws, root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}

		return nil, err
	}

	var out []string

	err := fs.WalkDir(ws.fsys, cleanFSPath(root), func(path string, d fs.DirEntry, err error) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if err != nil {
			return err
		}

		keep, entryErr := keepExecutableEntry(d, filter)
		if entryErr != nil {
			return entryErr
		}

		if keep {
			out = append(out, path)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk executable SBOM inputs under %s: %w", root, err)
	}

	return out, nil
}

func validateWalkRoot(ws workspace, root string) error {
	info, err := ws.lstat(root)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return fmt.Errorf("SBOM search root %s is not a real directory: %w", root, errs.ErrValidation)
	}

	return nil
}
