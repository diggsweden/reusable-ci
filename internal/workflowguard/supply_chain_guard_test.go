// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func supplyChainViolations(body []byte) []string {
	var workflow eventWorkflow
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		return []string{err.Error()}
	}

	var failures []string
	if workflow.Permissions.Value == "write-all" {
		failures = append(failures, "workflow write-all permissions are forbidden")
	}

	for _, job := range workflow.Jobs {
		if job.Permissions.Value == "write-all" {
			failures = append(failures, "job write-all permissions are forbidden")
		}

		refs := []string{job.Uses}
		for _, step := range job.Steps {
			refs = append(refs, step.Uses)
		}

		for _, ref := range refs {
			if ref == "" || strings.HasPrefix(ref, "./") {
				continue
			}

			if strings.HasPrefix(ref, "docker://") {
				if !domaincontainer.ValidDigestReference(strings.TrimPrefix(ref, "docker://")) {
					failures = append(failures, "unpinned container action: "+ref)
				}

				continue
			}

			_, revision, ok := strings.Cut(ref, "@")
			if !ok || !domaingit.ValidCommitSHA(revision) {
				failures = append(failures, "unpinned external uses: "+ref)
			}
		}
	}

	return failures
}

func TestExternalWorkflowReferencesAreImmutable(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob(filepath.Join(reporoot.Path(t), ".github", "workflows", "*.yml"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)

	for _, path := range paths {
		body, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Empty(t, supplyChainViolations(body), path)
	}
}
