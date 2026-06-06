// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom

import (
	"path/filepath"
	"sort"
	"strings"
)

// FindBuildBOMInput drives FindBuildBOM. Patterns are glob-style
// matched against the cleaned slash-form path of every file in roots.
// Excludes drop matching paths from the candidate set after the
// include filter is applied.
type FindBuildBOMInput struct {
	Files    []string // every file under the search root, slash-form path
	Includes []string // glob patterns; at least one must match
	Excludes []string // glob patterns; any match drops the candidate
}

// FindBuildBOM returns the path with the smallest number of "/"
// separators that matches at least one include and no excludes. Ties
// break lexicographically. Returns "" when no candidate matches.
//
// Mirrors the depth-sort logic in `_find_build_bom` — the bash
// `find -printf '%d\t%p\n' | sort -k1,1n -k2,2 | head -1` shape.
//nolint:cyclop // build-BOM discovery: per project type + per format + per layer.
func FindBuildBOM(in FindBuildBOMInput) string {
	type cand struct {
		depth int
		path  string
	}

	var cands []cand

	for _, p := range in.Files {
		clean := filepath.ToSlash(filepath.Clean(p))
		// Mirror the bash `find .` shape: it walks from "." and produces
		// "./<path>" entries, so leading `*/…` patterns can match
		// top-level files. Prepend "./" when the caller passes the bare
		// relative path.
		matchPath := clean
		if !strings.HasPrefix(matchPath, "./") && !strings.HasPrefix(matchPath, "/") {
			matchPath = "./" + matchPath
		}
		// Exclude filter first.
		excluded := false

		for _, ex := range in.Excludes {
			if ok, _ := pathMatch(ex, matchPath); ok {
				excluded = true

				break
			}
		}

		if excluded {
			continue
		}
		// Include filter.
		matched := false

		for _, inc := range in.Includes {
			if ok, _ := pathMatch(inc, matchPath); ok {
				matched = true

				break
			}
		}

		if !matched {
			continue
		}

		cands = append(cands, cand{depth: strings.Count(clean, "/"), path: clean})
	}

	if len(cands) == 0 {
		return ""
	}

	sort.Slice(cands, func(i, j int) bool {
		if cands[i].depth != cands[j].depth {
			return cands[i].depth < cands[j].depth
		}

		return cands[i].path < cands[j].path
	})

	return cands[0].path
}

// pathMatch matches a find-style glob (*, **, ?) against a clean path.
// We implement a custom matcher because filepath.Match doesn't span
// directory separators — we want `*/target/bom.json` to match
// `release-artifacts/foo/target/bom.json`.
func pathMatch(pattern, path string) (bool, error) {
	// Convert find-glob to regexp.
	var sb strings.Builder
	sb.WriteByte('^')

	i := 0 //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	for i < len(pattern) {
		c := pattern[i] //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		switch c {
		case '*':
			// `*` matches any run of non-`/` chars in glob, but the bash
			// patterns are find -path globs which match `/` too. We use the
			// permissive form: `.*`.
			sb.WriteString(".*")
		case '?':
			sb.WriteString(".")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		default:
			sb.WriteByte(c)
		}

		i++
	}

	sb.WriteByte('$')

	rx, err := regexpCompile(sb.String())
	if err != nil {
		return false, err
	}

	return rx.MatchString(path), nil
}
