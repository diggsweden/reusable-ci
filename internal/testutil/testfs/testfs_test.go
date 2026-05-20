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
