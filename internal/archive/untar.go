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
	"path"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// maxTarballUncompressedSize caps the total bytes written across all
// entries during one extraction. Defense in depth against decompression
// bombs: even when each tar.Header.Size is honest, an archive with
// many large entries could still exhaust the runner's disk. 2 GiB is
// well above any legitimate artifact this pipeline extracts (npm
// tarballs are tens of MB, Maven JARs hundreds of MB) and refuses
// runaway aggregates without breaking real builds.
const (
	maxTarballBodySize         int64 = 2 << 30
	maxTarballUncompressedSize int64 = 2 << 30
	maxTarballEntries                = 100_000
	// Bound the whole stream (including tar headers) and padding after tar EOF.
	maxTarballStreamSize = maxTarballUncompressedSize + maxTarballEntries*1024
	maxTarballPadding    = 1 << 20
)

// tarLimits is the set of resource caps one extraction enforces.
//
// They exist as a struct rather than as bare constants so they can be
// exercised at their own boundaries. Every one of these is a
// decompression-bomb defence and none of them had a test, because proving
// what happens at 2 GiB costs 2 GiB of I/O and proving what happens at
// 100,000 entries costs 100,000 inodes — per run, on every developer
// machine. Scaling the caps down for a test is the only way to check the
// arithmetic of the comparison itself: whether the cap is inclusive, which
// counter it is compared against, and whether the refusal happens before or
// after the bytes land.
//
// Production behaviour is unchanged: UntarStripOne uses defaultTarLimits,
// nothing here is exported, and the values are the same constants as before.
type tarLimits struct {
	bodySize         int64
	uncompressedSize int64
	entries          int
	streamSize       int64
	padding          int64
}

func defaultTarLimits() tarLimits {
	return tarLimits{
		bodySize:         maxTarballBodySize,
		uncompressedSize: maxTarballUncompressedSize,
		entries:          maxTarballEntries,
		streamSize:       maxTarballStreamSize,
		padding:          maxTarballPadding,
	}
}

// UntarStripOne extracts a gzip-tar archive into dest, dropping the
// first path component of every entry — equivalent to
// `tar -xzf <file> --strip-components=1`.
//
// Hardenings applied at every layer:
//   - Parent traversal is rejected before strip-components processing.
//   - All filesystem operations run through descriptor-backed os.Root.
//   - Symlink targets are rejected if absolute or if they resolve
//     outside dest (a later write through such a symlink would escape).
//   - Per-entry writes are bounded explicitly via io.CopyN(_, _, hdr.Size).
//   - Aggregate uncompressed size is bounded by maxTarballUncompressedSize.
//   - Entries are validated in private staging before any destination replacement.
//
//nolint:cyclop // extraction keeps each archive safety invariant explicit.
func UntarStripOne(archive, dest string) error {
	return untarStripOne(archive, dest, defaultTarLimits())
}

// inspectArchive stats the archive path without following a link. A missing
// archive is the caller's input not being there, so it is classified here: two
// of the three callers wrap without a sentinel, and an unclassified error
// reached the operator as EX_SOFTWARE (70), the exit reserved for defects in
// this tool, for a tarball that was never produced.
func inspectArchive(archive string) (fs.FileInfo, error) {
	info, err := os.Lstat(archive)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("inspect %s: %w: %w", archive, err, errs.ErrMissingInput)
	}

	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", archive, err)
	}

	return info, nil
}

