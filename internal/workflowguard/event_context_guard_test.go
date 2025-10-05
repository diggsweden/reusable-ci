// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"gopkg.in/yaml.v3"
)

// benignSecrets are the `workflow_call.secrets:` names that do not oblige a
// workflow to carry the event-context guard.
//
// Adding an entry is a reviewable act: you are asserting that this secret
// cannot be exfiltrated by a pull_request-triggered run, and recording why.
// Every declared secret not named here is treated as privileged, so a name
// nobody has classified fails the guard rather than slipping past it.
//
//nolint:gochecknoglobals // policy constant — read-only set.
var benignSecrets = map[string]string{
	"CODE_SCANNING_TOKEN": "SARIF upload from PR quality runs; those workflows are " +
		"triggered by pull_request by design, which is exactly what the guard refuses. " +
		"The token's scope is security-events:write, not contents or packages.",
	"RELEASE_GPG_PUBLIC_KEY": "public key material — published, verifiable, and useless to an attacker.",
}

// forwarderWorkflows delegate every privileged-secret-using job to
// other reusable workflows via `uses:`. They may have ancillary shell
// steps (summarize / aggregate jobs that do NOT consume privileged
// secrets), but the secret-forwarding path always lands at a guarded
// leaf. The guard belongs in those leaves, not here.
//
// Direct privileged use is forbidden by the parsed guard even on this list.
// A workflow that gains its own guarded secret-consuming job must leave the list.
//
//nolint:gochecknoglobals // policy constant — read-only set.
var forwarderWorkflows = map[string]bool{
	"release-build-stage.yml":            true,
	"release-snapshot-build-stage.yml":   true,
	"release-publish-stage.yml":          true,
	"release-snapshot-publish-stage.yml": true,
}

// TestPrivilegedWorkflowsHaveEventContextGuard is the static guard
// against a future maintainer dropping the event-context check from a
// workflow that handles signing/package/API secrets. For every
// `workflow_call:` workflow that declares any privilegedSecretNames
// in its `secrets:` block:
//
//   - it must contain at least one step whose `run:` invokes
//     `reusable-ci validate event-context` — OR
//   - it must be on the forwarderWorkflows allowlist (delegates every
//     secret-using job to a guarded leaf; the maintenance contract
//     is documented above the allowlist).
//
// Parsed steps and successful guard dependencies, rather than textual presence,
// must protect each direct privileged use. Read-only automatic tokens and the
// explicitly documented benign secrets do not trigger publishing policy.
//
//nolint:cyclop // straight-line: each branch is a distinct guard for the workflow-sweep contract.
func TestPrivilegedWorkflowsHaveEventContextGuard(t *testing.T) {
	t.Parallel()

	root := reporoot.Path(t)
	dir := filepath.Join(root, ".github", "workflows")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	type miss struct {
		file    string
		secrets []string
	}

	var missing []miss

	checked := 0

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		body, err := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // workflow files under repo root.
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}

		checked++

		if found := eventContextViolations(body, forwarderWorkflows[entry.Name()]); len(found) > 0 {
			slices.Sort(found)
			missing = append(missing, miss{file: entry.Name(), secrets: found})
		}
	}

	if checked == 0 {
		t.Fatal("event-context guard inspected no workflows")
	}

	if len(missing) > 0 {
		lines := make([]string, 0, len(missing))
		for _, m := range missing {
			lines = append(lines, "  - "+m.file+" declares "+strings.Join(m.secrets, ", "))
		}

		slices.Sort(lines)
		t.Fatalf(
			"the following workflows declare privileged secrets but do not "+
				"contain a `reusable-ci validate event-context` step:\n%s\n"+
				"Add the guard right after `Install reusable-ci binary` (plain "+
				"runners) or right after `Harden runner` (container-runtime jobs "+
				"with reusable-ci pre-baked). See publish-container.yml for the "+
				"canonical shape.",
			strings.Join(lines, "\n"),
		)
	}
}

// hasEventContextGuard reports whether any step in any job runs
// the canonical standalone guard, without conditional or error-swallowing wrappers.
func hasEventContextGuard(body []byte) bool {
	var workflow eventWorkflow
	if yaml.Unmarshal(body, &workflow) != nil {
		return false
	}

	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if canonicalEventGuard(step) {
				return true
			}
		}
	}

	return false
}

func TestSLSAAttestorGuardsBeforeSecrets(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(filepath.Join(reporoot.Path(t), ".github", "workflows", "slsa-attestor.yml")) //nolint:gosec // repository fixture.
	if err != nil {
		t.Fatal(err)
	}

	if !hasEventContextGuard(body) || len(eventContextViolations(body, false)) != 0 {
		t.Fatalf("slsa-attestor must validate event context after CLI install and before credentials/login")
	}
}
