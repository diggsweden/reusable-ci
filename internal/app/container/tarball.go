// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/app/archive"
)

// ExtractNPMTarballInput drives ExtractNPMTarball.
type ExtractNPMTarballInput struct {
	// Dir is the directory containing the tarball. Empty → cwd.
	Dir string
}

// ExtractNPMTarball finds a *.tgz / *.tar.gz at the top of Dir,
// extracts it with `tar --strip-components=1` semantics, removes the
// tarball. Mirrors scripts/container/extract-npm-tarball.sh —
// behaves as a no-op when no tarball is present (the bash also has
// no `else` branch).
func ExtractNPMTarball(stdout io.Writer, in ExtractNPMTarballInput) error {
	dir := in.Dir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}
	tarball, err := findFirstTarball(dir)
	if err != nil {
		return err
	}
	if tarball == "" {
		return nil
	}
	fmt.Fprintf(stdout, "Extracting %s...\n", tarball)
	if err := archive.UntarStripOne(tarball, dir); err != nil {
		return fmt.Errorf("extract %s: %w", tarball, err)
	}
	if err := os.Remove(tarball); err != nil {
		return fmt.Errorf("remove tarball: %w", err)
	}
	return nil
}

func findFirstTarball(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".tgz") || strings.HasSuffix(name, ".tar.gz") {
			return filepath.Join(dir, name), nil
		}
	}
	return "", nil
}
