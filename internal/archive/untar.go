// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package archive holds shared, security-hardened archive operations
// used across app/container and app/publish. Centralising the tarball
// extractor here means the symlink-escape, entry-escape, and
// decompression-bomb defenses live in one place — adding a new tarball
// consumer doesn't reintroduce gaps that already cost three audit
// rounds to close.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// maxTarballUncompressedSize caps the total bytes written across all
// entries during one extraction. Defense in depth against decompression
// bombs: even when each tar.Header.Size is honest, an archive with
// many large entries could still exhaust the runner's disk. 2 GiB is
// well above any legitimate artefact this pipeline extracts (npm
// tarballs are tens of MB, Maven JARs hundreds of MB) and refuses
// runaway aggregates without breaking real builds.
const maxTarballUncompressedSize int64 = 2 << 30

// UntarStripOne extracts a gzip-tar archive into dest, dropping the
// first path component of every entry — equivalent to
// `tar -xzf <file> --strip-components=1`.
//
// Hardenings applied at every layer:
//   - Each entry path is run through filepath.Clean and rejected if it
//     resolves outside dest.
//   - Symlink targets are rejected if absolute or if they resolve
//     outside dest (a later write through such a symlink would escape).
//   - Per-entry writes are bounded explicitly via io.CopyN(_, _, hdr.Size).
//   - Aggregate uncompressed size is bounded by maxTarballUncompressedSize.
func UntarStripOne(archive, dest string) error {
	f, err := os.Open(archive) //nolint:gosec,varnamelen // archive path is operator-supplied.
	if err != nil {
		return fmt.Errorf("open %s: %w", archive, err)
	}

	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}

	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)

	var totalBytes int64

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}

		stripped := stripFirstComponent(hdr.Name)
		if stripped == "" {
			continue // skip the archive root entry itself
		}

		target := filepath.Join(dest, stripped)
		if rel, relErr := filepath.Rel(dest, target); relErr != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("tar: refusing entry that escapes dest: %s: %w", hdr.Name, errs.ErrValidation)
		}

		written, err := writeTarEntry(tr, hdr, target, dest, totalBytes)
		if err != nil {
			return err
		}

		totalBytes += written
	}

	return nil
}

// writeTarEntry materializes one tar entry under target. Returns the
// number of bytes written for regular files (0 for dirs/symlinks) so
// the caller can keep the cumulative cap.
func writeTarEntry(tr *tar.Reader, hdr *tar.Header, target, dest string, totalBytes int64) (int64, error) {
	switch hdr.Typeflag {
	case tar.TypeDir:
		if err := os.MkdirAll(target, fs.FileMode(hdr.Mode)&0o755|0o700); err != nil { //nolint:gosec // hdr.Mode fits in uint32 by tar spec.
			return 0, fmt.Errorf("mkdir %s: %w", target, err)
		}

		return 0, nil

	case tar.TypeReg:
		return writeRegularEntry(tr, hdr, target, totalBytes)

	case tar.TypeSymlink:
		return 0, writeSymlinkEntry(hdr, target, dest)
	}

	return 0, nil
}

// writeRegularEntry writes one regular-file tar entry under target,
// honouring the cumulative byte cap.
func writeRegularEntry(tr *tar.Reader, hdr *tar.Header, target string, totalBytes int64) (int64, error) {
	if hdr.Size < 0 || totalBytes+hdr.Size > maxTarballUncompressedSize {
		return 0, fmt.Errorf("tar: archive exceeds %d uncompressed bytes: %w", maxTarballUncompressedSize, errs.ErrValidation)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil { //nolint:gosec // tar dest dir read by consumer; mode is escape-checked.
		return 0, fmt.Errorf("mkdir %s: %w", filepath.Dir(target), err)
	}

	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fs.FileMode(hdr.Mode)&0o777|0o600) //nolint:gosec // target is escape-checked above.
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", target, err)
	}
	// io.CopyN with hdr.Size makes the per-entry cap explicit
	// (tar.Reader already enforces it internally, but stating
	// it here removes the G110 false-positive cleanly).
	n, err := io.CopyN(out, tr, hdr.Size) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil && !errors.Is(err, io.EOF) {
		_ = out.Close()

		return 0, fmt.Errorf("copy %s: %w", target, err)
	}

	if err := out.Close(); err != nil {
		return 0, fmt.Errorf("close %s: %w", target, err)
	}

	return n, nil
}

// writeSymlinkEntry writes one tar symlink entry under target, rejecting
// absolute targets and any target that escapes dest.
func writeSymlinkEntry(hdr *tar.Header, target, dest string) error {
	if filepath.IsAbs(hdr.Linkname) {
		return fmt.Errorf("tar: refusing symlink with absolute target: %s -> %s: %w", hdr.Name, hdr.Linkname, errs.ErrValidation)
	}

	resolved := filepath.Join(filepath.Dir(target), hdr.Linkname) //nolint:gosec // escape-check below.
	if rel, relErr := filepath.Rel(dest, resolved); relErr != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("tar: refusing symlink that escapes dest: %s -> %s: %w", hdr.Name, hdr.Linkname, errs.ErrValidation)
	}

	if err := os.Symlink(hdr.Linkname, target); err != nil {
		return fmt.Errorf("symlink %s: %w", target, err)
	}

	return nil
}

// stripFirstComponent removes the first slash-separated path component.
// Returns "" when the input has no second component (matches the
// behaviour of `tar --strip-components=1` skipping the leading dir
// entry itself).
func stripFirstComponent(p string) string {
	clean := filepath.ToSlash(filepath.Clean(p))

	idx := strings.IndexByte(clean, '/')
	if idx < 0 {
		return ""
	}

	return clean[idx+1:]
}
