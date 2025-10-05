// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archive_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/archive"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// writeTarball lets a test write a tarball with arbitrary header fields
// so the path-traversal and symlink-escape defenses can be exercised.
// Each header must either be a TypeDir / TypeSymlink (no body) or carry
// a body that matches its Size.
func writeTarball(t *testing.T, path string, entries []tarEntry) {
	t.Helper()

	out, err := os.Create(path) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}

	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)

	for _, e := range entries { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		hdr := e.hdr
		if hdr.Typeflag == tar.TypeReg {
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

	// Both files, not just the top-level one: "strip one" has to keep the
	// directory structure below the stripped prefix, and only the nested
	// entry can show that.
	for path, want := range map[string]string{
		"out/package.json": `{"name":"demo"}`,
		"out/dist/cli.js":  "ok",
	} {
		if got := string(fsys.ReadFile(path)); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}

	// And the stripped prefix is gone, rather than merely also present.
	if _, err := os.Stat(fsys.Path("out", "package")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the stripped prefix survived into the destination: %v", err)
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

	link := fsys.Path("out", "cli")

	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink, not a regular file: info=%v err=%v", info, err)
	}

	if target, err := os.Readlink(link); err != nil || target != "dist/cli.js" {
		t.Fatalf("target=%q err=%v", target, err)
	}

	if body, err := os.ReadFile(link); err != nil || string(body) != "ok" {
		t.Fatalf("resolved content=%q err=%v", body, err)
	}
}

func TestUntarStripOne_RejectsTraversalBeforeStripping(t *testing.T) {
	fsys := testfs.NewReal(t)
	archivePath := fsys.Path("evil.tgz")
	writeTarball(t, archivePath, []tarEntry{
		{hdr: tar.Header{Name: "package/../../escaped", Mode: 0o644, Typeflag: tar.TypeReg}, body: "bad"},
	})

	err := archive.UntarStripOne(archivePath, fsys.MkdirAll("out"))
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}

	if _, err := os.Stat(fsys.Path("escaped")); !os.IsNotExist(err) {
		t.Fatalf("traversal created a file outside destination: %v", err)
	}
}

func TestUntarStripOne_RejectsWriteThroughPreexistingSymlink(t *testing.T) {
	fsys := testfs.NewReal(t)
	archivePath := fsys.Path("evil.tgz")
	dest := fsys.MkdirAll("out")

	outside := fsys.MkdirAll("outside")
	if err := os.Symlink(outside, filepath.Join(dest, "linked")); err != nil {
		t.Fatal(err)
	}

	writeTarball(t, archivePath, []tarEntry{
		{hdr: tar.Header{Name: "package/linked/escaped", Mode: 0o644, Typeflag: tar.TypeReg}, body: "bad"},
	})

	// os.Root refuses the traversal at open time, so this arrives as the
	// wrapped filesystem error rather than one of the explicit tar refusals
	// above; what matters is that it is refused and nothing is written.
	if err := archive.UntarStripOne(archivePath, dest); err == nil {
		t.Fatal("expected extraction through an escaping symlink to fail")
	} else if !strings.Contains(err.Error(), "escaped") && !strings.Contains(err.Error(), "linked") {
		t.Errorf("err = %v, want it to name the entry it refused", err)
	}

	if _, err := os.Stat(filepath.Join(outside, "escaped")); !os.IsNotExist(err) {
		t.Fatalf("symlink escape created a file outside destination: %v", err)
	}
}

