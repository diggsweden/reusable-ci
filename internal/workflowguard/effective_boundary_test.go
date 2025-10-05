// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInternalContractBoundary_RequiredAndUnknownFields(t *testing.T) {
	t.Parallel()

	callee := []byte("on:\n  workflow_call:\n    inputs:\n      project:\n        type: string\n        required: true\n    secrets:\n      KEY:\n        required: true\njobs: {}\n")
	for _, tc := range []struct {
		fields string
		count  int
	}{{"", 2}, {"    with:\n      project: x\n    secrets:\n      KEY: value\n", 0}, {"    with:\n      wrong: x\n    secrets:\n      OTHER: value\n", 4}, {"    with:\n      project: x\n    secrets: inherit\n", 0}} {
		docs := map[string][]byte{"callee.yml": callee, "caller.yml": []byte("jobs:\n  call:\n    uses: ./.github/workflows/callee.yml\n" + tc.fields)}
		failures, err := internalContractViolations(docs)
		require.NoError(t, err)
		require.Len(t, failures, tc.count)
	}
}

func TestEffectiveYAMLBoundary_ResolvesAliasesWithoutProse(t *testing.T) {
	t.Parallel()
	require.True(t, writesAttestations([]byte("base: &perms\n  attestations: write\npermissions: *perms\njobs: {}\n")))
	require.True(t, writesAttestations([]byte("base: &perms\n  attestations: write\njobs:\n  one:\n    permissions:\n      <<: *perms\n")))
	require.False(t, writesAttestations([]byte("# attestations: write\npermissions:\n  contents: read\njobs: {}\n")))
	require.True(t, branchDefaultIsMain([]byte("base: &default main\non:\n  workflow_call:\n    inputs:\n      branch:\n        default: *default\n")))
	require.False(t, branchDefaultIsMain([]byte("# default: main\non:\n  workflow_call:\n    inputs:\n      branch:\n        required: true\n")))
	require.False(t, requestTagTrigger([]byte("# release-request/v*\non: push\n"), "release-request/v*"))
	require.True(t, requestTagTrigger([]byte("on:\n  push:\n    tags: ['release-request/v*']\n"), "release-request/v*"))
}

func TestConsumerDefaultsBoundary_RecordsDefaultsAndOutputs(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "workflow.yml")
	body := "on:\n  workflow_call:\n    inputs:\n      enabled:\n        type: boolean\n        default: false\n      count:\n        type: number\n        default: 7\n    secrets:\n      KEY:\n        required: true\n    outputs:\n      result:\n        value: '${{ jobs.build.outputs.result }}'\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	contract := parseConsumerContract(t, path)
	require.Equal(t, false, contract.Inputs["enabled"].Default)
	require.Equal(t, 7, contract.Inputs["count"].Default)
	require.Equal(t, []string{"KEY"}, contract.RequiredSecrets)
	require.Equal(t, "${{ jobs.build.outputs.result }}", contract.Outputs["result"])

	changed := strings.Replace(body, "default: false", "default: true", 1)
	require.NoError(t, os.WriteFile(path, []byte(changed), 0o600))
	require.NotEqual(t, contract, parseConsumerContract(t, path))
}

func TestPreparationBoundary_CommentsCannotAuthorizeMutation(t *testing.T) {
	t.Parallel()
	body := reporoot.ReadFile(t, ".github/workflows/release-prepare-stage.yml")
	require.Empty(t, preparationViolations(body))

	for _, command := range []string{"reusable-ci version bump-plan", "reusable-ci version commit-push", "reusable-ci version tag-release"} {
		poison := strings.Replace(string(body), "run: "+command, "run: |\n          # "+command, 1)
		require.NotEmpty(t, preparationViolations([]byte(poison)))
	}
}

// The release commit stages whatever FILE_PATTERN holds. If that value stops
// being bump-plan's own output, the commit set is no longer the one the plan
// computed and preserved — and the workflow would still run green, because a
// wrong pathspec is a perfectly valid pathspec.
func TestPreparationBoundary_TheCommitStagesTheComputedPathspecs(t *testing.T) {
	t.Parallel()
	body := string(reporoot.ReadFile(t, ".github/workflows/release-prepare-stage.yml"))
	require.Empty(t, preparationViolations([]byte(body)))

	const binding = "FILE_PATTERN: ${{ steps.bump-plan.outputs.file-pattern }}"
	require.Contains(t, body, binding, "the guard below would be checking a line that no longer exists")

	for name, poison := range map[string]string{
		"a hardcoded pathspec":         "FILE_PATTERN: \".\"",
		"another step's output":        "FILE_PATTERN: ${{ steps.relctx.outputs.file-pattern }}",
		"a workflow input":             "FILE_PATTERN: ${{ inputs['file-pattern'] }}",
		"the wrong output of the step": "FILE_PATTERN: ${{ steps.bump-plan.outputs.version }}",
		"nothing at all":               "FILE_PATTERN: ''",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.NotEmpty(t, preparationViolations([]byte(strings.Replace(body, binding, poison, 1))))
		})
	}

	// Removing the id breaks the reference the commit step depends on, so the
	// guard has to notice that too rather than only checking the consumer.
	require.NotEmpty(t, preparationViolations([]byte(strings.Replace(body, "id: bump-plan\n", "", 1))))
}
