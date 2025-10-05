// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archive

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// Every cap in tarLimits is a decompression-bomb defence, and none of them had
// a test. The caps themselves cannot be exercised at their real values: 2 GiB
// of I/O and 100,000 inodes per run is not something to put in a unit suite,
// and a test that costs that much gets deleted rather than maintained.
//
// What is worth checking is the arithmetic, and that is independent of the
// magnitude: whether the comparison is inclusive or exclusive, which counter it
// reads, and whether the refusal happens before or after the bytes reach the
// disk. Each limit below is therefore scaled down and probed on both sides —
// exactly at the cap, which must extract, and one unit past it, which must
// refuse. An off-by-one in either direction is a real defect: too strict
// rejects a legitimate artifact, too loose leaves the bomb defence one byte
// short of what it claims.

// Two caps are deliberately not probed here.
//
// The negative declared size that writeRegularEntry also refuses cannot be
// produced through this door: archive/tar's writer will not encode one, and its
// reader rejects a GNU base-256 negative size field as an invalid header before
// this package sees it. The check stays regardless — it costs one comparison
// and guards against a standard-library behaviour this package should not have
// to depend on — but a test for it would be asserting on the standard library
// rather than on this code.
//
// A header that declares more than it carries is the same story: the writer
// refuses to encode one. What matters about it is covered anyway, because the
// cumulative test below refuses on the DECLARED size, which is the field a
// decompression bomb inflates.

// tarballWithBodies writes a gzip-tar, taking each regular entry's body from
// bodies by index. Bodies shorter than the declared Size are written as-is so
// a header can deliberately lie about its length.
func tarballWithBodies(t *testing.T, entries []*tar.Header, bodies map[int]string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "archive.tar.gz")

	out, err := os.Create(path) //nolint:gosec // test fixture in t.TempDir().
	if err != nil {
		t.Fatal(err)
	}

	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)

	for i, hdr := range entries {
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}

		if body, ok := bodies[i]; ok {
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
	}

	for _, closer := range []func() error{tw.Close, gz.Close, out.Close} {
		if err := closer(); err != nil {
			t.Fatal(err)
		}
	}

	return path
}

// regular returns a regular-file header under the stripped-away root.
func regular(name string, size int64) *tar.Header {
	return &tar.Header{Typeflag: tar.TypeReg, Name: "root/" + name, Size: size, Mode: 0o644}
}

func TestTarLimits_EntryCountBoundary(t *testing.T) {
	t.Parallel()

	// The archive root entry is skipped after the strip, so only the files
	// count. Three files against a cap of three must extract.
	headers := []*tar.Header{
		{Typeflag: tar.TypeDir, Name: "root/"},
		regular("a", 1), regular("b", 1), regular("c", 1),
	}
	bodies := map[int]string{1: "a", 2: "b", 3: "c"}

	limits := defaultTarLimits()
	limits.entries = len(headers) // the directory entry counts too

	if err := untarStripOne(tarballWithBodies(t, headers, bodies), filepath.Join(t.TempDir(), "dest"), limits); err != nil {
		t.Fatalf("an archive exactly at the entry cap was refused: %v", err)
	}

	limits.entries = len(headers) - 1

	err := untarStripOne(tarballWithBodies(t, headers, bodies), filepath.Join(t.TempDir(), "dest2"), limits)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation for one entry over the cap", err)
	}

	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("err = %v, want it to name the limit", err)
	}
}

func TestTarLimits_UncompressedSizeBoundary(t *testing.T) {
	t.Parallel()

	// Two entries so the cap is proven to apply to the CUMULATIVE total and
	// not to each entry on its own — the difference between bounding one
	// file and bounding the archive.
	headers := []*tar.Header{regular("a", 8), regular("b", 8)}
	bodies := map[int]string{0: "aaaaaaaa", 1: "bbbbbbbb"}

	limits := defaultTarLimits()
	limits.uncompressedSize = 16

	if err := untarStripOne(tarballWithBodies(t, headers, bodies), filepath.Join(t.TempDir(), "dest"), limits); err != nil {
		t.Fatalf("an archive exactly at the uncompressed cap was refused: %v", err)
	}

	limits.uncompressedSize = 15

	dest := filepath.Join(t.TempDir(), "dest2")
	if err := untarStripOne(tarballWithBodies(t, headers, bodies), dest, limits); !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation one byte over the cumulative cap", err)
	}

	// The refusal must publish nothing. Extraction writes into a private
	// staging directory inside the destination and renames entries out only
	// on success, so the destination is created but stays empty — and the
	// staging directory is removed rather than left for the next run to
	// find. A half-extracted archive is the failure this ordering exists to
	// prevent, and it is invisible unless the destination is inspected.
	entries, readErr := os.ReadDir(dest)
	if readErr != nil {
		t.Fatalf("destination unreadable after refusal: %v", readErr)
	}

	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}

		t.Errorf("the refused extraction published %v into %s", names, dest)
	}
}