//nolint:cyclop // extraction keeps each archive safety invariant explicit.
func untarStripOne(archive, dest string, limits tarLimits) error {
	archiveInfo, err := inspectArchive(archive)
	if err != nil {
		return err
	}

	if !archiveInfo.Mode().IsRegular() || archiveInfo.Size() > limits.bodySize {
		return fmt.Errorf("archive %s must be a regular file no larger than %d bytes: %w", archive, limits.bodySize, errs.ErrValidation)
	}

	f, err := os.Open(archive) //nolint:gosec,varnamelen // archive path is operator-supplied.
	if err != nil {
		return fmt.Errorf("open %s: %w", archive, err)
	}

	defer func() { _ = f.Close() }()

	openedInfo, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat opened archive %s: %w", archive, err)
	}

	if !openedInfo.Mode().IsRegular() || !os.SameFile(archiveInfo, openedInfo) || openedInfo.Size() > limits.bodySize {
		return fmt.Errorf("archive %s changed while opening: %w", archive, errs.ErrValidation)
	}

	compressed := &io.LimitedReader{R: f, N: limits.bodySize + 1}

	// Stream errors from the gzip and tar readers are malformed input: the
	// archive exists and cannot be read as what it claims to be. The decoder's
	// own error is kept as the cause.
	gz, err := gzip.NewReader(compressed)
	if err != nil {
		return fmt.Errorf("gzip: %w: %w", err, errs.ErrMalformedInput)
	}

	defer func() { _ = gz.Close() }()

	staging, err := pathsafe.NewArtifactStaging(dest)
	if err != nil {
		return fmt.Errorf("open tar destination %s: %w", dest, err)
	}
	defer func() { _ = staging.Close() }()

	root := staging.Root()

	expanded := &io.LimitedReader{R: gz, N: limits.streamSize + 1}
	tr := tar.NewReader(expanded)

	var totalBytes int64

	entryCount := 0

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			// A stream that hit its expanded budget runs out mid-header, so
			// tar reports a truncated archive rather than this package
			// reporting a limit. Both are refusals, but only one tells the
			// operator that the archive was too big rather than corrupt, and
			// only one carries the same sentinel as every other cap here.
			if expanded.N == 0 {
				return fmt.Errorf("tar: archive exceeds %d expanded bytes: %w", limits.streamSize, errs.ErrValidation)
			}

			return fmt.Errorf("tar next: %w: %w", err, errs.ErrMalformedInput)
		}

		entryCount++
		if entryCount > limits.entries {
			return fmt.Errorf("tar: archive exceeds %d entries: %w", limits.entries, errs.ErrValidation)
		}

		stripped, err := stripFirstComponent(hdr.Name)
		if err != nil {
			return err
		}

		if stripped == "" {
			continue // skip the archive root entry itself
		}

		written, err := writeTarEntry(root, tr, hdr, filepath.FromSlash(stripped), totalBytes, limits)
		if err != nil {
			return err
		}

		totalBytes += written
	}

	// Tar EOF precedes the gzip trailer. Read to gzip EOF before committing,
	// with a separate padding budget so ignored trailing data cannot grow freely.
	//
	// Three separate limits are checked here, and each says which one it was.
	// They used to share one message naming all three, which meant a test
	// could not tell them apart — and one written to exercise the compressed
	// cap passed on the padding cap's refusal instead, proving nothing about
	// the limit it named.
	padding, trailerErr := io.CopyN(io.Discard, expanded, limits.padding+1)
	if padding > limits.padding {
		return fmt.Errorf("tar: archive exceeds %d bytes of trailing padding: %w", limits.padding, errs.ErrValidation)
	}

	if expanded.N == 0 {
		return fmt.Errorf("tar: archive exceeds %d expanded bytes: %w", limits.streamSize, errs.ErrValidation)
	}

	if compressed.N == 0 {
		return fmt.Errorf("tar: archive exceeds %d compressed bytes: %w", limits.bodySize, errs.ErrValidation)
	}

	if !errors.Is(trailerErr, io.EOF) {
		return fmt.Errorf("gzip trailer: %w: %w", trailerErr, errs.ErrMalformedInput)
	}

	return staging.InstallWithRelativeSymlinks()
}

