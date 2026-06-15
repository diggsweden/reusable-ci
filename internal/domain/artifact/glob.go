// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// CollectUpload resolves an upload set, dispatching on which selector the
// caller supplied: glob patterns (paths) preserve directory structure, while
// dir/files use the literal walk/flatten collection. Paths take precedence.
func CollectUpload(dir string, files, paths []string, includeHidden bool) ([]UploadEntry, error) {
	if len(paths) > 0 {
		return CollectGlobEntries(paths, includeHidden)
	}

	return CollectUploadEntries(dir, files, includeHidden)
}

// CollectGlobEntries resolves an upload set from glob patterns, reproducing
// actions/upload-artifact's path semantics without a third-party glob library:
//
//   - each pattern may use *, ?, [set] (within one segment) and ** (across any
//     number of segments, including zero);
//   - a line beginning with ! is an exclude pattern, removed from the result;
//   - the artifact-relative path of every matched file is taken relative to the
//     least-common-ancestor directory of all matches, so directory structure is
//     preserved (a single file collapses to its basename);
//   - when includeHidden is false, dotfiles (and dot-directory descendants)
//     under that root are dropped.
//
// Patterns are slash- or OS-separated; multi-line values (a YAML `path: |`
// block passed as one argument) are split on newlines.
func CollectGlobEntries(patterns []string, includeHidden bool) ([]UploadEntry, error) {
	includes, excludes := splitPatterns(patterns)

	matched := map[string]struct{}{}

	for _, pat := range includes {
		files, err := globExpand(pat)
		if err != nil {
			return nil, err
		}

		for _, file := range files {
			matched[file] = struct{}{}
		}
	}

	for _, pat := range excludes {
		files, err := globExpand(pat)
		if err != nil {
			return nil, err
		}

		for _, file := range files {
			delete(matched, file)
		}
	}

	root, err := globRoot(includes, matched)
	if err != nil {
		return nil, err
	}

	return entriesFromMatches(matched, root, includeHidden)
}

// globRoot reproduces actions/upload-artifact's root selection: the artifact
// tree is rooted at the least-common-ancestor of the patterns' non-wildcard
// bases — except a single literal-file pattern, which roots at the file's
// parent directory so it stores as a bare basename.
func globRoot(includes []string, matched map[string]struct{}) (string, error) {
	bases := make([]string, 0, len(includes))

	for _, pat := range includes {
		base, _ := splitGlobBase(filepath.ToSlash(pat))
		if base == "" {
			base = "."
		}

		abs, err := filepath.Abs(filepath.FromSlash(base))
		if err != nil {
			return "", fmt.Errorf("absolute base of %q: %w", pat, err)
		}

		bases = append(bases, abs)
	}

	if len(includes) == 1 {
		if _, rest := splitGlobBase(filepath.ToSlash(includes[0])); rest == "" {
			if _, ok := matched[bases[0]]; ok && len(matched) == 1 {
				return filepath.Dir(bases[0]), nil
			}
		}

		return bases[0], nil
	}

	return commonPrefixDir(bases), nil
}

// entriesFromMatches turns the matched absolute file set into UploadEntries,
// rooting relative paths at root.
func entriesFromMatches(matched map[string]struct{}, root string, includeHidden bool) ([]UploadEntry, error) {
	files := make([]string, 0, len(matched))
	for file := range matched {
		files = append(files, file)
	}

	sort.Strings(files)

	out := make([]UploadEntry, 0, len(files))

	for _, file := range files {
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return nil, fmt.Errorf("relativise %q under %q: %w", file, root, err)
		}

		if !includeHidden && hasHiddenSegment(rel) {
			continue
		}

		info, err := os.Lstat(file)
		if err != nil {
			return nil, fmt.Errorf("stat %q: %w", file, err)
		}

		out = append(out, UploadEntry{Abs: file, RelPath: filepath.ToSlash(rel), Size: info.Size()})
	}

	return out, nil
}

// splitPatterns flattens multi-line entries, trims blanks, and partitions
// "!"-prefixed lines into the exclude set.
func splitPatterns(patterns []string) ([]string, []string) {
	var includes, excludes []string

	for _, block := range patterns {
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			if strings.HasPrefix(line, "!") {
				excludes = append(excludes, strings.TrimSpace(line[1:]))

				continue
			}

			includes = append(includes, line)
		}
	}

	return includes, excludes
}

