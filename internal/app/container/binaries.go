// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

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
// `<name>-linux-<arch>`. Mirrors
// scripts/container/suffix-extracted-binaries.sh.
//
// Files that don't exist (when ExpectedNames is set) are silently
// skipped — matches the bash `[[ -f ... ]]` guard.
func SuffixExtractedBinaries(stdout io.Writer, in SuffixExtractedBinariesInput) error {
	if in.Dir == "" {
		return fmt.Errorf("DIR is required: %w", errs.ErrUsage)
	}
	if in.Arch == "" {
		return fmt.Errorf("ARCH is required: %w", errs.ErrUsage)
	}
	info, err := os.Stat(in.Dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("directory not found: %s", in.Dir)
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

	for _, name := range names {
		old := filepath.Join(in.Dir, name)
		if _, err := os.Stat(old); err != nil {
			continue
		}
		newName := name + "-linux-" + in.Arch
		newPath := filepath.Join(in.Dir, newName)
		if err := os.Rename(old, newPath); err != nil {
			return fmt.Errorf("rename %s → %s: %w", old, newPath, err)
		}
		fmt.Fprintf(stdout, "renamed %s -> %s\n", name, newName)
	}
	return nil
}
