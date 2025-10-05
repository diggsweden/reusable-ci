// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package testfs_test

import (
	"io/fs"
	"os"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestReal_WriteAndRead(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("nested/file.txt", []byte("hello"))

	if got := string(fsys.ReadFile("nested/file.txt")); got != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestReal_MkdirAll(t *testing.T) {
	fsys := testfs.NewReal(t)
	dir := fsys.MkdirAll("a", "b")

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}

	if !info.IsDir() {
		t.Fatalf("%q is not a directory", dir)
	}
}

func TestReal_Chdir(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	if wd != fsys.Root {
		t.Errorf("wd = %q, want %q", wd, fsys.Root)
	}
}

func TestMemory_WriteAndRead(t *testing.T) {
	fsys := testfs.NewMemory(t)
	fsys.WriteFile(".github/workflows/ok.yml", []byte("name: ok\n"))

	body, err := fs.ReadFile(fsys.FS(), ".github/workflows/ok.yml")
	if err != nil {
		t.Fatal(err)
	}

	if got := string(body); got != "name: ok\n" {
		t.Errorf("got %q", got)
	}
}

func TestMemory_MkdirAllAndReadFile(t *testing.T) {
	fsys := testfs.NewMemory(t)
	fsys.MkdirAll("a/b")

	if info, err := fs.Stat(fsys.FS(), "a/b"); err != nil || !info.IsDir() {
		t.Fatalf("stat a/b: info=%v err=%v", info, err)
	}

	fsys.WriteFile("a/b/file.txt", []byte("body"))

	if got := string(fsys.ReadFile("a/b/file.txt")); got != "body" {
		t.Errorf("ReadFile = %q, want body", got)
	}
}

func TestMemory_ListsParentDirectories(t *testing.T) {
	fsys := testfs.NewMemory(t)
	fsys.WriteFile(".github/workflows/ok.yml", []byte("name: ok\n"))

	entries, err := fs.ReadDir(fsys.FS(), ".github/workflows")
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 || entries[0].Name() != "ok.yml" {
		t.Fatalf("entries = %+v", entries)
	}
}

// The tests above prove the helper stores what it was given and hands it back.
// They do not prove it keeps its own copy, and a fixture helper that shares a
// slice with the test is a way for one assertion to change what a later one
// reads — the sort of failure that only shows up when the test order changes.
//
// Nor did anything prove MkdirAll does its job: making it a no-op left every
// test that builds a directory tree with it passing, because most of them go
// on to write files, and writing a file creates its parents anyway. The cases
// where the directory itself is the fixture — an empty directory a validator
// is supposed to reject, a tree walked for its shape — are exactly the ones a
// silent no-op would break.

func TestMemory_WriteFileCopiesTheCallerSlice(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewMemory(t)

	body := []byte("original")
	fsys.WriteFile("a/b.txt", body)

	// A caller that reuses its buffer must not rewrite what was stored.
	copy(body, "MUTATED!")

	if got := string(fsys.ReadFile("a/b.txt")); got != "original" {
		t.Errorf("stored content = %q, want %q: the helper kept the caller's slice", got, "original")
	}
}

func TestMemory_ReadFileReturnsACopy(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewMemory(t)
	fsys.WriteFile("a.txt", []byte("original"))

	got := fsys.ReadFile("a.txt")
	copy(got, "MUTATED!")

	if second := string(fsys.ReadFile("a.txt")); second != "original" {
		t.Errorf("second read = %q, want %q: the helper handed out its own storage", second, "original")
	}
}

func TestMemory_MkdirAllRecordsTheDirectoryAndItsParents(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewMemory(t)
	fsys.MkdirAll("a/b/c")

	for _, dir := range []string{"a", "a/b", "a/b/c"} {
		info, err := fs.Stat(fsys.FS(), dir)
		if err != nil {
			t.Fatalf("stat %q: %v", dir, err)
		}

		if !info.IsDir() {
			t.Errorf("%q is not a directory", dir)
		}
	}
}

func TestReal_MkdirAllCreatesTheWholeTree(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)
	got := fsys.MkdirAll("a", "b", "c")

	if got != fsys.Path("a", "b", "c") {
		t.Errorf("returned %q, want %q", got, fsys.Path("a", "b", "c"))
	}

	for _, dir := range []string{fsys.Path("a"), fsys.Path("a", "b"), got} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %q: %v", dir, err)
		}

		if !info.IsDir() {
			t.Errorf("%q is not a directory", dir)
		}
	}
}

// TestReal_PathDoesNotCreateAnything separates joining from creating. Path is
// used to name a file that is supposed to be ABSENT — "refuses a missing
// config" fixtures pass it a path and expect nothing there. If it created
// what it named, those tests would be asserting the opposite of their name.
func TestReal_PathDoesNotCreateAnything(t *testing.T) {
	t.Parallel()

	fsys := testfs.NewReal(t)
	p := fsys.Path("nested", "missing.txt")

	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("Path created %q (stat err = %v)", p, err)
	}

	if _, err := os.Stat(fsys.Path("nested")); !os.IsNotExist(err) {
		t.Errorf("Path created the parent directory")
	}
}
