// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"fmt"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"gopkg.in/yaml.v3"
	"strings"
)

// Decode through values so yaml.v3 resolves aliases and merge keys; subsequent
// node walking sees effective data, never comments or alias placeholders.
func effectiveWorkflow(body []byte) (*yaml.Node, error) {
	var value map[string]any
	if err := yaml.Unmarshal(body, &value); err != nil {
		return nil, err
	}

	if value == nil {
		return nil, fmt.Errorf("workflow must be an object: %w", errs.ErrValidation)
	}

	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	return doc.Content[0], nil
}

func internalContractViolations(documents map[string][]byte) ([]string, error) {
	docs := map[string]*yaml.Node{}

	for name, body := range documents {
		doc, err := effectiveWorkflow(body)
		if err != nil {
			return nil, err
		}

		docs[name] = doc
	}

	var failures []string

	for caller, doc := range docs {
		jobs := mappingChild(doc, "jobs")
		if jobs == nil {
			continue
		}

		for i := 0; i+1 < len(jobs.Content); i += 2 {
			job := jobs.Content[i+1]

			ref := scalarChild(job, "uses")
			if !strings.HasPrefix(ref, "./.github/workflows/") {
				continue
			}

			target := strings.TrimPrefix(ref, "./.github/workflows/")

			callee, ok := docs[target]
			if !ok {
				failures = append(failures, caller+": unknown workflow "+target)

				continue
			}

			call := mappingChild(mappingChild(callee, "on"), "workflow_call")
			if call == nil {
				failures = append(failures, caller+": target is not reusable "+target)

				continue
			}

			for _, section := range []struct{ declared, supplied string }{{"inputs", "with"}, {"secrets", "secrets"}} {
				declared, supplied := mappingChild(call, section.declared), mappingChild(job, section.supplied)
				if section.declared == "secrets" && supplied != nil && supplied.Value == "inherit" {
					continue
				}

				for _, failure := range callFieldViolations(declared, supplied) {
					failures = append(failures, fmt.Sprintf("%s -> %s %s: %s", caller, target, section.declared, failure))
				}
			}
		}
	}

	return failures, nil
}

func callFieldViolations(declared, supplied *yaml.Node) []string {
	var failures []string

	if supplied != nil {
		if supplied.Kind != yaml.MappingNode {
			return []string{"arguments must be a mapping"}
		}

		for j := 0; j+1 < len(supplied.Content); j += 2 {
			key := supplied.Content[j].Value
			if mappingChild(declared, key) == nil {
				failures = append(failures, "unknown field "+key)
			}
		}
	}

	if declared != nil {
		for j := 0; j+1 < len(declared.Content); j += 2 {
			key, spec := declared.Content[j].Value, declared.Content[j+1]
			if scalarChild(spec, "required") == "true" && mappingChild(spec, "default") == nil && mappingChild(supplied, key) == nil {
				failures = append(failures, "missing required field "+key)
			}
		}
	}

	return failures
}

func requestTagTrigger(body []byte, pattern string) bool {
	doc, err := effectiveWorkflow(body)
	if err != nil {
		return false
	}

	tags := mappingChild(mappingChild(mappingChild(doc, "on"), "push"), "tags")
	if tags == nil || tags.Kind != yaml.SequenceNode || len(tags.Content) != 1 {
		return false
	}

	return tags.Content[0].Value == pattern
}

func branchDefaultIsMain(body []byte) bool {
	doc, err := effectiveWorkflow(body)
	if err != nil {
		return true
	}

	inputs := mappingChild(mappingChild(mappingChild(doc, "on"), "workflow_call"), "inputs")
	if inputs == nil {
		return false
	}

	for i := 1; i < len(inputs.Content); i += 2 {
		if scalarChild(inputs.Content[i], "default") == "main" {
			return true
		}
	}

	return false
}

func writesAttestations(body []byte) bool {
	doc, err := effectiveWorkflow(body)
	if err != nil {
		return true
	}

	permission := mappingChild(doc, "permissions")
	if permission != nil && (permission.Value == "write-all" || scalarChild(permission, "attestations") == "write") {
		return true
	}

	jobs := mappingChild(doc, "jobs")
	if jobs == nil {
		return false
	}

	for i := 1; i < len(jobs.Content); i += 2 {
		permission := mappingChild(jobs.Content[i], "permissions")
		if permission != nil && (permission.Value == "write-all" || scalarChild(permission, "attestations") == "write") {
			return true
		}
	}

	return false
}
