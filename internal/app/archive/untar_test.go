// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package archive_test

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"os"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/app/archive"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

// writeTarball lets a test write a tarball with arbitrary header fields
// so the path-traversal and symlink-escape defenses can be exercised.
// Each header must either be a TypeDir / TypeSymlink (no body) or carry
// a body that matches its Size.
func writeTarball(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := e.hdr
		if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA {
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatal(err)
		}
		if len(e.body) > 0 {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
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

type tarEntry struct {
	hdr  tar.Header
	body string
}

func TestUntarStripOne_HappyPath(t *testing.T) {
	fsys := testfs.NewReal(t)
	archivePath := fsys.Path("demo.tgz")
	dest := fsys.MkdirAll("out")
	writeTarball(t, archivePath, []tarEntry{
		{hdr: tar.Header{Name: "package/package.json", Mode: 0o644, Typeflag: tar.TypeReg}, body: `{"name":"demo"}`},
		{hdr: tar.Header{Name: "package/dist/", Mode: 0o755, Typeflag: tar.TypeDir}},
		{hdr: tar.Header{Name: "package/dist/cli.js", Mode: 0o644, Typeflag: tar.TypeReg}, body: "ok"},
	})
	if err := archive.UntarStripOne(archivePath, dest); err != nil {
		t.Fatalf("UntarStripOne: %v", err)
	}
	body := fsys.ReadFile("out/package.json")
	if string(body) != `{"name":"demo"}` {
		t.Errorf("package.json = %q", body)
	}
}

func TestUntarStripOne_RejectsSymlinkWithAbsoluteTarget(t *testing.T) {
	fsys := testfs.NewReal(t)
	archivePath := fsys.Path("evil.tgz")
	writeTarball(t, archivePath, []tarEntry{
		{hdr: tar.Header{Name: "package/link", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink}},
	})
	err := archive.UntarStripOne(archivePath, fsys.Root)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestUntarStripOne_RejectsSymlinkThatEscapesDest(t *testing.T) {
	fsys := testfs.NewReal(t)
	archivePath := fsys.Path("evil.tgz")
	writeTarball(t, archivePath, []tarEntry{
		{hdr: tar.Header{Name: "package/link", Linkname: "../../etc/passwd", Typeflag: tar.TypeSymlink}},
	})
	err := archive.UntarStripOne(archivePath, fsys.Root)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestUntarStripOne_AllowsBenignRelativeSymlink(t *testing.T) {
	fsys := testfs.NewReal(t)
	archivePath := fsys.Path("demo.tgz")
	dest := fsys.MkdirAll("out")
	writeTarball(t, archivePath, []tarEntry{
		{hdr: tar.Header{Name: "package/dist/", Mode: 0o755, Typeflag: tar.TypeDir}},
		{hdr: tar.Header{Name: "package/dist/cli.js", Mode: 0o644, Typeflag: tar.TypeReg}, body: "ok"},
		{hdr: tar.Header{Name: "package/cli", Linkname: "dist/cli.js", Typeflag: tar.TypeSymlink}},
	})
	if err := archive.UntarStripOne(archivePath, dest); err != nil {
		t.Fatalf("UntarStripOne: %v", err)
	}
	if _, err := os.Lstat(fsys.Path("out", "cli")); err != nil {
		t.Errorf("expected symlink to be created: %v", err)
	}
}
