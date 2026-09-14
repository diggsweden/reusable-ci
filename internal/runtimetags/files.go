// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package runtimetags

import (
	"fmt"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
	"io/fs"
	"os"
	"path/filepath"
)

// Files is the shared shipped surface for checking and rewriting runtime pins.
func Files(root string) ([]string, error) {
	var files []string

	for _, surface := range []string{".github/workflows", "templates"} {
		handle, err := pathsafe.OpenRoot(filepath.Join(root, surface))
		if err != nil {
			return nil, err
		}

		count := 0
		err = fs.WalkDir(handle.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("runtime surface contains symlink: %s/%s: %w", surface, path, errs.ErrValidation)
			}

			if !entry.IsDir() && (filepath.Ext(path) == ".yml" || filepath.Ext(path) == ".yaml") {
				files = append(files, filepath.Join(surface, path))
				count++
			}

			return nil
		})
		_ = handle.Close()

		if err != nil {
			return nil, err
		}

		if count == 0 {
			return nil, fmt.Errorf("runtime surface contains no YAML: %s: %w", surface, errs.ErrMissingInput)
		}
	}

	return files, nil
}
