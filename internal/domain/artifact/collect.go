// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// UploadEntry is one resolved file to upload: its on-disk path, the
// artifact-relative slash path it is stored under, and its size. The
// transport decides what to do with these (Forgejo PUTs each; GitHub zips
// them) — the collection itself is forge-neutral.
type UploadEntry struct {
	Abs     string
	RelPath string // forward-slash, relative to the artifact root
	Size    int64
}

// CollectUploadEntries resolves an upload set. Dir mode walks the tree and
// preserves relative paths; Files mode flattens to basenames. Symlinks and
// other non-regular entries are skipped (never followed), so an artifact
// can never smuggle a link out of the workspace. When includeHidden is false,
// dotfiles (and anything under a dot-directory) are skipped — matching
// actions/upload-artifact's include-hidden-files default.
func CollectUploadEntries(dir string, files []string, includeHidden bool) ([]UploadEntry, error) {
	if dir != "" {
		return walkDir(dir, includeHidden)
	}

	out := make([]UploadEntry, 0, len(files))

	for _, path := range files {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("stat %q: %w", path, err)
		}

		if !info.Mode().IsRegular() {
			continue
		}

		if !includeHidden && strings.HasPrefix(filepath.Base(path), ".") {
			continue
		}

		out = append(out, UploadEntry{Abs: path, RelPath: filepath.Base(path), Size: info.Size()})
	}

	return out, nil
}

func walkDir(dir string, includeHidden bool) ([]UploadEntry, error) {
	var out []UploadEntry

	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		if !includeHidden && hasHiddenSegment(rel) {
			if entry.IsDir() {
				return filepath.SkipDir // prune the whole dot-directory subtree
			}

			return nil
		}

		if !entry.Type().IsRegular() { // skips dirs and symlinks (never followed)
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		out = append(out, UploadEntry{Abs: path, RelPath: filepath.ToSlash(rel), Size: info.Size()})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %q: %w", dir, err)
	}

	return out, nil
}

// hasHiddenSegment reports whether any path segment of rel begins with a dot
// (a dotfile, or a file nested under a dot-directory). The walk root itself
// (rel ".") is not hidden — only its dot-prefixed descendants are.
func hasHiddenSegment(rel string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if seg == "." || seg == "" {
			continue
		}

		if strings.HasPrefix(seg, ".") {
			return true
		}
	}

	return false
}

// NoFilesError is the canonical "nothing matched" error so upload paths
// share one sentinel-wrapped message for the IfNoFiles=error policy.
func NoFilesError(name string) error {
	return fmt.Errorf("no files matched for artifact %q: %w", name, errs.ErrValidation)
}
