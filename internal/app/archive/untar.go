// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

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
	f, err := os.Open(archive)
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
		if err == io.EOF {
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

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, fs.FileMode(hdr.Mode)&0o755|0o700); err != nil {
				return fmt.Errorf("mkdir %s: %w", target, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if hdr.Size < 0 || totalBytes+hdr.Size > maxTarballUncompressedSize {
				return fmt.Errorf("tar: archive exceeds %d uncompressed bytes: %w", maxTarballUncompressedSize, errs.ErrValidation)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", filepath.Dir(target), err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fs.FileMode(hdr.Mode)&0o777|0o600)
			if err != nil {
				return fmt.Errorf("open %s: %w", target, err)
			}
			// io.CopyN with hdr.Size makes the per-entry cap explicit
			// (tar.Reader already enforces it internally, but stating
			// it here removes the G110 false-positive cleanly).
			n, err := io.CopyN(out, tr, hdr.Size)
			if err != nil && err != io.EOF {
				_ = out.Close()
				return fmt.Errorf("copy %s: %w", target, err)
			}
			totalBytes += n
			if err := out.Close(); err != nil {
				return fmt.Errorf("close %s: %w", target, err)
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(hdr.Linkname) {
				return fmt.Errorf("tar: refusing symlink with absolute target: %s -> %s: %w", hdr.Name, hdr.Linkname, errs.ErrValidation)
			}
			resolved := filepath.Join(filepath.Dir(target), hdr.Linkname)
			if rel, relErr := filepath.Rel(dest, resolved); relErr != nil || strings.HasPrefix(rel, "..") {
				return fmt.Errorf("tar: refusing symlink that escapes dest: %s -> %s: %w", hdr.Name, hdr.Linkname, errs.ErrValidation)
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("symlink %s: %w", target, err)
			}
		}
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
