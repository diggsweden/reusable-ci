// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// SuffixExtractedBinariesInput drives SuffixExtractedBinaries.
type SuffixExtractedBinariesInput struct {
	// Dir is the directory containing the binaries.
	Dir string
	// Arch is the arch token appended after `linux-` (e.g. amd64, arm64).
	Arch string
	// ExpectedNames is a comma-separated list of binary basenames to
	// rename. Empty → every top-level file in Dir.
	ExpectedNames string
}

// SuffixExtractedBinaries renames each binary in Dir to
// `<name>-linux-<arch>` using the workflow binary-extraction artifact naming
// contract.
//
// Files that don't exist (when ExpectedNames is set) are silently
// skipped.
//
//nolint:cyclop // renames per (variant, ext, platform) combination.
func SuffixExtractedBinaries(w io.Writer, in SuffixExtractedBinariesInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.Dir == "" {
		return fmt.Errorf("directory is required: pass --dir <path> or set $EXTRACTED_BINARIES_DIR: %w", errs.ErrUsage)
	}

	if in.Arch == "" {
		return fmt.Errorf("architecture is required: pass --arch <amd64|arm64|…> or set $ARCH: %w", errs.ErrUsage)
	}

	info, err := os.Stat(in.Dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("directory not found: %s: %w", in.Dir, errs.ErrMissingInput)
	}

	var names []string

	if in.ExpectedNames != "" {
		for _, n := range strings.Split(in.ExpectedNames, ",") {
			t := strings.TrimSpace(n)
			if t == "" {
				continue
			}

			names = append(names, t)
		}
	} else {
		entries, err := os.ReadDir(in.Dir)
		if err != nil {
			return fmt.Errorf("read %s: %w", in.Dir, err)
		}

		for _, e := range entries {
			if e.IsDir() {
				continue
			}

			names = append(names, e.Name())
		}
	}

	var missing []string

	for _, name := range names {
		old := filepath.Join(in.Dir, name)
		if _, err := os.Stat(old); err != nil {
			if in.ExpectedNames != "" && os.IsNotExist(err) {
				missing = append(missing, name)
			}

			continue
		}

		newName := name + "-linux-" + in.Arch

		newPath := filepath.Join(in.Dir, newName)
		if err := os.Rename(old, newPath); err != nil {
			return fmt.Errorf("rename %s → %s: %w", old, newPath, err)
		}

		_, _ = fmt.Fprintf(w, "renamed %s -> %s\n", name, newName)
	}

	if len(missing) > 0 {
		return fmt.Errorf("expected extracted binaries missing: %s: %w", strings.Join(missing, ", "), errs.ErrMissingInput)
	}

	return nil
}
