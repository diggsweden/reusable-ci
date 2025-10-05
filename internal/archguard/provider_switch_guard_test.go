// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package archguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"github.com/stretchr/testify/require"
)

func TestDomainPlatformBranchingIsConfinedToPresentation(t *testing.T) {
	t.Parallel()

	// repo-relative path -> why the forge-specific branching is acceptable here.
	allowed := map[string]string{
		"internal/domain/security/opengrep.go": "code-scanning summary label/note differ per forge surface",
		"internal/domain/summary/urls.go":      "run/commit URL shapes differ per forge",
	}

	branches := []string{
		"case provider.ForgeGitHub",
		"case provider.ForgeGitLab",
		"case provider.ForgeForgejo",
		"== provider.ForgeGitHub",
		"== provider.ForgeGitLab",
		"== provider.ForgeForgejo",
	}

	domainDir := filepath.Join(reporoot.Path(t), "internal", "domain")

	var offenders []string

	err := filepath.WalkDir(domainDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !isProductGoFile(d, path) {
			return nil
		}

		body, readErr := os.ReadFile(path) //nolint:gosec // test reads repo-local source files.
		if readErr != nil {
			return readErr
		}

		rel, _ := filepath.Rel(reporoot.Path(t), path)
		if _, ok := allowed[filepath.ToSlash(rel)]; ok {
			return nil
		}

		src := string(body)
		for _, needle := range branches {
			if strings.Contains(src, needle) {
				offenders = append(offenders, filepath.ToSlash(rel)+": "+needle)
			}
		}

		return nil
	})
	require.NoError(t, err)

	require.Emptyf(t, offenders,
		"internal/domain may branch on forge identity ONLY in the sanctioned "+
			"presentation modules (see the `allowed` map). Either refactor this "+
			"behind a provider role or add the file with a justification. Offenders:\n%s",
		strings.Join(offenders, "\n"))
}
