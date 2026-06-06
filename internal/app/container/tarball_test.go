// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/internal/app/container"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func makeTarball(t *testing.T, path string, files map[string]string) {
	t.Helper()

	out, err := os.Create(path) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}

	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)

	for rel, body := range files {
		full := "package/" + rel
		if err := tw.WriteHeader(&tar.Header{
			Name: full, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}

		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractNPMTarball_HappyPath(t *testing.T) {
	fsys := testfs.NewReal(t)
	makeTarball(t, fsys.Path("demo-1.0.0.tgz"), map[string]string{
		"package.json": `{"name":"demo"}`,
		"dist/cli.js":  "ok",
	})

	if err := appcontainer.ExtractNPMTarball(io.Discard, appcontainer.ExtractNPMTarballInput{Dir: fsys.Root}); err != nil {
		t.Fatalf("ExtractNPMTarball: %v", err)
	}

	body := fsys.ReadFile("package.json")
	if string(body) != `{"name":"demo"}` {
		t.Errorf("package.json = %q", body)
	}

	if _, err := os.Stat(fsys.Path("demo-1.0.0.tgz")); !os.IsNotExist(err) {
		t.Errorf("tarball should be removed: %v", err)
	}
}

func TestExtractNPMTarball_NoTarballIsNoOp(t *testing.T) {
	fsys := testfs.NewReal(t)
	// Bash silently does nothing; the Go port should too.
	if err := appcontainer.ExtractNPMTarball(io.Discard, appcontainer.ExtractNPMTarballInput{Dir: fsys.Root}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractNPMTarball_StripsPackagePrefixIntoWorkingDir(t *testing.T) {
	fsys := testfs.NewReal(t)
	makeTarball(t, fsys.Path("package.tgz"), map[string]string{
		"file.txt": "hello\n",
	})

	if err := appcontainer.ExtractNPMTarball(io.Discard, appcontainer.ExtractNPMTarballInput{Dir: fsys.Root}); err != nil {
		t.Fatalf("ExtractNPMTarball: %v", err)
	}

	body := fsys.ReadFile("file.txt")
	if string(body) != "hello\n" {
		t.Errorf("file.txt = %q", body)
	}
}