func TestUntarStripOne_RejectsHardLinks(t *testing.T) {
	fsys := testfs.NewReal(t)
	archivePath := fsys.Path("evil.tgz")
	writeTarball(t, archivePath, []tarEntry{
		{hdr: tar.Header{Name: "package/link", Linkname: "package/target", Typeflag: tar.TypeLink}},
	})

	err := archive.UntarStripOne(archivePath, fsys.MkdirAll("out"))
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestUntarStripOne_RejectsSymlinkedDestinationRootOrAncestor(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	archivePath := filepath.Join(base, "demo.tgz")
	writeTarball(t, archivePath, []tarEntry{
		{hdr: tar.Header{Name: "package/value", Mode: 0o644, Typeflag: tar.TypeReg}, body: "bad"},
	})

	realDest := filepath.Join(base, "real")
	if err := os.MkdirAll(realDest, 0o755); err != nil {
		t.Fatal(err)
	}

	linked := filepath.Join(base, "linked")
	if err := os.Symlink(realDest, linked); err != nil {
		t.Fatal(err)
	}

	for _, dest := range []string{linked, filepath.Join(linked, "child")} {
		err := archive.UntarStripOne(archivePath, dest)
		if !errors.Is(err, errs.ErrValidation) {
			t.Errorf("destination %q error = %v, want ErrValidation", dest, err)
		}
	}

	if _, err := os.Stat(filepath.Join(realDest, "value")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink destination was written through: %v", err)
	}
}

func TestUntarStripOne_RejectsSymlinkArchive(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	realArchive := filepath.Join(base, "demo.tgz")
	writeTarball(t, realArchive, []tarEntry{
		{hdr: tar.Header{Name: "package/value", Mode: 0o644, Typeflag: tar.TypeReg}, body: "value"},
	})

	linkedArchive := filepath.Join(base, "linked.tgz")
	if err := os.Symlink(realArchive, linkedArchive); err != nil {
		t.Fatal(err)
	}

	err := archive.UntarStripOne(linkedArchive, filepath.Join(base, "out"))
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("symlink archive error = %v, want ErrValidation", err)
	}
}

// TestUntarStripOne_RejectsATamperedGzipTrailerWithoutPublishing covers the
// integrity check that runs after every entry has already been read.
//
// The other refusal tests present a well-formed archive carrying something
// disallowed, so they fail before anything is staged. This one is the opposite
// shape and the more interesting one: the tar stream decodes cleanly, every
// entry is staged, and only the gzip checksum at the end says the bytes were
// altered. Ignoring that error publishes a tampered archive in full — the
// entries are all there and all look fine.
func TestUntarStripOne_RejectsATamperedGzipTrailerWithoutPublishing(t *testing.T) {
	fsys := testfs.NewReal(t)
	archivePath := fsys.Path("demo.tgz")
	dest := fsys.Path("out")

	writeTarball(t, archivePath, []tarEntry{
		{hdr: tar.Header{Name: "package/first.txt", Mode: 0o644, Typeflag: tar.TypeReg}, body: "first"},
		{hdr: tar.Header{Name: "package/second.txt", Mode: 0o644, Typeflag: tar.TypeReg}, body: "second"},
	})

	body, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}

	// The gzip trailer is the last eight bytes: CRC32 then ISIZE. Flipping a
	// CRC bit leaves the compressed stream fully decodable and makes only the
	// integrity check disagree.
	if len(body) < 8 {
		t.Fatalf("archive is unexpectedly small (%d bytes)", len(body))
	}

	body[len(body)-8] ^= 0xff

	//nolint:gosec // G703: archivePath is this test's own t.TempDir() fixture.
	if writeErr := os.WriteFile(archivePath, body, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	err = archive.UntarStripOne(archivePath, dest)
	if err == nil {
		t.Fatal("an archive whose checksum does not match its contents was extracted")
	}

	if !strings.Contains(err.Error(), "gzip trailer") {
		t.Errorf("err = %v, want it to name the trailer check", err)
	}

	entries, readErr := os.ReadDir(dest)
	if readErr != nil {
		t.Fatalf("destination unreadable: %v", readErr)
	}

	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}

		t.Errorf("a tampered archive published %v", names)
	}
}

// TestUntarStripOne_RejectsUnsupportedEntryTypes covers the default branch of
// the entry-type switch, which every existing case reaches only through the
// hard-link row.
//
// A tar can name device nodes and FIFOs. Creating either from an untrusted
// archive is a way to place something in the destination that is not a file at
// all, and a later step reading it can block forever on a FIFO.
func TestUntarStripOne_RejectsUnsupportedEntryTypes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		typeflag byte
	}{
		{name: "fifo", typeflag: tar.TypeFifo},
		{name: "character device", typeflag: tar.TypeChar},
		{name: "block device", typeflag: tar.TypeBlock},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			archivePath := fsys.Path("demo.tgz")
			dest := fsys.Path("out")

			writeTarball(t, archivePath, []tarEntry{
				{hdr: tar.Header{Name: "package/node", Mode: 0o644, Typeflag: tc.typeflag}},
			})

			if err := archive.UntarStripOne(archivePath, dest); !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("UntarStripOne error = %v, want ErrValidation for a %s entry", err, tc.name)
			}
		})
	}
}