func TestTarLimits_TrailingPaddingBoundary(t *testing.T) {
	t.Parallel()

	// Padding is whatever the gzip stream carries after the tar EOF marker.
	// It is ignored on extraction, which is exactly why it needs its own
	// budget: unbudgeted, it is free space for an arbitrarily large payload
	// inside an otherwise tiny-looking archive.
	build := func(t *testing.T, padding int) string {
		t.Helper()

		path := filepath.Join(t.TempDir(), "archive.tar.gz")

		out, err := os.Create(path) //nolint:gosec // test fixture in t.TempDir().
		if err != nil {
			t.Fatal(err)
		}

		gz := gzip.NewWriter(out)
		tw := tar.NewWriter(gz)

		if err := tw.WriteHeader(regular("a", 1)); err != nil {
			t.Fatal(err)
		}

		if _, err := tw.Write([]byte("a")); err != nil {
			t.Fatal(err)
		}

		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}

		if _, err := gz.Write([]byte(strings.Repeat("P", padding))); err != nil {
			t.Fatal(err)
		}

		for _, closer := range []func() error{gz.Close, out.Close} {
			if err := closer(); err != nil {
				t.Fatal(err)
			}
		}

		return path
	}

	limits := defaultTarLimits()
	limits.padding = 64

	if err := untarStripOne(build(t, 64), filepath.Join(t.TempDir(), "dest"), limits); err != nil {
		t.Fatalf("padding exactly at the cap was refused: %v", err)
	}

	err := untarStripOne(build(t, 65), filepath.Join(t.TempDir(), "dest2"), limits)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation one byte over the padding cap", err)
	}

	if !strings.Contains(err.Error(), "trailing padding") {
		t.Errorf("err = %v, want the padding refusal, not another limit's", err)
	}
}

// TestTarLimits_ArchiveFileSizeBoundary covers the cap applied before the file
// is opened at all, on the compressed bytes on disk.
func TestTarLimits_ArchiveFileSizeBoundary(t *testing.T) {
	t.Parallel()

	archive := tarballWithBodies(t, []*tar.Header{regular("a", 1)}, map[int]string{0: "a"})

	info, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}

	limits := defaultTarLimits()
	limits.bodySize = info.Size()

	if refusal := untarStripOne(archive, filepath.Join(t.TempDir(), "dest"), limits); refusal != nil {
		t.Fatalf("an archive exactly at the file-size cap was refused: %v", refusal)
	}

	limits.bodySize = info.Size() - 1

	err = untarStripOne(archive, filepath.Join(t.TempDir(), "dest2"), limits)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation one byte over the file-size cap", err)
	}

	// Naming the limit is what makes this test about the file-size cap. The
	// three stream limits used to share one message, and an earlier draft of
	// this test passed with BOTH file-size checks removed: the refusal was
	// coming from the compressed-bytes cap and the assertion could not tell.
	if !strings.Contains(err.Error(), "no larger than") {
		t.Errorf("err = %v, want the file-size refusal, not another limit's", err)
	}
}

// TestTarLimits_ExpandedStreamBoundary covers the cap on the decompressed
// stream, which bounds headers and padding as well as file bodies — the
// uncompressed cap alone counts only what reaches a file.
func TestTarLimits_ExpandedStreamBoundary(t *testing.T) {
	t.Parallel()

	archive := tarballWithBodies(t, []*tar.Header{regular("a", 1)}, map[int]string{0: "a"})

	limits := defaultTarLimits()
	limits.streamSize = 4 // far below one 512-byte tar header

	err := untarStripOne(archive, filepath.Join(t.TempDir(), "dest"), limits)
	if err == nil {
		t.Fatal("an archive whose expanded stream exceeds the cap was extracted")
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
}

// TestTarLimits_CompressedBytesCapIsItsOwnRefusal keeps the third stream limit
// distinguishable from the other two.
func TestTarLimits_CompressedBytesCapIsItsOwnRefusal(t *testing.T) {
	t.Parallel()

	archive := tarballWithBodies(t, []*tar.Header{regular("a", 1)}, map[int]string{0: "a"})

	info, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}

	limits := defaultTarLimits()
	// Large enough to pass the up-front file-size check, small enough that
	// the reader exhausts its compressed budget.
	limits.bodySize = info.Size()

	err = untarStripOne(archive, filepath.Join(t.TempDir(), "dest"), limits)
	if err == nil {
		return // the budget was exactly enough; nothing to assert
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
}