// writeTarEntry materializes one tar entry under target. Returns the
// number of bytes written for regular files (0 for dirs/symlinks) so
// the caller can keep the cumulative cap.
func writeTarEntry(root *os.Root, tr *tar.Reader, hdr *tar.Header, target string, totalBytes int64, limits tarLimits) (int64, error) {
	switch hdr.Typeflag {
	case tar.TypeDir:
		if err := root.MkdirAll(target, fs.FileMode(hdr.Mode)&0o755|0o700); err != nil { //nolint:gosec // hdr.Mode fits in uint32 by tar spec.
			return 0, fmt.Errorf("mkdir %s: %w", target, err)
		}

		return 0, nil

	case tar.TypeReg:
		return writeRegularEntry(root, tr, hdr, target, totalBytes, limits)

	case tar.TypeSymlink:
		return 0, writeSymlinkEntry(root, hdr, target)

	default:
		return 0, fmt.Errorf("tar: unsupported entry type %d for %s: %w", hdr.Typeflag, hdr.Name, errs.ErrValidation)
	}
}

// writeRegularEntry writes one regular-file tar entry under target,
// honouring the cumulative byte cap.
func writeRegularEntry(root *os.Root, tr *tar.Reader, hdr *tar.Header, target string, totalBytes int64, limits tarLimits) (int64, error) {
	if hdr.Size < 0 || hdr.Size > limits.uncompressedSize-totalBytes {
		return 0, fmt.Errorf("tar: archive exceeds %d uncompressed bytes: %w", limits.uncompressedSize, errs.ErrValidation)
	}

	if err := root.MkdirAll(filepath.Dir(target), 0o755); err != nil { //nolint:gosec // tar dest dir read by consumer; os.Root contains it.
		return 0, fmt.Errorf("mkdir %s: %w", filepath.Dir(target), err)
	}

	// Remove the leaf first so an existing symlink is replaced rather than
	// followed. Parent symlinks are resolved safely by os.Root.
	if err := root.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, fmt.Errorf("replace %s: %w", target, err)
	}

	out, err := root.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fs.FileMode(hdr.Mode)&0o777|0o600) //nolint:gosec // os.Root contains target.
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", target, err)
	}

	complete := false
	defer func() {
		if !complete {
			_ = out.Close()
			_ = root.Remove(target)
		}
	}()

	// io.CopyN with hdr.Size makes the per-entry cap explicit
	// (tar.Reader already enforces it internally, but stating
	// it here removes the G110 false-positive cleanly).
	n, err := io.CopyN(out, tr, hdr.Size) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		return 0, fmt.Errorf("copy %s: %w", target, err)
	}

	if err := out.Close(); err != nil {
		return 0, fmt.Errorf("close %s: %w", target, err)
	}

	complete = true

	return n, nil
}

// writeSymlinkEntry writes one tar symlink entry under target, rejecting
// absolute targets and any target that escapes dest.
func writeSymlinkEntry(root *os.Root, hdr *tar.Header, target string) error {
	linkname := filepath.ToSlash(hdr.Linkname)
	if path.IsAbs(linkname) {
		return fmt.Errorf("tar: refusing symlink with absolute target: %s -> %s: %w", hdr.Name, hdr.Linkname, errs.ErrValidation)
	}

	resolved := path.Clean(path.Join(filepath.ToSlash(filepath.Dir(target)), linkname))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return fmt.Errorf("tar: refusing symlink that escapes dest: %s -> %s: %w", hdr.Name, hdr.Linkname, errs.ErrValidation)
	}

	if err := root.Symlink(hdr.Linkname, target); err != nil {
		return fmt.Errorf("symlink %s: %w", target, err)
	}

	return nil
}

// stripFirstComponent removes the first slash-separated path component.
// Returns "" when the input has no second component (matches the
// behaviour of `tar --strip-components=1` skipping the leading dir
// entry itself).
func stripFirstComponent(name string) (string, error) {
	name = filepath.ToSlash(name)
	if path.IsAbs(name) {
		return "", fmt.Errorf("tar: refusing absolute entry: %s: %w", name, errs.ErrValidation)
	}

	for _, component := range strings.Split(name, "/") {
		if component == ".." {
			return "", fmt.Errorf("tar: refusing entry with parent traversal: %s: %w", name, errs.ErrValidation)
		}
	}

	clean := path.Clean(name)

	idx := strings.IndexByte(clean, '/')
	if idx < 0 {
		return "", nil
	}

	return clean[idx+1:], nil
}
