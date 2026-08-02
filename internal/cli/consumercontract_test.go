// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// regenerateContractsEnv, when set to 1, makes
// TestConsumerContractSnapshot rewrite the snapshot instead of
// comparing against it — the deliberate way to change the contract.
const regenerateContractsEnv = "REGENERATE_CONTRACTS"

// consumerInput is the per-input slice of the consumer contract:
// whether the input is mandatory and what type it carries. Names live
// as the map keys in consumerContract.Inputs.
type consumerInput struct {
	Required bool   `json:"required"`
	Type     string `json:"type"`
}

// consumerContract is one orchestrator's public API surface: the
// `on.workflow_call` inputs (name, required, type) and secret names
// that every consuming repository's thin caller workflow codes against.
type consumerContract struct {
	Inputs  map[string]consumerInput `json:"inputs"`
	Secrets []string                 `json:"secrets"`
}

// TestConsumerContractSnapshot pins the consumer-facing API of this
// repository — the `on.workflow_call.inputs` (names + required + types)
// and secret names of every *-orchestrator.yml — to a checked-in
// snapshot. Renaming an input, flipping required, or dropping a secret
// breaks every consuming repository on their NEXT tag bump with no
// signal here; this test turns that silent break into a reviewed diff.
//
// To change the contract deliberately, regenerate the snapshot and
// commit both sides:
//
//	REGENERATE_CONTRACTS=1 go test ./internal/cli -run TestConsumerContractSnapshot
func TestConsumerContractSnapshot(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	pattern := filepath.Join(root, ".github", "workflows", "*-orchestrator.yml")
	files, err := filepath.Glob(pattern)
	require.NoError(t, err)
	require.NotEmptyf(t, files, "no consumer-facing orchestrators matched %s", pattern)

	contracts := make(map[string]consumerContract, len(files))
	for _, path := range files {
		contracts[filepath.Base(path)] = parseConsumerContract(t, path)
	}

	blob, err := json.MarshalIndent(contracts, "", "  ")
	require.NoError(t, err)

	got := string(blob) + "\n"

	snapshot := filepath.Join(root, "internal", "cli", "testdata", "consumer-contracts.json")

	if os.Getenv(regenerateContractsEnv) == "1" {
		require.NoError(t, os.MkdirAll(filepath.Dir(snapshot), 0o750))
		require.NoError(t, os.WriteFile(snapshot, []byte(got), 0o600))
		t.Logf("regenerated %s", snapshot)

		return
	}

	want, err := os.ReadFile(snapshot) //nolint:gosec // test reads repo-local snapshot.
	require.NoErrorf(t, err,
		"consumer-contract snapshot missing; generate it deliberately with "+
			regenerateContractsEnv+"=1 go test ./internal/cli -run TestConsumerContractSnapshot")

	require.Equalf(t, string(want), got,
		"the consumer contract changed: the orchestrators' on.workflow_call inputs/secrets "+
			"are the public API every consuming repository's caller workflow codes against. "+
			"If the change is deliberate, regenerate the snapshot and commit it: "+
			regenerateContractsEnv+"=1 go test ./internal/cli -run TestConsumerContractSnapshot")
}

// parseConsumerContract extracts the workflow_call inputs and secret
// names from one orchestrator file via raw node walking (key strings
// stay literal, so the `on:` key needs no YAML-1.1 bool gymnastics).
func parseConsumerContract(t *testing.T, path string) consumerContract {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // test reads repo-local workflow.
	require.NoErrorf(t, err, "read %s", path)

	var doc yaml.Node

	require.NoErrorf(t, yaml.Unmarshal(body, &doc), "parse %s", path)
	require.Truef(t, doc.Kind == yaml.DocumentNode && len(doc.Content) > 0, "%s: empty YAML document", path)

	call := mappingChild(mappingChild(doc.Content[0], "on"), "workflow_call")
	require.NotNilf(t, call, "%s: no on.workflow_call block — not a consumer-facing reusable workflow", path)

	contract := consumerContract{
		Inputs:  map[string]consumerInput{},
		Secrets: []string{},
	}

	if inputs := mappingChild(call, "inputs"); inputs != nil && inputs.Kind == yaml.MappingNode {
		for i := 0; i < len(inputs.Content); i += 2 {
			spec := inputs.Content[i+1]

			contract.Inputs[inputs.Content[i].Value] = consumerInput{
				Required: scalarChild(spec, "required") == "true",
				Type:     scalarChild(spec, "type"),
			}
		}
	}

	if secrets := mappingChild(call, "secrets"); secrets != nil && secrets.Kind == yaml.MappingNode {
		for i := 0; i < len(secrets.Content); i += 2 {
			contract.Secrets = append(contract.Secrets, secrets.Content[i].Value)
		}

		sort.Strings(contract.Secrets)
	}

	return contract
}

// mappingChild returns the value node of key inside a mapping node, or
// nil when node is nil, not a mapping, or lacks the key.
func mappingChild(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}

	return nil
}

// scalarChild returns the scalar value of key inside a mapping node,
// or "" when absent or non-scalar.
func scalarChild(node *yaml.Node, key string) string {
	child := mappingChild(node, key)
	if child == nil || child.Kind != yaml.ScalarNode {
		return ""
	}

	return child.Value
}
