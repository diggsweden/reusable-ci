// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// XcodeListBuiltArtifacts prints the .ipa / .xcarchive paths under
// build/.
func XcodeListBuiltArtifacts(w io.Writer) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	paths, err := mobileArtifactPaths("build", true)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(w, "Built artifacts:")
	for _, path := range paths {
		_, _ = fmt.Fprintln(w, filepath.Join("build", path))
	}

	if len(paths) == 0 {
		_, _ = fmt.Fprintln(w, "No artifacts found")
	}

	return nil
}

// Discover the full set before printing any publication paths. The opened
// root and walker both reject links; archives must be directories, apps files.
func mobileArtifactPaths(directory string, archives bool) ([]string, error) { //nolint:cyclop // rooted walk distinguishes links, application files and archive directories before output.
	root, err := pathsafe.OpenRoot(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	defer func() { _ = root.Close() }()

	var paths []string

	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("mobile artifact tree contains a symlink: %w", errs.ErrValidation)
		}

		ext := strings.ToLower(filepath.Ext(path))
		if archives && ext == ".xcarchive" {
			if !entry.IsDir() {
				return fmt.Errorf("xcarchive must be a directory: %w", errs.ErrValidation)
			}

			paths = append(paths, filepath.FromSlash(path))

			return fs.SkipDir
		}

		if (archives && ext == ".ipa") || (!archives && (ext == ".apk" || ext == ".aab")) {
			if !entry.Type().IsRegular() {
				return fmt.Errorf("mobile application artifact must be a regular file: %w", errs.ErrValidation)
			}

			paths = append(paths, filepath.FromSlash(path))
		}

		return nil
	})

	return paths, err
}
