// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact_test

import (
	"path/filepath"
	"testing"

	domainartifact "github.com/diggsweden/reusable-ci/v3/internal/domain/artifact"
)

func TestCollectGlobEntries_StarPreservesStructure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "dist", "index.js"))
	writeFile(t, filepath.Join(dir, "dist", "assets", "styles.css"))
	writeFile(t, filepath.Join(dir, "dist", "skip.txt"))

	entries, err := domainartifact.CollectGlobEntries([]string{filepath.Join(dir, "dist", "**")}, false)
	if err != nil {
		t.Fatalf("CollectGlobEntries: %v", err)
	}

	got := relPaths(entries)
	want := []string{"assets/styles.css", "index.js", "skip.txt"}

	if !equalRel(got, want) {
		t.Fatalf("** collect = %v, want %v (rooted at dist)", got, want)
	}
}

func TestCollectGlobEntries_SingleLevelStar(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.json"))
	writeFile(t, filepath.Join(dir, "b.json"))
	writeFile(t, filepath.Join(dir, "c.txt"))
	writeFile(t, filepath.Join(dir, "nested", "d.json"))

	entries, err := domainartifact.CollectGlobEntries([]string{filepath.Join(dir, "*.json")}, false)
	if err != nil {
		t.Fatalf("CollectGlobEntries: %v", err)
	}

	got := relPaths(entries)
	if want := []string{"a.json", "b.json"}; !equalRel(got, want) {
		t.Fatalf("*.json collect = %v, want %v (single level, no nested/d.json)", got, want)
	}
}

func TestCollectGlobEntries_DoubleStarLeading(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a", "bom.json"))
	writeFile(t, filepath.Join(dir, "a", "b", "bom.json"))
	writeFile(t, filepath.Join(dir, "a", "other.json"))

	entries, err := domainartifact.CollectGlobEntries([]string{filepath.Join(dir, "**", "bom.json")}, false)
	if err != nil {
		t.Fatalf("CollectGlobEntries: %v", err)
	}

	got := relPaths(entries)
	if want := []string{"a/b/bom.json", "a/bom.json"}; !equalRel(got, want) {
		t.Fatalf("**/bom.json collect = %v, want %v", got, want)
	}
}

func TestCollectGlobEntries_LiteralFileCollapsesToBasename(t *testing.T) {
	t.Parallel()

	// A literal (wildcard-free) single-file pattern roots at the file's parent
	// directory, so it stores as a bare basename — `path: results.sarif`.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "sub", "results.sarif"))

	entries, err := domainartifact.CollectGlobEntries([]string{filepath.Join(dir, "sub", "results.sarif")}, false)
	if err != nil {
		t.Fatalf("CollectGlobEntries: %v", err)
	}

	if got := relPaths(entries); len(got) != 1 || got[0] != "results.sarif" {
		t.Fatalf("literal-file collect = %v, want [results.sarif]", got)
	}
}

func TestCollectGlobEntries_GlobKeepsStructureUnderBase(t *testing.T) {
	t.Parallel()

	// A glob roots at its non-wildcard base, preserving structure beneath it
	// even for a single match — `path: <dir>/**/*.sarif`.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "sub", "results.sarif"))

	entries, err := domainartifact.CollectGlobEntries([]string{filepath.Join(dir, "**", "*.sarif")}, false)
	if err != nil {
		t.Fatalf("CollectGlobEntries: %v", err)
	}

	if got := relPaths(entries); len(got) != 1 || got[0] != "sub/results.sarif" {
		t.Fatalf("glob collect = %v, want [sub/results.sarif]", got)
	}
}

func TestCollectGlobEntries_LiteralDirectoryExpandsContents(t *testing.T) {
	t.Parallel()

	// `path: build/out` (a bare directory) uploads its contents rooted at the
	// directory — the actions/upload-artifact directory-path behaviour.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "out", "app.bin"))
	writeFile(t, filepath.Join(dir, "out", "meta", "info.txt"))

	entries, err := domainartifact.CollectGlobEntries([]string{filepath.Join(dir, "out")}, false)
	if err != nil {
		t.Fatalf("CollectGlobEntries: %v", err)
	}

	got := relPaths(entries)
	if want := []string{"app.bin", "meta/info.txt"}; !equalRel(got, want) {
		t.Fatalf("literal-dir collect = %v, want %v", got, want)
	}
}

func TestCollectGlobEntries_ExcludePattern(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.json"))
	writeFile(t, filepath.Join(dir, "b.json"))
	writeFile(t, filepath.Join(dir, "ignore.json"))

	patterns := []string{
		filepath.Join(dir, "*.json"),
		"!" + filepath.Join(dir, "ignore.json"),
	}

	entries, err := domainartifact.CollectGlobEntries(patterns, false)
	if err != nil {
		t.Fatalf("CollectGlobEntries: %v", err)
	}

	if got := relPaths(entries); !equalRel(got, []string{"a.json", "b.json"}) {
		t.Fatalf("exclude collect = %v, want [a.json b.json]", got)
	}
}

func TestCollectGlobEntries_MultilineBlock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "one.txt"))
	writeFile(t, filepath.Join(dir, "two.txt"))

	// A YAML `path: |` block arrives as one newline-joined argument.
	block := filepath.Join(dir, "one.txt") + "\n" + filepath.Join(dir, "two.txt") + "\n"

	entries, err := domainartifact.CollectGlobEntries([]string{block}, false)
	if err != nil {
		t.Fatalf("CollectGlobEntries: %v", err)
	}

	if got := relPaths(entries); !equalRel(got, []string{"one.txt", "two.txt"}) {
		t.Fatalf("multiline collect = %v, want [one.txt two.txt]", got)
	}
}

func TestCollectGlobEntries_HiddenRootNotTreatedAsHidden(t *testing.T) {
	t.Parallel()

	// .ci-results/*.json — the dot-directory is the root, so the matched
	// files are not hidden relative to it and survive the default filter.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".ci-results", "lint.json"))
	writeFile(t, filepath.Join(dir, ".ci-results", "test.json"))

	entries, err := domainartifact.CollectGlobEntries([]string{filepath.Join(dir, ".ci-results", "*.json")}, false)
	if err != nil {
		t.Fatalf("CollectGlobEntries: %v", err)
	}

	if got := relPaths(entries); !equalRel(got, []string{"lint.json", "test.json"}) {
		t.Fatalf("hidden-root collect = %v, want [lint.json test.json]", got)
	}
}

func TestCollectGlobEntries_MissingPatternIsEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	entries, err := domainartifact.CollectGlobEntries([]string{filepath.Join(dir, "nope", "*.json")}, false)
	if err != nil {
		t.Fatalf("CollectGlobEntries: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("missing-pattern collect = %v, want empty", entries)
	}
}

// equalRel compares two already-sorted-by-collection slices irrespective of
// order (relPaths sorts, but tests build wants in display order).
func equalRel(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	seen := map[string]int{}
	for _, value := range got {
		seen[value]++
	}

	for _, value := range want {
		if seen[value] == 0 {
			return false
		}

		seen[value]--
	}

	return true
}
