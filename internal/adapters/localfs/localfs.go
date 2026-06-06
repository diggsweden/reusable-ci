// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package localfs is a tiny filesystem adapter that backs the
// app/release CreateRelease asset-collection paths. Production reads
// the real filesystem; tests pass an in-memory fakefs implementation
// instead.
package localfs

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/diggsweden/reusable-ci/internal/domain/release"
)

// FS satisfies app/release.fsOps via the real OS filesystem.
type FS struct{}

// New returns the default OS-backed FS.
func New() *FS { return &FS{} }

// FileExists reports whether path is a regular file.
func (f *FS) FileExists(path string) bool {
	if path == "" {
		return false
	}

	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return info.Mode().IsRegular()
}

// FileNonEmpty reports whether path exists and has size > 0.
func (f *FS) FileNonEmpty(path string) bool {
	if path == "" {
		return false
	}

	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return info.Mode().IsRegular() && info.Size() > 0
}

// FindReleaseArtifacts walks dir and returns every file with a
// recognised release extension (excluding original-*.jar). Returns nil
// when dir doesn't exist.
func (f *FS) FindReleaseArtifacts(dir string) []string {
	if dir == "" {
		dir = release.DefaultReleaseArtifactsDir
	}

	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		slog.Warn("FindReleaseArtifacts: stat failed", "dir", dir, "err", err)

		return nil
	}

	var out []string

	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if err != nil {
			slog.Debug("FindReleaseArtifacts: skipping unreadable entry", "path", p, "err", err)

			return nil
		}

		if d.IsDir() {
			return nil
		}

		if release.IsReleaseArtifact(p) {
			out = append(out, p)

			return nil
		}

		return nil
	})

	sort.Strings(out)

	return out
}

// Glob expands a shell-style pattern.
func (f *FS) Glob(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}

	return matches
}

// ListSignatureSidecars returns every signature sidecar in the cwd
// across all supported signing methods: *.asc (gpg) and *.bundle
// (cosign v3, used by both --method=sigstore and --method=kms).
// Matches are sorted for deterministic test output.
func (f *FS) ListSignatureSidecars() []string {
	var out []string

	for _, pattern := range []string{"*.asc", "*.bundle"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}

		out = append(out, matches...)
	}

	sort.Strings(out)

	return out
}