// globExpand returns the absolute paths of the regular files matching one
// pattern. The literal (non-wildcard) leading segments fix the directory to
// walk; the remainder is matched with matchPath. A pattern with no wildcards
// is a literal file reference.
func globExpand(pattern string) ([]string, error) {
	pattern = filepath.ToSlash(pattern)
	base, rest := splitGlobBase(pattern)

	if rest == "" {
		return literalMatch(base)
	}

	walkRoot := base
	if walkRoot == "" {
		walkRoot = "."
	}

	var out []string

	err := filepath.WalkDir(walkRoot, func(entryPath string, entry fs.DirEntry, walkErr error) error {
		abs, matched, mErr := matchWalkEntry(walkRoot, rest, entryPath, entry, walkErr)
		if mErr != nil {
			return mErr
		}

		if matched {
			out = append(out, abs)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("glob %q: %w", pattern, err)
	}

	return out, nil
}

// matchWalkEntry decides whether one walked entry is a glob match, returning
// its absolute path when so. A missing walk root yields no match (an absent
// optional path is not an error).
func matchWalkEntry(walkRoot, rest, entryPath string, entry fs.DirEntry, walkErr error) (string, bool, error) {
	if walkErr != nil {
		if os.IsNotExist(walkErr) {
			return "", false, nil
		}

		return "", false, walkErr
	}

	if !entry.Type().IsRegular() { // never follow symlinks, skip dirs
		return "", false, nil
	}

	rel, err := filepath.Rel(walkRoot, entryPath)
	if err != nil {
		return "", false, err
	}

	ok, err := matchPath(rest, filepath.ToSlash(rel))
	if err != nil || !ok {
		return "", false, err
	}

	abs, err := filepath.Abs(entryPath)
	if err != nil {
		return "", false, err
	}

	return abs, true, nil
}

// literalMatch resolves a wildcard-free pattern. A regular file resolves to
// itself; a directory expands to all regular files beneath it (the way
// actions/upload-artifact globs a bare directory path); anything absent or
// non-regular yields nothing.
func literalMatch(literal string) ([]string, error) {
	info, err := os.Lstat(literal)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("stat %q: %w", literal, err)
	}

	if info.IsDir() {
		return walkRegularFiles(literal)
	}

	if !info.Mode().IsRegular() {
		return nil, nil
	}

	abs, err := filepath.Abs(literal)
	if err != nil {
		return nil, fmt.Errorf("absolute %q: %w", literal, err)
	}

	return []string{abs}, nil
}

// walkRegularFiles returns the absolute paths of every regular file beneath
// dir (symlinks are skipped, never followed).
func walkRegularFiles(dir string) ([]string, error) {
	var out []string

	err := filepath.WalkDir(dir, func(entryPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if !entry.Type().IsRegular() {
			return nil
		}

		abs, absErr := filepath.Abs(entryPath)
		if absErr != nil {
			return absErr
		}

		out = append(out, abs)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %q: %w", dir, err)
	}

	return out, nil
}

// splitGlobBase splits a slash pattern into the longest wildcard-free leading
// directory and the wildcard remainder. A leading "/" is preserved on base.
func splitGlobBase(pattern string) (string, string) {
	segs := strings.Split(pattern, "/")

	idx := 0
	for idx < len(segs) && !hasGlobMeta(segs[idx]) {
		idx++
	}

	return strings.Join(segs[:idx], "/"), strings.Join(segs[idx:], "/")
}

func hasGlobMeta(seg string) bool {
	return strings.ContainsAny(seg, "*?[")
}

// matchPath reports whether the slash-separated name matches pattern, where
// "**" spans any number of segments (including zero) and *, ?, [set] match
// within one segment via path.Match.
func matchPath(pattern, name string) (bool, error) {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegments(pat, name []string) (bool, error) {
	for len(pat) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			if len(rest) == 0 {
				return true, nil // trailing ** matches the remainder
			}

			for idx := 0; idx <= len(name); idx++ {
				ok, err := matchSegments(rest, name[idx:])
				if err != nil || ok {
					return ok, err
				}
			}

			return false, nil
		}

		if len(name) == 0 {
			return false, nil
		}

		ok, err := path.Match(pat[0], name[0])
		if err != nil || !ok {
			return false, err
		}

		pat = pat[1:]
		name = name[1:]
	}

	return len(name) == 0, nil
}

// commonPrefixDir returns the deepest directory that is a common ancestor of
// every base path (segment-wise common prefix).
func commonPrefixDir(paths []string) string {
	if len(paths) == 0 {
		return ""
	}

	common := strings.Split(filepath.ToSlash(paths[0]), "/")

	for _, candidate := range paths[1:] {
		segs := strings.Split(filepath.ToSlash(candidate), "/")

		end := 0
		for end < len(common) && end < len(segs) && common[end] == segs[end] {
			end++
		}

		common = common[:end]
	}

	root := strings.Join(common, "/")
	if root == "" {
		return "/"
	}

	return root
}
