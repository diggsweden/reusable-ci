// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// UploadEntry is one resolved file to upload: its on-disk path, the
// artifact-relative slash path it is stored under, and its size. The
// transport decides what to do with these (Forgejo PUTs each; GitHub zips
// them) — the collection itself is forge-neutral.
type UploadEntry struct {
	Abs     string
	RelPath string // forward-slash, relative to the artifact root
	Size    int64
	info    fs.FileInfo
}

// CollectUploadEntries resolves an upload set. Dir mode walks the tree; Files
// mode roots exact files at their least common parent. Both preserve stable
// artifact-relative paths. Symlinks and
// other non-regular entries are skipped (never followed), so an artifact
// can never smuggle a link out of the workspace. When includeHidden is false,
// dotfiles and dot-directories below that artifact root are skipped. External
// ancestors of the directory or least common parent do not affect selection.
//
//nolint:cyclop // directory and exact-file collection share normalization checks.
func CollectUploadEntries(dir string, files []string, includeHidden bool) ([]UploadEntry, error) {
	if dir != "" {
		return walkDir(dir, includeHidden)
	}

	type selectedFile struct {
		abs  string
		info fs.FileInfo
	}

	selected := make([]selectedFile, 0, len(files))
	parents := make([]string, 0, len(files))
	seen := make(map[string]struct{}, len(files))

	for _, path := range files {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("stat %q: %w", path, err)
		}

		if !info.Mode().IsRegular() {
			continue
		}

		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("absolute path for %q: %w", path, err)
		}

		if _, ok := seen[abs]; ok {
			continue
		}

		seen[abs] = struct{}{}
		selected = append(selected, selectedFile{abs: abs, info: info})
		parents = append(parents, filepath.Dir(abs))
	}

	if len(selected) == 0 {
		return nil, nil
	}

	root := commonPrefixDir(parents)

	out := make([]UploadEntry, 0, len(selected))
	for _, file := range selected {
		rel, err := filepath.Rel(root, file.abs)
		if err != nil {
			return nil, fmt.Errorf("relativise %q under %q: %w", file.abs, root, err)
		}

		if !includeHidden && hasHiddenSegment(rel) {
			continue
		}

		out = append(out, UploadEntry{Abs: file.abs, RelPath: filepath.ToSlash(rel), Size: file.info.Size(), info: file.info})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })

	return out, nil
}

func walkDir(dir string, includeHidden bool) ([]UploadEntry, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("absolute directory: %w", err)
	}

	var out []UploadEntry

	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
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

		out = append(out, UploadEntry{Abs: path, RelPath: filepath.ToSlash(rel), Size: info.Size(), info: info})

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
