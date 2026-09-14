// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// Report paths are explicit caller destinations, not a confinement root. They
// must nevertheless be distinct regular files with real directory parents.
func validateReportPaths(paths ...string) error { //nolint:cyclop // spelling, parent, type and identity checks must all precede retirement/publication.
	seen := make(map[string]bool, len(paths))

	var files []os.FileInfo

	for _, name := range paths {
		if strings.HasSuffix(name, string(filepath.Separator)) {
			return fmt.Errorf("scan report path must name a file, not end with a directory separator: %w", errs.ErrUsage)
		}

		absolute, err := filepath.Abs(name)
		if err != nil {
			return err
		}

		if seen[absolute] {
			return fmt.Errorf("scan report paths must be distinct: %w", errs.ErrUsage)
		}

		seen[absolute] = true

		root, err := pathsafe.OpenRoot(filepath.Dir(absolute))
		if err != nil {
			return fmt.Errorf("report directory: %w: %w", err, errs.ErrMissingInput)
		}

		info, statErr := root.Lstat(filepath.Base(absolute))
		_ = root.Close()

		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}

		if statErr != nil {
			return statErr
		}

		if !info.Mode().IsRegular() {
			return fmt.Errorf("scan report path must be a nonlinked regular file: %w", errs.ErrValidation)
		}

		for _, other := range files {
			if os.SameFile(info, other) {
				return fmt.Errorf("scan report paths alias the same file: %w", errs.ErrUsage)
			}
		}

		files = append(files, info)
	}

	return nil
}
