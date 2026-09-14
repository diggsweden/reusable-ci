// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package artifact_test

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	domainartifact "github.com/diggsweden/reusable-ci/v3/internal/domain/artifact"
)

// relPaths returns the sorted artifact-relative paths of the collected set.
func relPaths(entries []domainartifact.UploadEntry) []string {
	out := make([]string, 0, len(entries))

	for _, entry := range entries {
		out = append(out, entry.RelPath)
	}

	slices.Sort(out)

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
	hiddenNested := filepath.Join(dir, ".private", "nested.txt")

	writeFile(t, visible)
	writeFile(t, hidden)
	writeFile(t, hiddenNested)

	entries, err := domainartifact.CollectUploadEntries("", []string{visible, hidden, hiddenNested}, false)
	if err != nil {
		t.Fatalf("CollectUploadEntries: %v", err)
	}

	got := relPaths(entries)
	if want := []string{"keep.txt"}; !slices.Equal(got, want) {
		t.Fatalf("files-mode collect = %v, want %v", got, want)
	}
}

func TestCollectUploadEntries_FilesModePreservesCommonRootRelativePaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first := filepath.Join(dir, "nested", "a", "first.txt")
	second := filepath.Join(dir, "nested", "b", "second.txt")

	writeFile(t, first)
	writeFile(t, second)

	entries, err := domainartifact.CollectUploadEntries("", []string{second, first, first}, false)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := relPaths(entries), []string{"a/first.txt", "b/second.txt"}; !slices.Equal(got, want) {
		t.Fatalf("files-mode collect = %v, want stable common-root paths %v", got, want)
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

func TestCollectUploadEntries_HiddenAncestorsVersusMembers(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), ".workspace", "out")

	files := []string{filepath.Join(root, "visible.txt"), filepath.Join(root, ".hidden"), filepath.Join(root, ".private", "value")}
	for _, file := range files {
		writeFile(t, file)
	}

	for _, includeHidden := range []bool{false, true} {
		exact, err := domainartifact.CollectUploadEntries("", files, includeHidden)
		if err != nil {
			t.Fatal(err)
		}

		dir, err := domainartifact.CollectUploadEntries(root, nil, includeHidden)
		if err != nil {
			t.Fatal(err)
		}

		want := []string{"visible.txt"}
		if includeHidden {
			want = []string{".hidden", ".private/value", "visible.txt"}
		}

		if !slices.Equal(relPaths(exact), want) || !slices.Equal(relPaths(dir), want) {
			t.Fatalf("includeHidden=%v exact=%v dir=%v want=%v", includeHidden, relPaths(exact), relPaths(dir), want)
		}
	}

	exact, err := domainartifact.CollectUploadEntries("", files[:1], false)
	if err != nil || len(exact) != 1 || exact[0].Abs != files[0] || exact[0].RelPath != "visible.txt" || exact[0].Size != 1 {
		t.Fatalf("single-file metadata=%v err=%v", exact, err)
	}
}

func TestCollectUpload_DirectoryIdentitySurvivesCWDChange(t *testing.T) { //nolint:paralleltest // t.Chdir restores process-wide cwd; this case must stay serial.
	root := t.TempDir()
	file := filepath.Join(root, "out", "value.txt")
	writeFile(t, file)
	t.Chdir(root)

	entries, err := domainartifact.CollectUpload("out", nil, nil, false)
	if err != nil || len(entries) != 1 || entries[0].Abs != file || entries[0].RelPath != "value.txt" {
		t.Fatalf("entries=%v err=%v", entries, err)
	}

	decoy := t.TempDir()
	writeFile(t, filepath.Join(decoy, "out", "value.txt"))

	if writeErr := os.WriteFile(filepath.Join(decoy, "out", "value.txt"), []byte("decoy"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	t.Chdir(decoy)

	opened, err := domainartifact.OpenUploadEntry(entries[0])
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = opened.Close() }()

	body, err := io.ReadAll(opened)
	if err != nil || string(body) != "x" {
		t.Fatalf("redirected read=%q err=%v", body, err)
	}
}

func TestCollectUpload_AllModesPreserveCompleteEntries(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), ".workspace", "out")
	want := []struct{ rel, body string }{
		{".hidden.txt", "abc"}, {".private/value.txt", "hidden"}, {"a.txt", "a"}, {"sub/b.txt", "bbbb"},
	}

	files := make([]string, 0, len(want)+1)
	for _, entry := range want {
		file := filepath.Join(root, entry.rel)
		writeFile(t, file)

		if err := os.WriteFile(file, []byte(entry.body), 0o600); err != nil {
			t.Fatal(err)
		}

		files = append(files, file)
	}

	slices.Reverse(files)
	files = append(files, files[0])

	for _, hidden := range []bool{false, true} {
		for _, mode := range []string{"directory", "exact", "glob"} {
			var (
				entries []domainartifact.UploadEntry
				err     error
			)

			switch mode {
			case "directory":
				entries, err = domainartifact.CollectUpload(root, nil, nil, hidden)
			case "exact":
				entries, err = domainartifact.CollectUpload("", files, nil, hidden)
			case "glob":
				entries, err = domainartifact.CollectUpload("", nil, []string{root + "/**/*.txt"}, hidden)
			}

			if err != nil {
				t.Fatal(err)
			}

			expected := want[2:]
			if hidden {
				expected = want
			}

			if len(entries) != len(expected) {
				t.Fatalf("mode=%s hidden=%v entries=%v", mode, hidden, entries)
			}

			for index, entry := range entries {
				if entry.Abs != filepath.Join(root, expected[index].rel) || entry.RelPath != expected[index].rel || entry.Size != int64(len(expected[index].body)) {
					t.Errorf("mode=%s hidden=%v entry=%+v expected=%+v", mode, hidden, entry, expected[index])
				}
			}
		}
	}
}
