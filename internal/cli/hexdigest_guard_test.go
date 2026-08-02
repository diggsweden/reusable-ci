// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// bareSHA256HexPattern is the bare "64 lowercase hex" regex — a sha256 with no
// `sha256:` prefix. The caret sits directly before the hex class, which the
// digest-pinned-ref patterns (`…:[0-9a-f]{64}$`, `^sha256:[0-9a-f]{64}$`) never
// do, so this literal is a precise sentinel for the bare form alone.
const bareSHA256HexPattern = "^[0-9a-f]{64}$"

// TestBareSHA256HexRegexIsSingleSourced keeps the bare sha256-hex shape check
// single-sourced in domain/container (container.ValidSHA256Hex). It is a trust
// invariant — a mutable value must never pass where a pinned content hash is
// required — so, like digestRE/ValidDigest beside it, it lives in one place
// rather than being re-compiled in every package that records or re-validates
// ledger entries. Callers use container.ValidSHA256Hex.
func TestBareSHA256HexRegexIsSingleSourced(t *testing.T) {
	t.Parallel()

	allowed := map[string]bool{
		"internal/cli/hexdigest_guard_test.go":  true,
		"internal/domain/container/ref.go":      true,
		"internal/domain/container/ref_test.go": true,
	}

	root := repoRoot(t)

	var offenders []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if name := entry.Name(); name == ".git" || name == "dist" || name == "node_modules" {
				return filepath.SkipDir
			}

			return nil
		}

		if filepath.Ext(entry.Name()) != ".go" {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		if allowed[filepath.ToSlash(rel)] {
			return nil
		}

		content, readErr := os.ReadFile(path) //nolint:gosec // test walks repo-local files.
		if readErr != nil {
			return readErr
		}

		if strings.Contains(string(content), bareSHA256HexPattern) {
			offenders = append(offenders, filepath.ToSlash(rel))
		}

		return nil
	})
	require.NoError(t, err)

	require.Emptyf(t, offenders,
		"bare sha256-hex regex re-declared outside domain/container; call container.ValidSHA256Hex instead: %v",
		offenders)
}
