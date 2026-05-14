// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package publish

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/app/archive"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
)

// NPMValidateTarballInput drives NPMValidateTarball.
type NPMValidateTarballInput struct {
	// Dir is the directory containing the tarball. Empty → cwd.
	Dir string
}

// NPMValidateTarball finds a single *.tgz / *.tar.gz at the top of Dir,
// extracts it with `tar --strip-components=1` semantics, removes the
// tarball, lists the extracted contents from dist/ or build/, and
// checks that dist/cli.js exists. Mirrors
// scripts/publish/npm-validate-tarball.sh.
func NPMValidateTarball(_ context.Context, stdout, stderr io.Writer, annot output.Annotator, in NPMValidateTarballInput) error {
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
		annot.Errorf("No tarball found in artifacts")
		return fmt.Errorf("no tarball found in %s: %w", dir, errs.ErrMissingInput)
	}

	fmt.Fprintf(stdout, "Extracting %s...\n", tarball)
	if err := archive.UntarStripOne(tarball, dir); err != nil {
		return fmt.Errorf("extract %s: %w", tarball, err)
	}
	if err := os.Remove(tarball); err != nil {
		return fmt.Errorf("remove tarball: %w", err)
	}

	fmt.Fprintln(stdout, "Extracted contents:")
	listed := listFiles(stdout, filepath.Join(dir, "dist"))
	if !listed {
		listFiles(stdout, filepath.Join(dir, "build"))
	}

	cliJS := filepath.Join(dir, "dist", "cli.js")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Verifying dist/cli.js exists:")
	if _, err := os.Stat(cliJS); err == nil {
		fmt.Fprintln(stdout, "✓ dist/cli.js found")
	} else {
		fmt.Fprintln(stdout, "✗ dist/cli.js NOT found")
	}
	return nil
}

// findFirstTarball returns the path to the first *.tgz / *.tar.gz file
// found in dir (non-recursive). Matches the bash `find . -maxdepth 1`.
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

// listFiles prints "<path>" for every regular file under dir. Returns
// true iff dir existed and was walked.
func listFiles(out io.Writer, dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("listFiles: skipping unreadable entry", "path", path, "err", err)
			return nil
		}
		if !d.IsDir() {
			fmt.Fprintln(out, path)
		}
		return nil
	})
	return true
}
