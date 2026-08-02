// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// IsolationConfig parameterises the SLSA Build L3 job-isolation checks. Job
// names and the signing-secret set differ per pipeline, so they are supplied by
// the caller rather than hard-coded.
type IsolationConfig struct {
	// BuildJob is the artifact-producing job that must NOT reference any signing
	// secret. Empty disables the build-job-secrets check.
	BuildJob string
	// SigningSecrets are the secret names that must never appear inside BuildJob
	// (e.g. RELEASE_GPG_PRIVATE_KEY, COSIGN_PRIVATE_KEY).
	SigningSecrets []string
}

// IsolationViolation is one failed invariant, located for annotation against the
// source workflow.
type IsolationViolation struct {
	Line int
	Msg  string
}

// CheckIsolation returns the SLSA Build L3 isolation violations in a workflow:
//
//  1. the build job referencing a signing secret (keys must live only in the
//     separately-trusted signing job, never in the job that produces artifacts);
//  2. any actions/checkout step that does not set persist-credentials: false
//     (a persisted token in the build job widens the blast radius).
//
// It is pure — the caller supplies the raw YAML and owns IO and annotation — so
// it is exhaustively table-testable without a filesystem.
func CheckIsolation(workflowYAML []byte, cfg IsolationConfig) ([]IsolationViolation, error) {
	var doc struct {
		Jobs map[string]yaml.Node `yaml:"jobs"`
	}

	if err := yaml.Unmarshal(workflowYAML, &doc); err != nil {
		return nil, fmt.Errorf("parse workflow yaml: %w", err)
	}

	violations := buildJobSecretViolations(doc.Jobs, cfg)
	violations = append(violations, persistCredentialViolations(doc.Jobs)...)

	return violations, nil
}

// buildJobSecretViolations flags the build job referencing a signing secret —
// the artifact-producing job must have no access to signing keys (SLSA Build
// L3 isolation).
func buildJobSecretViolations(jobs map[string]yaml.Node, cfg IsolationConfig) []IsolationViolation {
	if cfg.BuildJob == "" || len(cfg.SigningSecrets) == 0 {
		return nil
	}

	node, ok := jobs[cfg.BuildJob]
	if !ok {
		return nil
	}

	line, secret, found := findSecretRef(&node, signingSecretPattern(cfg.SigningSecrets))
	if !found {
		return nil
	}

	return []IsolationViolation{{
		Line: line,
		Msg: fmt.Sprintf("build job %q references signing secret %q; the artifact-producing job must have no access to signing keys (SLSA Build L3 isolation)",
			cfg.BuildJob, secret),
	}}
}

// persistCredentialViolations flags every actions/checkout step that does not
// set persist-credentials: false.
func persistCredentialViolations(jobs map[string]yaml.Node) []IsolationViolation {
	var violations []IsolationViolation

	for _, name := range sortedKeys(jobs) {
		node := jobs[name]

		var job struct {
			Steps []yaml.Node `yaml:"steps"`
		}

		if err := node.Decode(&job); err != nil {
			continue // a reusable-call job has no steps; nothing to check
		}

		for _, stepNode := range job.Steps {
			var step struct {
				Uses string `yaml:"uses"`
				With struct {
					PersistCredentials *bool `yaml:"persist-credentials"`
				} `yaml:"with"`
			}

			if err := stepNode.Decode(&step); err != nil {
				continue
			}

			if !strings.Contains(step.Uses, "actions/checkout") {
				continue
			}

			if step.With.PersistCredentials == nil || *step.With.PersistCredentials {
				violations = append(violations, IsolationViolation{
					Line: stepNode.Line,
					Msg:  fmt.Sprintf("job %q: actions/checkout step must set persist-credentials: false (SLSA Build L3 isolation)", name),
				})
			}
		}
	}

	return violations
}

// signingSecretPattern matches `secrets.<NAME>` for any configured secret. Names
// are regexp-escaped so dots/specials in secret names can't widen the match.
func signingSecretPattern(secrets []string) *regexp.Regexp {
	escaped := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if s == "" {
			continue
		}

		escaped = append(escaped, regexp.QuoteMeta(s))
	}

	return regexp.MustCompile(`secrets\.(` + strings.Join(escaped, "|") + `)`)
}

// findSecretRef walks a job's YAML node tree for the first scalar that matches
// re, returning its source line (accurate, from the original document) and the
// captured secret name.
func findSecretRef(node *yaml.Node, re *regexp.Regexp) (int, string, bool) {
	if node.Kind == yaml.ScalarNode {
		if m := re.FindStringSubmatch(node.Value); m != nil {
			return node.Line, m[1], true
		}
	}

	for _, child := range node.Content {
		if l, s, ok := findSecretRef(child, re); ok {
			return l, s, true
		}
	}

	return 0, "", false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}
