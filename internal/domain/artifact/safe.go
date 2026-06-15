// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package artifact holds the transport-neutral safety rules for run
// artifacts. Both the github (zip) and forgejo (per-file) download
// transports route entry paths through here, so the path-traversal,
// symlink, and size guarantees live in one place rather than being
// re-implemented — and re-bugged — per forge.
package artifact

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// MaxFileBytes bounds a single extracted entry so a malicious or
// runaway archive cannot silently fill the disk. 5 GiB is far above any
// legitimate CI artifact file.
const MaxFileBytes int64 = 5 << 30

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
