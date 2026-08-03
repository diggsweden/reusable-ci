// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// This file owns the release hand-off digest: the rule that says what the
// canonical digest of a dist/ tree is, and what shapes of tree are refused.
// It is the definition the build -> sign boundary is checked against, so it
// lives in the domain and is expressed over fs.FS: no runner, no os, nothing
// to stub. internal/app/release supplies the filesystem and the operator
// context (see VerifyDist there).
//
// Not to be confused with internal/domain/artifact.Digest, a different and
// stricter scheme that also commits to each file's execute bit and size.
// This one is deliberately weaker because it is a wire contract: it must stay
// byte-compatible with the shell pipeline it replaced.

// DistDigest computes the canonical digest of the tree in fsys: the SHA-256
// of the concatenated "<sha256>  <path>\n" lines for every regular file,
// sorted by the path written into the manifest. Byte-compatible with
// forgejo-ci's dist-digest.sh:
//
//	find <dir> -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum
//
// (paths are byte-sorted, matching a C/POSIX-locale `sort`).
//
// manifestRoot is the prefix written into the manifest, which the digest
// therefore commits to. Pass "." to match `cd <dir> && find . -type f ...`,
// which is what a producer and a verifier should agree on when the tree may
// be staged under different directory names on each side of a job boundary.
//
// An empty tree is refused rather than digested to the empty-tree hash: a
// hand-off with nothing in it is a bug on the producing side, and hashing it
// to a well-known constant would let that bug verify successfully.
func DistDigest(fsys fs.FS, manifestRoot string) (string, error) {
	files, err := regularFiles(fsys, manifestRoot)
	if err != nil {
		return "", err
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no files under the tree; refusing to digest an empty hand-off: %w", errs.ErrValidation)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].manifestPath < files[j].manifestPath
	})

	outer := sha256.New()

	for _, file := range files {
		sum, err := fileSHA256(fsys, file.fsPath)
		if err != nil {
			return "", err
		}

		_, _ = fmt.Fprintf(outer, "%s  %s\n", sum, file.manifestPath)
	}

	return hex.EncodeToString(outer.Sum(nil)), nil
}

// WalkSafe rejects symlinks, non-regular/non-directory entries, and control
// characters anywhere in the tree. Structural rules mirror verify-dist.sh.
//
// It cannot speak to the root itself: fs.FS has already resolved that, so a
// dist/ that *is* a symlink has to be caught before the tree is opened. That
// check belongs to the caller (app/release.VerifyDist).
func WalkSafe(fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if strings.ContainsAny(path, "\n\r") {
			return fmt.Errorf("path contains control characters: %q: %w", path, errs.ErrValidation)
		}

		mode := entry.Type()
		if mode&fs.ModeSymlink != 0 {
			return fmt.Errorf("contains a symlink: %s: %w", path, errs.ErrValidation)
		}

		if !entry.IsDir() && !mode.IsRegular() {
			return fmt.Errorf("contains a non-regular entry: %s: %w", path, errs.ErrValidation)
		}

		return nil
	})
}

// digestFile pairs a file's path within fsys with the path that must be
// written into the sha256sum manifest.
type digestFile struct {
	fsPath       string
	manifestPath string
}

// regularFiles returns every regular file in fsys, paired with its manifest
// path. Symlinks and other non-regular entries are skipped rather than
// rejected, matching `find -type f`; WalkSafe is what refuses them.
func regularFiles(fsys fs.FS, manifestRoot string) ([]digestFile, error) {
	var files []digestFile

	err := fs.WalkDir(fsys, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.Type().IsRegular() {
			files = append(files, digestFile{fsPath: path, manifestPath: manifestPath(manifestRoot, path)})
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk the tree: %w", err)
	}

	return files, nil
}

// manifestPath joins the manifest prefix to a tree-relative path. fs.FS paths
// are always slash-separated, which is also what sha256sum emits, so the
// separator is "/" rather than the host's.
func manifestPath(root, rel string) string {
	trimmedRoot := strings.TrimRight(root, "/")

	if rel == "." {
		return trimmedRoot
	}

	if trimmedRoot == "" {
		return "/" + rel
	}

	return trimmedRoot + "/" + rel
}

// fileSHA256 returns the lowercase hex SHA-256 of a file's contents.
func fileSHA256(fsys fs.FS, path string) (string, error) {
	file, err := fsys.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}

	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}
