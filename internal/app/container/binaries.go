// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/listval"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
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
// Every source and destination is checked before the first rename.
//
//nolint:cyclop // renames per (variant, ext, platform) combination.
func SuffixExtractedBinaries(w io.Writer, in SuffixExtractedBinariesInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.Dir == "" {
		return fmt.Errorf("directory is required: pass --dir <path> or set $EXTRACTED_BINARIES_DIR: %w", errs.ErrUsage)
	}

	switch in.Arch {
	case "":
		return fmt.Errorf("architecture is required: pass --arch <amd64|arm64> or set $ARCH: %w", errs.ErrUsage)
	case domaincontainer.ArchAMD64, domaincontainer.ArchARM64:
	default:
		return fmt.Errorf("unsupported architecture %q; expected amd64 or arm64: %w", in.Arch, errs.ErrValidation)
	}

	root, err := pathsafe.OpenRoot(in.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("directory not found: %s: %w", in.Dir, errs.ErrMissingInput)
	}

	if err != nil {
		return err
	}

	defer func() { _ = root.Close() }()

	var names []string

	if in.ExpectedNames != "" {
		for _, n := range listval.Tokens(in.ExpectedNames) {
			t := strings.TrimSpace(n)
			if t == "" {
				continue
			}

			names = append(names, t)
		}
	} else {
		entries, err := fs.ReadDir(root.FS(), ".")
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

	if err := preflightBinaryNames(root, names, in.Arch); err != nil {
		return err
	}

	for _, name := range names {
		newName := name + "-linux-" + in.Arch
		if err := root.Rename(name, newName); err != nil {
			return fmt.Errorf("rename %s -> %s: %w", name, newName, err)
		}

		_, _ = fmt.Fprintf(w, "renamed %s -> %s\n", name, newName)
	}

	return nil
}

func preflightBinaryNames(root *os.Root, names []string, arch string) error {
	var missing []string

	seen := make(map[string]bool, len(names))

	for _, name := range names {
		if !fs.ValidPath(name) || strings.ContainsAny(name, "/\\") || seen[name] {
			return fmt.Errorf("expected binary names must be unique plain basenames: %w", errs.ErrValidation)
		}

		seen[name] = true

		info, statErr := root.Lstat(name)
		if errors.Is(statErr, os.ErrNotExist) {
			missing = append(missing, name)

			continue
		}

		if statErr != nil {
			return statErr
		}

		if !info.Mode().IsRegular() {
			return fmt.Errorf("binary %s is not a regular file: %w", name, errs.ErrValidation)
		}

		if _, destErr := root.Lstat(name + "-linux-" + arch); !errors.Is(destErr, os.ErrNotExist) {
			return fmt.Errorf("binary destination already exists or cannot be inspected: %s: %w", name, errs.ErrValidation)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("expected extracted binaries missing: %s: %w", strings.Join(missing, ", "), errs.ErrMissingInput)
	}

	return nil
}
