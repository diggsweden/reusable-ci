// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestWorkflowInputContract proves that every `with:` key passed from
// one repo-internal workflow to another exists in the called workflow's
// `inputs:` declaration. This is the static-analysis-style guard that
// would have caught (for example) `release-publish-stage.yml` passing
// `sign-image: ...` to `publish-container.yml` before the input was
// declared, or a rename of a reusable workflow's input silently
// breaking every caller until the next production release.
//
// Scope: only internal calls (`uses: ./.github/workflows/X.yml`).
// External `uses:` (third-party actions, slsa-* refs) are out of
// scope because the test can't reach their input declarations
// without network — and `actionlint` already handles many of them.
func TestWorkflowInputContract(t *testing.T) {
	root := repoRoot(t)
	workflowsDir := filepath.Join(root, ".github", "workflows")

	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		t.Fatal(err)
	}

	reusable := make(map[string]map[string]bool) // basename → set of declared input names

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}

		body, err := os.ReadFile(filepath.Join(workflowsDir, e.Name())) //nolint:gosec // walked under .github/workflows.
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}

		inputs, ok := workflowCallInputs(body)
		if ok {
			reusable[e.Name()] = inputs
		}
	}

	// Now sweep callers and assert every `with:` key matches.
	failures := 0

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}

		body, err := os.ReadFile(filepath.Join(workflowsDir, e.Name())) //nolint:gosec // walked under .github/workflows.
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}

		for _, call := range internalCalls(body) {
			declared, found := reusable[call.target]
			if !found {
				t.Errorf("%s: calls unknown reusable workflow %q", e.Name(), call.target)

				failures++

				continue
			}

			for _, key := range call.withKeys {
				if !declared[key] {
					t.Errorf("%s → %s: passes `with: %s` but %s declares no such input",
						e.Name(), call.target, key, call.target)

					failures++
				}
			}
		}
	}

	if failures > 0 {
		t.Logf("\nfailures: %d", failures)
		t.Logf("declared inputs are in each reusable workflow's `on.workflow_call.inputs:` block")
		t.Logf("fix either by declaring the new input on the callee or removing the `with: key` on the caller")
	}
}

// workflowCallInputs returns the set of declared inputs if the YAML
// is a reusable workflow (`on.workflow_call.inputs:` present). The
// bool reports whether the workflow IS reusable.
func workflowCallInputs(body []byte) (map[string]bool, bool) {
	var raw struct {
		On yaml.Node `yaml:"on"`
	}
	if err := yaml.Unmarshal(body, &raw); err != nil {
		return nil, false
	}

	// `on:` can be a string ("push"), a list, or a map. We only
	// care about the map form with workflow_call.
	if raw.On.Kind != yaml.MappingNode {
		return nil, false
	}

	for i := 0; i < len(raw.On.Content); i += 2 {
		key := raw.On.Content[i]
		val := raw.On.Content[i+1]

		if key.Value != "workflow_call" {
			continue
		}

		// val is the workflow_call body — look for `inputs:`.
		if val.Kind != yaml.MappingNode {
			return map[string]bool{}, true // workflow_call with no inputs
		}

		for j := 0; j < len(val.Content); j += 2 {
			subKey := val.Content[j]
			subVal := val.Content[j+1]

			if subKey.Value != "inputs" {
				continue
			}

			inputs := make(map[string]bool)

			if subVal.Kind == yaml.MappingNode {
				for k := 0; k < len(subVal.Content); k += 2 {
					inputs[subVal.Content[k].Value] = true
				}
			}

			return inputs, true
		}

		return map[string]bool{}, true
	}

	return nil, false
}

// reusableCall describes one `uses: ./.github/workflows/X.yml` call
// site with the `with:` keys it passes.
type reusableCall struct {
	target   string
	withKeys []string
}

// internalCalls returns every internal reusable-workflow call (and
// its `with:` keys) found in body. External calls are skipped.
func internalCalls(body []byte) []reusableCall {
	// Walk every job's `uses:` + sibling `with:`. urfave/yaml v3
	// preserves order, so we can pair them off positionally.
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}

	var jobs *yaml.Node

	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == "jobs" {
			jobs = root.Content[i+1]

			break
		}
	}

	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return nil
	}

	var calls []reusableCall

	for i := 0; i < len(jobs.Content); i += 2 {
		job := jobs.Content[i+1]
		if job.Kind != yaml.MappingNode {
			continue
		}

		var (
			usesValue string
			withNode  *yaml.Node
		)

		for j := 0; j < len(job.Content); j += 2 {
			switch job.Content[j].Value {
			case "uses":
				if job.Content[j+1].Kind == yaml.ScalarNode {
					usesValue = job.Content[j+1].Value
				}
			case "with":
				withNode = job.Content[j+1]
			}
		}

		// Only care about repo-internal `uses:` paths.
		if !strings.HasPrefix(usesValue, "./.github/workflows/") {
			continue
		}

		target := strings.TrimPrefix(usesValue, "./.github/workflows/")

		var keys []string

		if withNode != nil && withNode.Kind == yaml.MappingNode {
			for k := 0; k < len(withNode.Content); k += 2 {
				keys = append(keys, withNode.Content[k].Value)
			}

			sort.Strings(keys)
		}

		calls = append(calls, reusableCall{target: target, withKeys: keys})
	}

	return calls
}
