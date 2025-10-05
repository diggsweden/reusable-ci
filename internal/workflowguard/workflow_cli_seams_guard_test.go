// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestReleaseOrchestratorResolvesReusableCIRefFromCanonicalRemote(t *testing.T) {
	t.Parallel()

	env := workflowStepEnv(t, "release-orchestrator.yml", "parse-config", "Resolve reusable-ci ref to commit SHA")
	require.Equal(t, "https://github.com/diggsweden/reusable-ci", env["REMOTE_URL"])
	body, err := os.ReadFile(filepath.Join(reporoot.Path(t), ".github", "workflows", "release-orchestrator.yml"))
	require.NoError(t, err)
	require.True(t, canonicalResolveStep(body))
}

func canonicalResolveStep(body []byte) bool {
	var workflow eventWorkflow
	if yaml.Unmarshal(body, &workflow) != nil {
		return false
	}

	job := workflow.Jobs["parse-config"]
	for _, step := range job.Steps {
		if step.Name != "Resolve reusable-ci ref to commit SHA" {
			continue
		}

		for _, env := range []*yaml.Node{&workflow.Env, &job.Env, &step.Env} {
			for _, key := range []string{"BASH_ENV", "ENV"} {
				if value := mappingChild(env, key); value != nil && value.Value != "" {
					return false
				}
			}
		}

		remote := mappingChild(&step.Env, "REMOTE_URL")

		return remote != nil && remote.Value == "https://github.com/diggsweden/reusable-ci" && strings.TrimSpace(step.Run) == "reusable-ci platform resolve-ref" && step.Shell == ""
	}

	return false
}

func TestPublishContainerValidatesNamespaceForSelectedRegistry(t *testing.T) {
	t.Parallel()

	env := workflowStepEnv(t, "publish-container.yml", "prep", "Validate Image Namespace")
	require.Equal(t, "${{ inputs.registry }}", env["CONTAINER_REGISTRY"])
	require.Equal(t, "${{ inputs.registry }}", env["ENFORCE_NAMESPACE_ON"])
}

func workflowStepEnv(t *testing.T, workflowName, jobName, stepName string) map[string]string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(reporoot.Path(t), ".github", "workflows", workflowName)) //nolint:gosec // repository contract fixture.
	require.NoError(t, err)

	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string            `yaml:"name"`
				Env  map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(body, &workflow))

	job, ok := workflow.Jobs[jobName]
	require.Truef(t, ok, "%s has no job %q", workflowName, jobName)

	for _, step := range job.Steps {
		if step.Name == stepName {
			return step.Env
		}
	}

	require.FailNowf(t, "workflow step not found", "%s job %q has no step %q", workflowName, jobName, stepName)

	return nil
}
