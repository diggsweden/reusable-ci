// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact_test

import (
	"errors"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	domainartifact "github.com/diggsweden/reusable-ci/v3/internal/domain/artifact"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestCollectGlobEntries_RepeatedLiteralKeepsBasename(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "value.txt")
	writeFile(t, file)

	for _, patterns := range [][]string{{file}, {file, file}, {file, root + "/./value.txt"}, {file + "\n" + file}} {
		entries, err := domainartifact.CollectGlobEntries(patterns, false)
		if err != nil || len(entries) != 1 || entries[0].Abs != file || entries[0].RelPath != "value.txt" || entries[0].Size != 1 {
			t.Fatalf("patterns=%v entries=%v err=%v", patterns, entries, err)
		}
	}
}

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

	if !slices.Equal(got, want) {
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
	if want := []string{"a.json", "b.json"}; !slices.Equal(got, want) {
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
	if want := []string{"a/b/bom.json", "a/bom.json"}; !slices.Equal(got, want) {
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
	if want := []string{"app.bin", "meta/info.txt"}; !slices.Equal(got, want) {
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

	if got := relPaths(entries); !slices.Equal(got, []string{"a.json", "b.json"}) {
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

	if got := relPaths(entries); !slices.Equal(got, []string{"one.txt", "two.txt"}) {
		t.Fatalf("multiline collect = %v, want [one.txt two.txt]", got)
	}
}

func TestCollectGlobEntries_MultiplePatternsUseLeastCommonAncestor(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "shared", "reports", "junit.xml"))
	writeFile(t, filepath.Join(dir, "shared", "coverage", "coverage.json"))

	entries, err := domainartifact.CollectGlobEntries([]string{
		filepath.Join(dir, "*", "reports", "*.xml"),
		filepath.Join(dir, "shared", "coverage", "*.json"),
	}, false)
	if err != nil {
		t.Fatalf("CollectGlobEntries: %v", err)
	}

	// Both matches are below shared/, but the first pattern's wildcard-free
	// base is dir. Computing the root from matched files would incorrectly drop
	// the shared/ prefix and make this assertion fail.
	want := []string{"shared/coverage/coverage.json", "shared/reports/junit.xml"}
	if got := relPaths(entries); !slices.Equal(got, want) {
		t.Fatalf("multi-pattern collect = %v, want %v (rooted at pattern-base common ancestor)", got, want)
	}
}

func TestCollectGlobEntries_HiddenFilesRequireIncludeHidden(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "dist", "visible.txt"))
	writeFile(t, filepath.Join(dir, "dist", ".secret"))
	writeFile(t, filepath.Join(dir, "dist", ".metadata", "build.json"))
	pattern := filepath.Join(dir, "dist", "**")

	withoutHidden, err := domainartifact.CollectGlobEntries([]string{pattern}, false)
	if err != nil {
		t.Fatalf("CollectGlobEntries without hidden: %v", err)
	}

	if got := relPaths(withoutHidden); !slices.Equal(got, []string{"visible.txt"}) {
		t.Fatalf("default glob collect = %v, want [visible.txt]", got)
	}

	withHidden, err := domainartifact.CollectGlobEntries([]string{pattern}, true)
	if err != nil {
		t.Fatalf("CollectGlobEntries with hidden: %v", err)
	}

	want := []string{".metadata/build.json", ".secret", "visible.txt"}
	if got := relPaths(withHidden); !slices.Equal(got, want) {
		t.Fatalf("include-hidden glob collect = %v, want %v", got, want)
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

	if got := relPaths(entries); !slices.Equal(got, []string{"lint.json", "test.json"}) {
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

func TestCollectGlobEntries_RejectsMalformedPatternEvenWhenRootIsEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	_, err := domainartifact.CollectGlobEntries([]string{filepath.Join(dir, "[broken")}, false)
	// A mistyped --path is CLI misuse, so it exits 2 rather than the
	// unclassified 70 that reads "file a bug"...
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	// ...and the doublestar cause survives the wrap, so the operator still
	// sees that the pattern itself is what is malformed.
	if !errors.Is(err, path.ErrBadPattern) {
		t.Errorf("err = %v, want it to wrap path.ErrBadPattern", err)
	}

	if !strings.Contains(err.Error(), "[broken") {
		t.Errorf("err = %v, want it to quote the malformed pattern", err)
	}
}
