// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// DistDigest computes the canonical digest of a directory tree: the
// SHA-256 of the concatenated "<sha256>  <path>\n" lines for every
// regular file, sorted by path. Byte-compatible with forgejo-ci's
// dist-digest.sh:
//
//	find <dir> -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum
//
// (paths are byte-sorted, matching a C/POSIX-locale `sort`).
func DistDigest(dir string) (string, error) {
	files, err := regularFiles(dir)
	if err != nil {
		return "", err
	}

	sort.Strings(files)

	outer := sha256.New()

	for _, file := range files {
		sum, err := fileSHA256(file)
		if err != nil {
			return "", err
		}

		_, _ = fmt.Fprintf(outer, "%s  %s\n", sum, file)
	}

	return hex.EncodeToString(outer.Sum(nil)), nil
}

// VerifyDist checks a dist directory is structurally safe and matches the
// expected digest — the cross-job-boundary integrity check that runs
// before signing. Structural rules mirror verify-dist.sh: dist must be a
// real directory (not a symlink), contain no symlinks, no non-regular /
// non-directory entries, and no control characters in any path. A
// structural violation or a digest mismatch is a validation error.
func VerifyDist(dir, expectedDigest string) error {
	if expectedDigest == "" {
		return fmt.Errorf("verify-dist: --expected-digest is required: %w", errs.ErrUsage)
	}

	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("verify-dist: stat %s: %w", dir, err)
	}

	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("verify-dist: %s must be a real directory, not a symlink: %w", dir, errs.ErrValidation)
	}

	if !info.IsDir() {
		return fmt.Errorf("verify-dist: %s is not a directory: %w", dir, errs.ErrValidation)
	}

	if err = walkSafe(dir); err != nil {
		return err
	}

	actual, err := DistDigest(dir)
	if err != nil {
		return err
	}

	if actual != expectedDigest {
		return fmt.Errorf("verify-dist: %s digest mismatch across job boundary: got %s, want %s: %w",
			dir, actual, expectedDigest, errs.ErrValidation)
	}

	return nil
}

// walkSafe rejects symlinks, non-regular/non-directory entries, and
// control characters anywhere under dir.
func walkSafe(dir string) error {
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if strings.ContainsAny(path, "\n\r") {
			return fmt.Errorf("verify-dist: path contains control characters: %q: %w", path, errs.ErrValidation)
		}

		mode := entry.Type()
		if mode&fs.ModeSymlink != 0 {
			return fmt.Errorf("verify-dist: %s contains a symlink: %s: %w", dir, path, errs.ErrValidation)
		}

		if !entry.IsDir() && !mode.IsRegular() {
			return fmt.Errorf("verify-dist: %s contains a non-regular entry: %s: %w", dir, path, errs.ErrValidation)
		}

		return nil
	})
}

// regularFiles returns every regular file under dir, paths rooted at dir
// (e.g. "dist/app.tar.gz"), matching `find <dir> -type f`.
func regularFiles(dir string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.Type().IsRegular() {
			files = append(files, path)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("verify-dist: walk %s: %w", dir, err)
	}

	return files, nil
}

// fileSHA256 returns the lowercase hex SHA-256 of a file's contents.
func fileSHA256(path string) (string, error) {
	file, err := os.Open(path) //nolint:gosec // G304: dist artifact path from a controlled directory walk, not attacker input.
	if err != nil {
		return "", fmt.Errorf("verify-dist: open %s: %w", path, err)
	}

	defer func() { _ = file.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("verify-dist: hash %s: %w", path, err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
