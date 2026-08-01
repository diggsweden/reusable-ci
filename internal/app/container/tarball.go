// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/archive"
)

// ExtractNPMTarballInput drives ExtractNPMTarball.
type ExtractNPMTarballInput struct {
	// Dir is the directory containing the tarball. Empty → cwd.
	Dir string
}

// ExtractNPMTarball finds a *.tgz / *.tar.gz at the top of Dir,
// extracts it with `tar --strip-components=1` semantics, removes the
// tarball for container build contexts, and behaves as a no-op when no tarball
// is present.
func ExtractNPMTarball(w io.Writer, in ExtractNPMTarballInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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

	_, _ = fmt.Fprintf(w, "Extracting %s...\n", tarball)

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
