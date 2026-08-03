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

// britishArtifact is the British spelling the codebase standardized away from.
// The domain, identifiers, CLI usage strings, and prose all use the American
// "artifact"; the wire contract (artifacts.yml, --artifact, JSON keys) was
// already American, so this guard keeps the Go source from drifting back to a
// mixed spelling.
const britishArtifact = "artefact"

// TestArtifactSpellingIsAmerican fails if any Go source reintroduces the
// British "artefact"/"Artefact" spelling, the way the commit-SHA and
// retry-backoff guards lock their single source.
func TestArtifactSpellingIsAmerican(t *testing.T) {
	t.Parallel()

	allowed := map[string]bool{
		"internal/cli/artifactspelling_guard_test.go": true,
		// contractresidue.go's ARTEFACT_NAME is the *legacy* env-var name its
		// residue detector must keep verbatim to catch workflows still using
		// the old British spelling — the one place the word legitimately stays.
		"internal/app/validate/contractresidue.go": true,
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

		if strings.Contains(strings.ToLower(string(content)), britishArtifact) {
			offenders = append(offenders, filepath.ToSlash(rel))
		}

		return nil
	})
	require.NoError(t, err)

	require.Emptyf(t, offenders,
		"British \"artefact\" spelling found; use \"artifact\": %v", offenders)
}
