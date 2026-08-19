// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// dirEntryNames lists a directory's entries, sorted.
func dirEntryNames(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	sort.Strings(names)

	return names
}

func TestSuffixExtractedBinaries_AllFilesWhenNoExpected(t *testing.T) {
	fsys := testfs.NewReal(t)

	dir := fsys.Root
	for _, name := range []string{"hsm-worker", "digg-hsm-keytool"} {
		fsys.WriteFile(name, []byte("ELF"))
	}

	var out bytes.Buffer

	err := appcontainer.SuffixExtractedBinaries(&out, appcontainer.SuffixExtractedBinariesInput{
		Dir:  dir,
		Arch: "amd64", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		t.Fatalf("SuffixExtractedBinaries: %v", err)
	}

	// The whole directory, not just the names expected to appear: this is a
	// rename, so the originals must be gone. Copying instead would leave
	// hsm-worker beside hsm-worker-linux-amd64 and both would ship.
	want := []string{"digg-hsm-keytool-linux-amd64", "hsm-worker-linux-amd64"}
	if got := dirEntryNames(t, dir); !reflect.DeepEqual(got, want) {
		t.Errorf("directory = %v, want %v", got, want)
	}

	if !strings.Contains(out.String(), "renamed hsm-worker -> hsm-worker-linux-amd64") {
		t.Errorf("missing log line:\n%s", out.String())
	}
}

func TestSuffixExtractedBinaries_ExpectedNamesOnly(t *testing.T) {
	fsys := testfs.NewReal(t)

	dir := fsys.Root
	for _, name := range []string{"hsm-worker", "extra-tool"} {
		fsys.WriteFile(name, []byte("ELF"))
	}

	err := appcontainer.SuffixExtractedBinaries(io.Discard, appcontainer.SuffixExtractedBinariesInput{
		Dir: dir, Arch: "arm64", ExpectedNames: " hsm-worker ", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		t.Fatal(err)
	}
	// hsm-worker renamed and gone; extra-tool untouched and not renamed.
	// Stating the directory covers all three, including the one the separate
	// checks missed: that the original no longer exists.
	want := []string{"extra-tool", "hsm-worker-linux-arm64"}
	if got := dirEntryNames(t, dir); !reflect.DeepEqual(got, want) {
		t.Errorf("directory = %v, want %v", got, want)
	}
}

func TestSuffixExtractedBinaries_MissingExpectedNameErrors(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("hsm-worker", []byte("ELF"))

	err := appcontainer.SuffixExtractedBinaries(io.Discard, appcontainer.SuffixExtractedBinariesInput{
		Dir: fsys.Root, Arch: "arm64", ExpectedNames: " hsm-worker , digg-missing ",
	})
	if err == nil || !strings.Contains(err.Error(), "digg-missing") {
		t.Fatalf("expected missing binary error, got %v", err)
	}
}

func TestSuffixExtractedBinaries_MissingDirErrors(t *testing.T) {
	if err := appcontainer.SuffixExtractedBinaries(io.Discard, appcontainer.SuffixExtractedBinariesInput{
		Dir: "/nonexistent", Arch: "amd64",
	}); err == nil {
		t.Fatal("expected error")
	}
}

func TestSuffixExtractedBinaries_RequiresArch(t *testing.T) {
	fsys := testfs.NewReal(t)
	if err := appcontainer.SuffixExtractedBinaries(io.Discard, appcontainer.SuffixExtractedBinariesInput{
		Dir: fsys.Root,
	}); err == nil {
		t.Fatal("expected error")
	}
}

func TestSuffixExtractedBinaries_NestedFilesUntouched(t *testing.T) {
	fsys := testfs.NewReal(t)
	dir := fsys.Root
	fsys.MkdirAll("nested")
	fsys.WriteFile("nested/keepme", []byte("deep"))
	fsys.WriteFile("hsm-worker", []byte("ELF"))

	err := appcontainer.SuffixExtractedBinaries(io.Discard, appcontainer.SuffixExtractedBinariesInput{
		Dir:  dir,
		Arch: "amd64",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(fsys.Path("hsm-worker-linux-amd64")); err != nil {
		t.Errorf("missing renamed top-level binary: %v", err)
	}

	if _, err := os.Stat(fsys.Path("nested", "keepme")); err != nil {
		t.Errorf("nested file should remain: %v", err)
	}

	if _, err := os.Stat(fsys.Path("nested", "keepme-linux-amd64")); !os.IsNotExist(err) {
		t.Errorf("nested file should not be renamed: %v", err)
	}
}

func TestSuffixExtractedBinaries_EmptyDirNoOp(t *testing.T) {
	fsys := testfs.NewReal(t)
	if err := appcontainer.SuffixExtractedBinaries(io.Discard, appcontainer.SuffixExtractedBinariesInput{
		Dir:  fsys.Root,
		Arch: "amd64",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
