// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact_test

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	domainartifact "github.com/diggsweden/reusable-ci/v3/internal/domain/artifact"
)

// relPaths returns the sorted artifact-relative paths of the collected set.
func relPaths(entries []domainartifact.UploadEntry) []string {
	out := make([]string, 0, len(entries))

	for _, entry := range entries {
		out = append(out, entry.RelPath)
	}

	sort.Strings(out)

	return out
}

func TestCollectUploadEntries_HiddenExcludedByDefault(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "keep.txt"))
	writeFile(t, filepath.Join(dir, ".hidden"))
	writeFile(t, filepath.Join(dir, "sub", "nested.txt"))
	writeFile(t, filepath.Join(dir, ".git", "config"))

	entries, err := domainartifact.CollectUploadEntries(dir, nil, false)
	if err != nil {
		t.Fatalf("CollectUploadEntries: %v", err)
	}

	got := relPaths(entries)
	want := []string{"keep.txt", "sub/nested.txt"}

	if !slices.Equal(got, want) {
		t.Fatalf("default collect = %v, want %v (dotfiles and dot-dirs pruned)", got, want)
	}
}

func TestCollectUploadEntries_HiddenIncludedWhenSet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "keep.txt"))
	writeFile(t, filepath.Join(dir, ".hidden"))
	writeFile(t, filepath.Join(dir, ".git", "config"))

	entries, err := domainartifact.CollectUploadEntries(dir, nil, true)
	if err != nil {
		t.Fatalf("CollectUploadEntries: %v", err)
	}

	got := relPaths(entries)
	want := []string{".git/config", ".hidden", "keep.txt"}

	if !slices.Equal(got, want) {
		t.Fatalf("include-hidden collect = %v, want %v", got, want)
	}
}

func TestCollectUploadEntries_FilesModeFiltersHidden(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	visible := filepath.Join(dir, "keep.txt")
	hidden := filepath.Join(dir, ".secret")

	writeFile(t, visible)
	writeFile(t, hidden)

	entries, err := domainartifact.CollectUploadEntries("", []string{visible, hidden}, false)
	if err != nil {
		t.Fatalf("CollectUploadEntries: %v", err)
	}

	got := relPaths(entries)
	if want := []string{"keep.txt"}; !slices.Equal(got, want) {
		t.Fatalf("files-mode collect = %v, want %v", got, want)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
