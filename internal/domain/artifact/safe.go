// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package artifact holds the transport-neutral safety rules for run
// artifacts. Both the github (zip) and forgejo (per-file) download
// transports route entry paths through here, so the path-traversal,
// symlink, and size guarantees live in one place rather than being
// re-implemented — and re-bugged — per forge.
package artifact

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// MaxFileBytes bounds a single extracted entry so a malicious or
// runaway archive cannot silently fill the disk. 5 GiB is far above any
// legitimate CI artifact file.
const MaxFileBytes int64 = 5 << 30

// MaxArchiveBytes bounds a compressed artifact response, while MaxTotalBytes
// and MaxFileCount bound the materialized result. The limits match the largest
// artifact class accepted by the supported forges while preventing an
// unbounded response, aggregate expansion, or tiny-file exhaustion.
const (
	MaxArchiveBytes int64 = 10 << 30
	MaxTotalBytes   int64 = 10 << 30
	MaxFileCount          = 10_000
)

// ValidateTotals checks an aggregate before the next file is materialized.
func ValidateTotals(totalBytes int64, fileCount int, nextBytes int64) error {
	return ValidateAggregate(totalBytes, fileCount, nextBytes, 1)
}

// ValidateAggregate checks a multi-file addition against the same limits.
func ValidateAggregate(totalBytes int64, fileCount int, addBytes int64, addFiles int) error {
	if totalBytes < 0 || addBytes < 0 || totalBytes > MaxTotalBytes || addBytes > MaxTotalBytes-totalBytes {
		return fmt.Errorf("artifact exceeds %d extracted bytes: %w", MaxTotalBytes, errs.ErrValidation)
	}

	if fileCount < 0 || addFiles < 0 || fileCount > MaxFileCount || addFiles > MaxFileCount-fileCount {
		return fmt.Errorf("artifact exceeds %d files: %w", MaxFileCount, errs.ErrValidation)
	}

	return nil
}

// CopyAtMost copies up to max bytes and rejects a source with even one byte
// more. Reading max+1 distinguishes an exactly-maximal file from a truncated
// oversized one. MaxInt64 is excluded so the lookahead count is representable.
func CopyAtMost(dst io.Writer, src io.Reader, maxBytes int64) (int64, error) {
	if maxBytes < 0 || maxBytes == math.MaxInt64 {
		return 0, fmt.Errorf("copy limit must be non-negative and less than MaxInt64: %w", errs.ErrUsage)
	}

	written, err := io.CopyN(dst, src, maxBytes+1)
	if written > maxBytes {
		return written, fmt.Errorf("file exceeds %d bytes: %w", maxBytes, errs.ErrValidation)
	}

	if err != nil && !errors.Is(err, io.EOF) {
		return written, err
	}

	return written, nil
}

// OpenUploadEntry reopens a collected upload without following a replaced
// leaf symlink. The identity and size checks bind validation to the same file
// descriptor the transport will read.
//
//nolint:cyclop // descriptor identity checks intentionally fail at each filesystem boundary.
func OpenUploadEntry(entry UploadEntry) (*os.File, error) {
	root, err := os.OpenRoot(filepath.Dir(entry.Abs))
	if err != nil {
		return nil, fmt.Errorf("open upload root for %q: %w", entry.Abs, err)
	}
	defer func() { _ = root.Close() }()

	name := filepath.Base(entry.Abs)

	current, err := root.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("stat upload %q: %w", entry.Abs, err)
	}

	if !current.Mode().IsRegular() || entry.info == nil || !os.SameFile(entry.info, current) {
		return nil, fmt.Errorf("upload file %q changed after collection: %w", entry.Abs, errs.ErrValidation)
	}

	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open upload %q: %w", entry.Abs, err)
	}

	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()

		return nil, fmt.Errorf("stat opened upload %q: %w", entry.Abs, err)
	}

	if !opened.Mode().IsRegular() || !os.SameFile(current, opened) || opened.Size() != entry.Size {
		_ = file.Close()

		return nil, fmt.Errorf("upload file %q changed while opening: %w", entry.Abs, errs.ErrValidation)
	}

	if opened.Size() > MaxFileBytes {
		_ = file.Close()

		return nil, fmt.Errorf("upload file %q exceeds %d bytes: %w", entry.Abs, MaxFileBytes, errs.ErrValidation)
	}

	return file, nil
}

// SafeJoin resolves an artifact entry path against destination root and
// returns the absolute target, rejecting anything unsafe: absolute
// entries, "..*" traversal, backslash segments (Windows-style paths from
// the runtime API), embedded newlines, and any result that escapes root.
// It is the single gate every downloaded entry must pass before a byte is
// written.
func SafeJoin(root, entry string) (string, error) {
	switch {
	case entry == "":
		return "", fmt.Errorf("empty artifact entry path: %w", errs.ErrValidation)
	case strings.ContainsAny(entry, "\n\r"):
		return "", fmt.Errorf("artifact entry path has embedded newline: %w", errs.ErrValidation)
	case strings.Contains(entry, `\`):
		return "", fmt.Errorf("artifact entry %q uses backslash separators: %w", entry, errs.ErrValidation)
	case filepath.IsAbs(entry):
		return "", fmt.Errorf("artifact entry %q is absolute: %w", entry, errs.ErrValidation)
	}

	for _, seg := range strings.Split(entry, "/") {
		if seg == ".." {
			return "", fmt.Errorf("artifact entry %q traverses parent directories: %w", entry, errs.ErrValidation)
		}
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve destination %q: %w", root, err)
	}

	dest := filepath.Join(absRoot, entry)
	if dest != absRoot && !strings.HasPrefix(dest, absRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact entry %q escapes destination: %w", entry, errs.ErrValidation)
	}

	return dest, nil
}

// ValidateName rejects an empty artifact name or one containing control
// characters (newlines, tabs, escape sequences, …) — the single-value guard
// both upload and download share before any API call. The control-character
// check matters beyond tidiness: a downloaded name also becomes a `dir/<name>/`
// path on disk and is echoed verbatim into CI logs / the operator's terminal,
// so allowing raw ESC/CR/etc. would be a terminal-escape (log-spoofing)
// injection vector — and for `download --pattern` the matched names come from
// the forge (a PR author's or another job's artifact), not the operator.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("artifact name must be non-empty: %w", errs.ErrValidation)
	}

	// Reject invalid UTF-8 first: a raw 0x9b byte is invalid UTF-8 yet can act
	// as a C1 CSI on an 8-bit terminal, and unicode.IsControl below only sees
	// it as the replacement rune. (A byte-level control scan can't be used —
	// UTF-8 continuation bytes overlap the C1 range, so it would reject
	// legitimate Cyrillic/accented names.)
	if !utf8.ValidString(name) {
		return fmt.Errorf("artifact name %q must be valid UTF-8: %w", name, errs.ErrValidation)
	}

	if strings.ContainsFunc(name, unicode.IsControl) {
		return fmt.Errorf("artifact name %q must not contain control characters: %w", name, errs.ErrValidation)
	}

	return nil
}
