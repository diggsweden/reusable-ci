// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

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

	for _, want := range []string{"hsm-worker-linux-amd64", "digg-hsm-keytool-linux-amd64"} {
		if _, err := os.Stat(fsys.Path(want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
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
	// hsm-worker renamed.
	if _, err := os.Stat(fsys.Path("hsm-worker-linux-arm64")); err != nil {
		t.Errorf("missing rename: %v", err)
	}
	// extra-tool untouched.
	if _, err := os.Stat(fsys.Path("extra-tool")); err != nil {
		t.Errorf("extra-tool should still exist: %v", err)
	}

	if _, err := os.Stat(fsys.Path("extra-tool-linux-arm64")); !os.IsNotExist(err) {
		t.Errorf("extra-tool should NOT be renamed (not in ExpectedNames): %v", err)
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