// TestUntarStripOne_DuplicateDestinationKeepsTheLastEntry pins what happens
// when one archive names the same path twice.
//
// It is a shape a malicious or merely sloppy archive can have, and the two
// plausible answers — refuse, or let the later entry win — differ in what the
// destination ends up holding. Leaving it unstated means nobody knows which
// bytes a duplicate publishes.
func TestUntarStripOne_DuplicateDestinationKeepsTheLastEntry(t *testing.T) {
	fsys := testfs.NewReal(t)
	archivePath := fsys.Path("demo.tgz")
	dest := fsys.Path("out")

	writeTarball(t, archivePath, []tarEntry{
		{hdr: tar.Header{Name: "package/dup.txt", Mode: 0o644, Typeflag: tar.TypeReg}, body: "first"},
		{hdr: tar.Header{Name: "package/dup.txt", Mode: 0o644, Typeflag: tar.TypeReg}, body: "second"},
	})

	if err := archive.UntarStripOne(archivePath, dest); err != nil {
		t.Fatalf("UntarStripOne: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "dup.txt"))
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != "second" {
		t.Errorf("duplicate destination holds %q, want %q (the later entry)", got, "second")
	}
}

// TestUntarStripOne_ClassifiesBadInputAndKeepsTheCause covers the three ways an
// archive input is wrong without breaking a safety rule: it is not there, it is
// not gzip, or it is gzip around something that is not a tar stream.
//
// None carried a class, and two of the three callers wrap without adding one,
// so each surfaced as EX_SOFTWARE (70) -- the exit that tells an operator to
// file a bug -- for a tarball that was never produced or was corrupt. The
// decoder's own error stays in the chain, since that is what explains which
// kind of corrupt it was. Nothing is published for any of them.
func TestUntarStripOne_ClassifiesBadInputAndKeepsTheCause(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	notGzip := filepath.Join(dir, "not-gzip.tgz")
	if err := os.WriteFile(notGzip, []byte("plain text, no gzip header"), 0o600); err != nil {
		t.Fatal(err)
	}

	var compressed bytes.Buffer

	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write([]byte("gzip-wrapped bytes that are not a tar header")); err != nil {
		t.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	notTar := filepath.Join(dir, "not-tar.tgz")
	if err := os.WriteFile(notTar, compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		archive string
		class   error
		cause   error
	}{
		"missing":      {archive: filepath.Join(dir, "absent.tgz"), class: errs.ErrMissingInput, cause: fs.ErrNotExist},
		"not gzip":     {archive: notGzip, class: errs.ErrMalformedInput, cause: gzip.ErrHeader},
		"gzip not tar": {archive: notTar, class: errs.ErrMalformedInput, cause: io.ErrUnexpectedEOF},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dest := filepath.Join(dir, "out-"+strings.ReplaceAll(name, " ", "-"))

			err := archive.UntarStripOne(tc.archive, dest)
			if !errors.Is(err, tc.class) {
				t.Errorf("err = %v, want class %v", err, tc.class)
			}

			if !errors.Is(err, tc.cause) {
				t.Errorf("err = %v, want the decoder's cause %v kept", err, tc.cause)
			}

			// The destination may exist -- staging creates it before the first
			// entry is read -- but a refusal must leave nothing in it, the same
			// contract the tampered-trailer test holds.
			entries, readErr := os.ReadDir(dest)
			if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
				t.Fatalf("destination unreadable: %v", readErr)
			}

			if len(entries) != 0 {
				t.Errorf("a refused archive left %d entr(y/ies) in %s", len(entries), dest)
			}
		})
	}
}
