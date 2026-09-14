// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// regeneratePolicyEnv, when set to 1, rewrites the privilege snapshot instead
// of comparing against it — the deliberate way to grant a new permission.
const regeneratePolicyEnv = "REGENERATE_PRIVILEGE_POLICY"

// TestJobPrivilegePolicy pins every job that holds more than read access.
//
// Rejecting write-all and reading job-over-workflow inheritance already stopped
// the blunt mistakes, but neither says which jobs may write what. Permissions
// inherit downward, so a scope added at workflow level silently reaches every
// job in the file, and a job added to a workflow that writes packages inherits
// that write without anyone deciding it should. The effective grant is what the
// runner hands the token, so that is what this pins: a per-job snapshot of the
// non-read scopes, checked in and reviewed. Adding a write becomes a diff on
// this file rather than an inherited default nobody looked at.
//
// This is an explicit least-privilege policy, not a proof of necessity: it
// records which jobs were granted what and makes a new grant visible. Whether a
// recorded grant is still needed is a review question, not one this guard can
// answer.
//
// To grant a permission deliberately, regenerate and commit both sides:
//
//	REGENERATE_PRIVILEGE_POLICY=1 go test ./internal/workflowguard -run TestJobPrivilegePolicy
func TestJobPrivilegePolicy(t *testing.T) {
	t.Parallel()

	root := reporoot.Path(t)
	files, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	policy := map[string][]string{}

	for _, path := range files {
		body, readErr := os.ReadFile(path) //nolint:gosec // test reads repo-local workflow.
		require.NoErrorf(t, readErr, "read %s", path)

		for job, scopes := range privilegedJobs(t, body) {
			policy[filepath.Base(path)+" jobs."+job] = scopes
		}
	}

	blob, err := json.MarshalIndent(policy, "", "  ")
	require.NoError(t, err)

	got := string(blob) + "\n"

	snapshot := filepath.Join("testdata", "job-privileges.json")
	if os.Getenv(regeneratePolicyEnv) == "1" {
		require.NoError(t, os.WriteFile(snapshot, []byte(got), 0o600))
		t.Log("regenerated " + snapshot)

		return
	}

	want, err := os.ReadFile(snapshot) //nolint:gosec // test reads repo-local snapshot.
	require.NoErrorf(t, err, "privilege snapshot missing; generate it deliberately with "+
		regeneratePolicyEnv+"=1 go test ./internal/workflowguard -run TestJobPrivilegePolicy")

	require.Equalf(t, string(want), got,
		"a job's effective permissions changed. Permissions inherit from the workflow, so this can happen "+
			"without touching the job at all. If the grant is deliberate and least-privilege, regenerate the "+
			"snapshot and commit it: "+regeneratePolicyEnv+"=1 go test ./internal/workflowguard -run TestJobPrivilegePolicy")
}

// privilegedJobs returns each job's effective non-read permission scopes.
// A job without its own block inherits the workflow's; one with a block
// replaces it outright, which is how the runner resolves them.
func privilegedJobs(t *testing.T, body []byte) map[string][]string {
	t.Helper()

	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal(body, &doc))

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}

	workflow := privilegedScopes(mappingChild(doc.Content[0], "permissions"))

	jobs := mappingChild(doc.Content[0], "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return nil
	}

	found := map[string][]string{}

	for i := 0; i+1 < len(jobs.Content); i += 2 {
		scopes := workflow
		if own := mappingChild(jobs.Content[i+1], "permissions"); own != nil {
			scopes = privilegedScopes(own)
		}

		if len(scopes) > 0 {
			found[jobs.Content[i].Value] = scopes
		}
	}

	return found
}

// privilegedScopes lists the scopes a permissions block grants beyond read.
// The shorthand forms carry no scope names: write-all is refused elsewhere and
// recorded here as itself so it can never look like an absence of privilege.
func privilegedScopes(node *yaml.Node) []string {
	if node == nil {
		return nil
	}

	if node.Kind == yaml.ScalarNode {
		if node.Value == "read-all" || node.Value == "{}" {
			return nil
		}

		return []string{node.Value}
	}

	if node.Kind != yaml.MappingNode {
		return nil
	}

	var scopes []string

	for i := 0; i+1 < len(node.Content); i += 2 {
		if level := node.Content[i+1].Value; level != "read" && level != "none" {
			scopes = append(scopes, node.Content[i].Value+": "+level)
		}
	}

	slices.Sort(scopes)

	return scopes
}

// TestJobPrivilegePolicyReadsInheritance proves the snapshot records the grant
// the runner actually applies: inherited when the job is silent, replaced when
// the job declares its own, and absent when everything is read-only.
func TestJobPrivilegePolicyReadsInheritance(t *testing.T) {
	t.Parallel()

	got := privilegedJobs(t, []byte(strings.Join([]string{
		"permissions:",
		"  contents: read",
		"  packages: write",
		"jobs:",
		"  inherits: {}",
		"  narrows:",
		"    permissions:",
		"      contents: read",
		"  widens:",
		"    permissions:",
		"      contents: write",
		"      id-token: write",
		"  drops:",
		"    permissions:",
		"      packages: none",
		"",
	}, "\n")))

	require.Equal(t, map[string][]string{
		"inherits": {"packages: write"},
		"widens":   {"contents: write", "id-token: write"},
	}, got, "a job that declares permissions replaces the workflow's rather than adding to them")

	require.Equal(t, []string{"write-all"}, privilegedScopes(&yaml.Node{Kind: yaml.ScalarNode, Value: "write-all"}),
		"a shorthand grant must never read as no privilege")
	require.Nil(t, privilegedScopes(&yaml.Node{Kind: yaml.ScalarNode, Value: "read-all"}))
	require.Nil(t, privilegedScopes(nil))
}
