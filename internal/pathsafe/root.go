// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pathsafe

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// OpenRoot opens an existing directory without accepting a symlink in the
// directory itself or any of its ancestors. Each component is opened from its
// already-open parent and identity-checked, binding validation to the returned
// descriptor rather than trusting a pathname after an Lstat check.
func OpenRoot(name string) (*os.Root, error) {
	return openRoot(name, 0, false)
}

// MkdirRoot creates missing path components and returns a descriptor-backed
// root. Existing symlink components are rejected rather than followed.
func MkdirRoot(name string, perm fs.FileMode) (*os.Root, error) {
	return openRoot(name, perm, true)
}

//nolint:cyclop // every path component is identity-checked around optional creation.
func openRoot(name string, perm fs.FileMode, create bool) (*os.Root, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("root path must be non-empty: %w", errs.ErrUsage)
	}

	abs, err := filepath.Abs(name)
	if err != nil {
		return nil, fmt.Errorf("resolve root %q: %w", name, err)
	}

	volumeRoot := filepath.VolumeName(abs) + string(filepath.Separator)

	rel, err := filepath.Rel(volumeRoot, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("resolve root %q from volume root: %w", name, errs.ErrValidation)
	}

	root, err := os.OpenRoot(volumeRoot)
	if err != nil {
		return nil, fmt.Errorf("open volume root for %q: %w", name, err)
	}

	if rel == "." {
		return root, nil
	}

	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		info, statErr := root.Lstat(component)
		if create && errors.Is(statErr, fs.ErrNotExist) {
			if mkdirErr := root.Mkdir(component, perm); mkdirErr != nil && !errors.Is(mkdirErr, fs.ErrExist) {
				_ = root.Close()

				return nil, fmt.Errorf("create root component %q in %q: %w", component, name, mkdirErr)
			}

			info, statErr = root.Lstat(component)
		}

		if statErr != nil {
			_ = root.Close()

			return nil, fmt.Errorf("inspect root component %q in %q: %w", component, name, statErr)
		}

		if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
			_ = root.Close()

			return nil, fmt.Errorf("root component %q in %q is not a real directory: %w", component, name, errs.ErrValidation)
		}

		child, openErr := root.OpenRoot(component)
		if openErr != nil {
			_ = root.Close()

			return nil, fmt.Errorf("open root component %q in %q: %w", component, name, openErr)
		}

		opened, openedErr := child.Stat(".")

		current, currentErr := root.Lstat(component)
		if openedErr != nil || currentErr != nil || current.Mode()&fs.ModeSymlink != 0 ||
			!current.IsDir() || !os.SameFile(info, opened) || !os.SameFile(current, opened) {
			_ = child.Close()
			_ = root.Close()

			return nil, fmt.Errorf("root component %q in %q changed while opening: %w", component, name, errs.ErrValidation)
		}

		_ = root.Close()
		root = child
	}

	return root, nil
}
