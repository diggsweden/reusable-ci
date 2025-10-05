// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

func yamlMappingScalarValues(data []byte, key string) ([]string, error) {
	doc, err := parseYAMLNode(data)
	if err != nil {
		return nil, err
	}

	var values []string

	walkYAML(doc, map[*yaml.Node]bool{}, func(node *yaml.Node) {
		if node.Kind != yaml.MappingNode {
			return
		}

		// A YAML mapping node stores its children flat as key, value, key,
		// value, so the cursor advances two at a time.
		for pair := 0; pair+1 < len(node.Content); pair += 2 {
			mappingKey, ok := resolvedScalar(node.Content[pair])
			if !ok || mappingKey != key {
				continue
			}

			if value, ok := resolvedScalar(node.Content[pair+1]); ok {
				values = append(values, value)
			}
		}
	})

	return values, nil
}

func yamlScalarValues(data []byte) ([]string, error) {
	doc, err := parseYAMLNode(data)
	if err != nil {
		return nil, err
	}

	var values []string

	walkYAML(doc, map[*yaml.Node]bool{}, func(node *yaml.Node) {
		if node.Kind == yaml.ScalarNode {
			values = append(values, node.Value)
		}
	})

	return values, nil
}

func parseYAMLNode(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse workflow YAML: %w", err)
	}

	return &doc, nil
}

func walkYAML(node *yaml.Node, seen map[*yaml.Node]bool, visit func(*yaml.Node)) {
	if node == nil || seen[node] {
		return
	}

	seen[node] = true

	visit(node)

	if node.Kind == yaml.AliasNode {
		walkYAML(node.Alias, seen, visit)

		return
	}

	for _, child := range node.Content {
		walkYAML(child, seen, visit)
	}
}

func resolvedScalar(node *yaml.Node) (string, bool) {
	seen := map[*yaml.Node]bool{}
	for node != nil && node.Kind == yaml.AliasNode && !seen[node] {
		seen[node] = true
		node = node.Alias
	}

	if node == nil || node.Kind != yaml.ScalarNode {
		return "", false
	}

	return node.Value, true
}

// pinnedSubjectSHA returns the 40-hex commit a uses: value pins subject to,
// or "" when the value does not reference subject as whole path components.
//
// A bare substring match also accepted a sibling repository whose name merely
// starts with the subject ("org/ci" inside "org/ci-extras/…"), so that
// repository's pin was collected as if it were the subject's: pin-reachability
// then reported it orphaned in the wrong clone, and the single-pin check
// reported "mixed pins" for a directory that pinned each repository once.
func pinnedSubjectSHA(uses, subject string) string {
	if subject == "" {
		return ""
	}

	for start := 0; start < len(uses); {
		idx := strings.Index(uses[start:], subject)
		if idx < 0 {
			return ""
		}

		idx += start
		end := idx + len(subject)

		if !subjectIsPathComponent(uses, idx, end) {
			start = idx + 1

			continue
		}

		at := strings.LastIndexByte(uses[end:], '@')
		if at < 0 {
			return ""
		}

		if ref := uses[end+at+1:]; isSHA1(ref) {
			return ref
		}

		return ""
	}

	return ""
}

// subjectIsPathComponent reports whether uses[idx:end] is bounded by path
// separators: it starts the value or follows a "/", and a "/" or the "@ref"
// separator follows it.
func subjectIsPathComponent(uses string, idx, end int) bool {
	if idx > 0 && uses[idx-1] != '/' {
		return false
	}

	return end < len(uses) && (uses[end] == '/' || uses[end] == '@')
}

func isSHA1(value string) bool {
	if len(value) != 40 {
		return false
	}

	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}

	return true
}
