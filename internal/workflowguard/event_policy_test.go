// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"regexp"
	"strings"
)

type eventWorkflow struct {
	On          yaml.Node           `yaml:"on"`
	Env         yaml.Node           `yaml:"env"`
	Permissions yaml.Node           `yaml:"permissions"`
	Jobs        map[string]eventJob `yaml:"jobs"`
}
type eventJob struct {
	Continue    yaml.Node   `yaml:"continue-on-error"`
	Env         yaml.Node   `yaml:"env"`
	Permissions yaml.Node   `yaml:"permissions"`
	Needs       yaml.Node   `yaml:"needs"`
	If          string      `yaml:"if"`
	Uses        string      `yaml:"uses"`
	With        yaml.Node   `yaml:"with"`
	Steps       []eventStep `yaml:"steps"`
}
type eventStep struct {
	Uses     string    `yaml:"uses"`
	Name     string    `yaml:"name"`
	Run      string    `yaml:"run"`
	If       string    `yaml:"if"`
	Shell    string    `yaml:"shell"`
	Continue yaml.Node `yaml:"continue-on-error"`
	Env      yaml.Node `yaml:"env"`
	With     yaml.Node `yaml:"with"`
}

func canonicalEventGuard(step eventStep) bool {
	return strings.TrimSpace(step.Run) == "reusable-ci validate event-context" && step.If == "" &&
		(step.Continue.Kind == 0 || step.Continue.Value == "false") &&
		(step.Shell == "" || step.Shell == "bash" || step.Shell == "sh")
}

func writableToken(permissions yaml.Node) bool {
	if permissions.Kind == 0 {
		return true
	} // Unspecified permissions inherit from the caller.

	if permissions.Kind == yaml.ScalarNode {
		return permissions.Value != "read-all"
	}

	for i := 0; i+1 < len(permissions.Content); i += 2 {
		if permissions.Content[i+1].Value == "write" && permissions.Content[i].Value != "security-events" && permissions.Content[i].Value != "id-token" {
			return true
		}
	}

	return false
}

func privilegedValue(node yaml.Node, writable bool) bool {
	if node.Kind == yaml.AliasNode {
		return true
	}

	for _, child := range node.Content {
		if privilegedValue(*child, writable) {
			return true
		}
	}

	if !strings.Contains(node.Value, "${{") {
		return false
	}
	// Summary presence flags reveal no credential material. Only this complete
	// boolean projection is exempt, never an expression that returns the secret.
	if regexp.MustCompile(`^\s*\$\{\{\s*secrets(?:\.[A-Za-z0-9_-]+|\[['"][^'"]+['"]\])\s*!=\s*''\s*\}\}\s*$`).MatchString(node.Value) {
		return false
	}

	if writable && regexp.MustCompile(`\bgithub\s*(?:\.\s*token|\[['"]token['"]\])`).MatchString(node.Value) {
		return true
	}

	if regexp.MustCompile(`(?:\(\s*secrets\s*\)|\{\{\s*secrets\s*\}\})`).MatchString(node.Value) {
		return true
	}

	if !regexp.MustCompile(`\bsecrets\s*[.\[]`).MatchString(node.Value) {
		return false
	}

	pattern := regexp.MustCompile(`\bsecrets(?:\.([A-Za-z0-9_-]+)|\[['"]([^'"]+)['"]\])`)

	matches := pattern.FindAllStringSubmatch(node.Value, -1)
	if len(matches) == 0 {
		return true
	}

	for _, match := range matches {
		name := match[1] + match[2]
		if name == "GITHUB_TOKEN" && !writable {
			continue
		}

		if _, benign := benignSecrets[name]; !benign {
			return true
		}
	}

	return false
}

// Only an unconditional, standalone guard establishes protection. Arbitrary
// shell programs are not accepted as evidence of a successful guard.
func eventContextViolations(body []byte, forwarder bool) []string {
	var workflow eventWorkflow
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		return []string{fmt.Sprintf("invalid workflow: %v", err)}
	}

	if mappingChild(&workflow.On, "workflow_call") == nil && !forwarder {
		return nil
	}

	var failures []string

	for name, job := range workflow.Jobs {
		permissions := job.Permissions
		if permissions.Kind == 0 {
			permissions = workflow.Permissions
		}

		writable := writableToken(permissions)
		guarded := false

		for _, dep := range eventNeeds(job.Needs) {
			if requiresSuccess(job.If) && protectedDependency(workflow.Jobs, dep, map[string]bool{}) {
				guarded = true
			}
		}

		dependencyGuarded := guarded

		direct := privilegedValue(workflow.Env, writable) || privilegedValue(job.Env, writable) || privilegedValue(job.With, writable)
		if direct && (!guarded || forwarder) {
			failures = append(failures, name+": privileged job environment/inputs precede a guard")
		}

		for index, step := range job.Steps {
			secret := privilegedValue(step.Env, writable) || privilegedValue(step.With, writable) || privilegedValue(yaml.Node{Value: step.Run}, writable)
			if secret && (forwarder || !dependencyGuarded && (!guarded || !requiresSuccess(step.If))) {
				failures = append(failures, fmt.Sprintf("%s step %d: privileged use is not guarded", name, index+1))
			}

			if canonicalEventGuard(step) {
				guarded = true
			}
		}
	}

	return failures
}

func requiresSuccess(condition string) bool {
	condition = strings.ToLower(strings.Join(strings.Fields(condition), ""))

	condition = strings.TrimSuffix(strings.TrimPrefix(condition, "${{"), "}}")
	if condition == "success()" || condition == "!failure()&&!cancelled()" {
		return true
	}

	for _, status := range []string{"success(", "always(", "failure(", "cancelled("} {
		if strings.Contains(condition, status) {
			return false
		}
	}

	return true
}

func protectedDependency(jobs map[string]eventJob, name string, seen map[string]bool) bool {
	job, ok := jobs[name]
	if !ok || seen[name] {
		return false
	}

	seen[name] = true
	defer delete(seen, name)

	if job.If != "" || job.Continue.Kind != 0 && job.Continue.Value != "false" {
		return false
	}

	for _, step := range job.Steps {
		if canonicalEventGuard(step) {
			return true
		}
	}

	for _, dep := range eventNeeds(job.Needs) {
		if protectedDependency(jobs, dep, seen) {
			return true
		}
	}

	return false
}

func eventNeeds(node yaml.Node) []string {
	if node.Kind == yaml.ScalarNode {
		return []string{node.Value}
	}

	var names []string
	for _, item := range node.Content {
		names = append(names, item.Value)
	}

	return names
}
