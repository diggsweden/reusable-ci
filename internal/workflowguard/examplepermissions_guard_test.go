// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"github.com/stretchr/testify/require"
)

func TestExampleReleaseWorkflowsDoNotGrantGitHubAttestations(t *testing.T) {
	t.Parallel()

	root := reporoot.Path(t)
	count := 0

	var violations []string

	err := filepath.WalkDir(filepath.Join(root, "examples"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() || entry.Name() != "release-workflow.yml" {
			return nil
		}

		count++

		body, readErr := os.ReadFile(path) //nolint:gosec // repository fixture.
		if readErr != nil {
			return readErr
		}

		if writesAttestations(body) {
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}

			violations = append(violations, relative)
		}

		return nil
	})
	require.NoError(t, err)
	require.NotZero(t, count, "no example release workflows found")
	require.Emptyf(t, violations,
		"example release workflows must not grant attestations:write; reusable-ci uses cosign, not the GitHub attestation API: %s",
		strings.Join(violations, ", "))
}
