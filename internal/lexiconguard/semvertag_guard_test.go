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

// TestStrictSemverTagRegexIsSingleSourced keeps the strict stable-release-tag
// shape single-sourced in domain/version (version.StableSemverTagRE), the way
// the commit-SHA and sha256-hex shapes are single-sourced in domain/git and
// domain/container. A re-declaration outside the one home fails the build.
//
// strictSemverTagPatterns are the two notations of the strict v-prefixed
// release-tag shape (vMAJOR.MINOR.PATCH, no pre-release/build suffix). Before
// consolidation this shape had drifted into the snapshot-version base picker,
// the runtime-image pin pattern, the sign-publish context gate, and the
// release-tag-guard default. Callers now reference version.StableSemverTagRE (or
// IsStableSemverTag), and the release-tag-guard default reads it via .String().
//
// Distinct from domain/validate.SemverTagPattern (permissive, admits a
// pre-release suffix) and from the composed patterns that merely embed the
// shape (runtimetags.pinnedRefPattern, the ledger release-tag extractor),
// none of which contain either strict-anchored literal verbatim.
func TestStrictSemverTagRegexIsSingleSourced(t *testing.T) {
	t.Parallel()

	strictSemverTagPatterns := []string{
		`^v\d+\.\d+\.\d+$`,
		`^v[0-9]+[.][0-9]+[.][0-9]+$`,
	}

	allowed := map[string]bool{
		"internal/lexiconguard/semvertag_guard_test.go":       true,
		"internal/domain/version/snapshotversion.go": true,
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

		for _, pat := range strictSemverTagPatterns {
			if strings.Contains(string(content), pat) {
				offenders = append(offenders, filepath.ToSlash(rel))

				break
			}
		}

		return nil
	})
	require.NoError(t, err)

	require.Emptyf(t, offenders,
		"strict semver-tag regex re-declared outside domain/version; use version.StableSemverTagRE / IsStableSemverTag: %v",
		offenders)
}
