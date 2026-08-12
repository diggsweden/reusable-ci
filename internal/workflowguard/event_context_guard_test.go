// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"os"
	"path/filepath"
	"sort"
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
// Maintenance contract: if you add a job to one of these workflows
// that consumes a privilegedSecretName via `env:` (rather than
// forwarding it to another reusable workflow), remove the entry from
// this allowlist and add the guard. The test only enforces "has the
// guard OR is on this list"; the no-direct-secret-use invariant is
// maintainer responsibility.
//
//nolint:gochecknoglobals // policy constant — read-only set.
var forwarderWorkflows = map[string]bool{
	"release-prepare-stage.yml":          true,
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
// The test does not assert step placement (any job works because
// `github.event_name` is workflow-run-scoped — refusal at any step
// fails the run before downstream jobs that `needs:` it consume
// secrets). It only asserts presence.
//
//nolint:cyclop // straight-line: each branch is a distinct guard for the workflow-sweep contract.
func TestPrivilegedWorkflowsHaveEventContextGuard(t *testing.T) {
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

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		body, err := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // workflow files under repo root.
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}

		declared := declaredCallSecrets(body)

		// Iterate what the workflow declares, not a list of what we thought was
		// privileged: an unrecognised secret must make the workflow MORE
		// interesting to this test, not invisible to it.
		var found []string

		for name := range declared {
			if _, benign := benignSecrets[name]; !benign {
				found = append(found, name)
			}
		}

		if len(found) == 0 {
			continue
		}

		if forwarderWorkflows[entry.Name()] {
			continue
		}

		if !hasEventContextGuard(body) {
			sort.Strings(found)
			missing = append(missing, miss{file: entry.Name(), secrets: found})
		}
	}

	if len(missing) > 0 {
		lines := make([]string, 0, len(missing))
		for _, m := range missing {
			lines = append(lines, "  - "+m.file+" declares "+strings.Join(m.secrets, ", "))
		}

		sort.Strings(lines)
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

// declaredCallSecrets returns the set of names under
// `on.workflow_call.secrets:`. Empty set for non-reusable workflows or
// workflows with no secrets block.
func declaredCallSecrets(body []byte) map[string]bool {
	out := map[string]bool{}

	var raw struct {
		On yaml.Node `yaml:"on"`
	}
	if err := yaml.Unmarshal(body, &raw); err != nil {
		return out
	}

	if raw.On.Kind != yaml.MappingNode {
		return out
	}

	for i := 0; i < len(raw.On.Content); i += 2 {
		if raw.On.Content[i].Value != "workflow_call" {
			continue
		}

		wc := raw.On.Content[i+1]
		if wc.Kind != yaml.MappingNode {
			return out
		}

		for j := 0; j < len(wc.Content); j += 2 {
			if wc.Content[j].Value != "secrets" {
				continue
			}

			secrets := wc.Content[j+1]
			if secrets.Kind != yaml.MappingNode {
				return out
			}

			for k := 0; k < len(secrets.Content); k += 2 {
				out[secrets.Content[k].Value] = true
			}
		}
	}

	return out
}

// hasEventContextGuard reports whether any step in any job runs
// `reusable-ci validate event-context`. A substring match on the
// raw body is sufficient — the step is unique enough that no
// other workflow content collides.
func hasEventContextGuard(body []byte) bool {
	return strings.Contains(string(body), "reusable-ci validate event-context")
}

func TestSLSAAttestorGuardsBeforeSecrets(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(reporoot.Path(t), ".github", "workflows", "slsa-attestor.yml")) //nolint:gosec // repository fixture.
	if err != nil {
		t.Fatal(err)
	}

	text := string(body)
	guard := strings.Index(text, "reusable-ci validate event-context")
	credential := strings.Index(text, "KMS_AUTH_ENV: ${{ secrets.kms-auth-env }}")

	login := strings.Index(text, "REGISTRY_PASSWORD: ${{ secrets.registry-password")
	if guard < 0 || credential < 0 || login < 0 || guard > credential || guard > login {
		t.Fatalf("slsa-attestor must validate event context after CLI install and before credentials/login")
	}
}
