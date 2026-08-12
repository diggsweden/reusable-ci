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

// ociTagComponentFragment is the unanchored OCI tag-component charset — the
// value after `:` in a reference, and the rule for a promotion stage name.
// Unlike the anchored digest/hex sentinels, this fragment is DESIGNED to be
// embedded in larger patterns, so it is single-sourced as
// container.OCITagComponent and callers splice that constant in. A verbatim
// re-spelling of the class outside its one home is what this guard forbids.
const ociTagComponentFragment = "[A-Za-z0-9_][A-Za-z0-9._-]{0,127}"

// TestOCITagComponentFragmentIsSingleSourced keeps the tag-component charset
// single-sourced in domain/container (container.OCITagComponent /
// ValidOCITagComponent), the way the commit-SHA, sha256-hex and sha256-digest
// shapes are. The ledger's composed ref patterns (imageRefRE/tagRefRE) and the
// stage-name validator embed the constant, so the literal must not reappear.
func TestOCITagComponentFragmentIsSingleSourced(t *testing.T) {
	t.Parallel()

	allowed := map[string]bool{
		"internal/lexiconguard/ocitagcomponent_guard_test.go": true,
		"internal/domain/container/ref.go":           true,
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

		if strings.Contains(string(content), ociTagComponentFragment) {
			offenders = append(offenders, filepath.ToSlash(rel))
		}

		return nil
	})
	require.NoError(t, err)

	require.Emptyf(t, offenders,
		"OCI tag-component charset re-declared outside domain/container; embed container.OCITagComponent (or call ValidOCITagComponent) instead: %v",
		offenders)
}
