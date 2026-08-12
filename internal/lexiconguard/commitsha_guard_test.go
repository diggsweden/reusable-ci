// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package lexiconguard

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"github.com/stretchr/testify/require"
)

// commitSHAPattern is the full-length git-commit-hash regex (40 or 64 lowercase
// hex). Before consolidation this shape had drifted into strict (`40|64`) and
// loose (`40,64`) spellings across the provenance, sign-and-publish, and
// signer-image paths.
const commitSHAPattern = "^([0-9a-f]{40}|[0-9a-f]{64})$"

// TestCommitSHARegexIsSingleSourced keeps the commit-SHA shape check
// single-sourced in domain/git (git.ValidCommitSHA), the way the bare
// sha256-hex check is single-sourced in domain/container. Callers validate a
// commit SHA through git.ValidCommitSHA rather than re-compiling the pattern.
func TestCommitSHARegexIsSingleSourced(t *testing.T) {
	t.Parallel()

	allowed := map[string]bool{
		"internal/lexiconguard/commitsha_guard_test.go": true,
		"internal/domain/git/sha.go":           true,
	}

	root := reporoot.Path(t)

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

		if strings.Contains(string(content), commitSHAPattern) {
			offenders = append(offenders, filepath.ToSlash(rel))
		}

		return nil
	})
	require.NoError(t, err)

	require.Emptyf(t, offenders,
		"commit-SHA regex re-declared outside domain/git; call git.ValidCommitSHA instead: %v",
		offenders)
}
