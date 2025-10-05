// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"fmt"
	"io/fs"
	"os"

	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// The digest scheme and the structural rules live in
// internal/domain/release.DistDigest / WalkSafe. What is left here is the
// part that genuinely needs the real filesystem: lstat'ing the root before
// anything opens it, and handing the tree to the domain as an fs.FS.

// DistDigest computes the canonical hand-off digest of a directory tree.
// See domainrelease.DistDigest for the scheme and the compatibility contract.
func DistDigest(dir string) (string, error) {
	return DistDigestWithManifestRoot(dir, "")
}

// DistDigestWithManifestRoot is DistDigest with an explicit path prefix for
// the hashed sha256sum manifest. When manifestRoot is empty, dir is used so
// existing path-sensitive release digests remain unchanged. Set manifestRoot
// to "." to match `cd <dir> && find . -type f ...` for artifact contents that
// may be staged under different directory names on each side of a job
// boundary.
func DistDigestWithManifestRoot(dir, manifestRoot string) (string, error) {
	if manifestRoot == "" {
		manifestRoot = dir
	}

	sum, err := domainrelease.DistDigest(os.DirFS(dir), manifestRoot)
	if err != nil {
		return "", fmt.Errorf("dist-digest %s: %w", dir, err)
	}

	return sum, nil
}

// VerifyDist checks a dist directory is structurally safe and matches the
// expected digest — the cross-job-boundary integrity check that runs before
// signing. Structural rules mirror verify-dist.sh: dist must be a real
// directory (not a symlink), contain no symlinks, no non-regular /
// non-directory entries, and no control characters in any path. A structural
// violation or a digest mismatch is a validation error.
func VerifyDist(dir, expectedDigest string) error {
	return VerifyDistWithManifestRoot(dir, expectedDigest, "")
}

// VerifyDistWithManifestRoot is VerifyDist with an explicit path prefix for
// digest recomputation. See DistDigestWithManifestRoot.
func VerifyDistWithManifestRoot(dir, expectedDigest, manifestRoot string) error {
	if expectedDigest == "" {
		return fmt.Errorf("validate-dist: --expected-digest is required: %w", errs.ErrUsage)
	}

	// Before the tree is opened. os.DirFS resolves the root, so a dist/ that
	// is itself a symlink would be followed silently and the check lost: this
	// is the one part of verify-dist.sh that cannot be expressed over fs.FS.
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("validate-dist: stat %s: %w", dir, err)
	}

	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("validate-dist: %s must be a real directory, not a symlink: %w", dir, errs.ErrValidation)
	}

	if !info.IsDir() {
		return fmt.Errorf("validate-dist: %s is not a directory: %w", dir, errs.ErrValidation)
	}

	if err = domainrelease.WalkSafe(os.DirFS(dir)); err != nil {
		return fmt.Errorf("validate-dist: %s: %w", dir, err)
	}

	actual, err := DistDigestWithManifestRoot(dir, manifestRoot)
	if err != nil {
		return fmt.Errorf("validate-dist: %w", err)
	}

	if actual != expectedDigest {
		return fmt.Errorf("validate-dist: %s digest mismatch across job boundary: got %s, want %s: %w",
			dir, actual, expectedDigest, errs.ErrValidation)
	}

	return nil
}
