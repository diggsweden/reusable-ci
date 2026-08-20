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
	// SignJob is the reusable signing workflow call-site job. Empty disables the
	// Forgejo release-signing call-site checks, keeping the generic L3 validator
	// backwards-compatible for workflows that only want the core SLSA isolation
	// checks.
	SignJob string
	// PrepareJob is the job that produces release-tag/release-sha and may run
	// fail-fast signing-secret presence checks before the consumer checkout.
	PrepareJob string
	// DistDigestOutput is the build job output carrying the build-to-sign digest.
	// Empty disables the dist-digest channel check.
	DistDigestOutput string
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
	violations = append(violations, prepareOrderingViolations(doc.Jobs, cfg)...)
	violations = append(violations, signCallSiteViolations(doc.Jobs, cfg)...)
	violations = append(violations, releaseIdentityViolations(doc.Jobs, cfg)...)
	violations = append(violations, distDigestViolations(doc.Jobs, cfg)...)
	violations = append(violations, persistCredentialViolations(doc.Jobs)...)
	violations = append(violations, setupToolchainCacheViolations(doc.Jobs)...)

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
		return []IsolationViolation{{
			Line: 1,
			Msg:  fmt.Sprintf("build job %q is missing from workflow", cfg.BuildJob),
		}}
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

func prepareOrderingViolations(jobs map[string]yaml.Node, cfg IsolationConfig) []IsolationViolation {
	if cfg.PrepareJob == "" || len(cfg.SigningSecrets) == 0 {
		return nil
	}

	node, ok := jobs[cfg.PrepareJob]
	if !ok {
		return nil
	}

	steps := jobSteps(node)
	checkoutIndex := -1

	for i, step := range steps {
		uses := scalarValue(mappingValue(&step, "uses"))
		if strings.Contains(uses, "actions/checkout@") || strings.Contains(uses, "checkout-consumer") {
			checkoutIndex = i

			break
		}
	}

	if checkoutIndex < 0 {
		return nil
	}

	var violations []IsolationViolation

	re := signingSecretPattern(cfg.SigningSecrets)
	for _, step := range steps[checkoutIndex:] {
		line, secret, found := findSecretRef(&step, re)
		if !found {
			continue
		}

		violations = append(violations, IsolationViolation{
			Line: line,
			Msg:  fmt.Sprintf("prepare job %q references signing secret %q at/after checkout; signing-secret checks must run before consumer code is checked out", cfg.PrepareJob, secret),
		})
	}

	return violations
}

func signCallSiteViolations(jobs map[string]yaml.Node, cfg IsolationConfig) []IsolationViolation {
	if cfg.SignJob == "" {
		return nil
	}

	node, ok := jobs[cfg.SignJob]
	if !ok {
		return []IsolationViolation{{Line: 1, Msg: fmt.Sprintf("sign job %q is missing from workflow", cfg.SignJob)}}
	}

	uses := scalarValue(mappingValue(&node, "uses"))
	if uses == "" || strings.HasPrefix(uses, "./") {
		return []IsolationViolation{{
			Line: node.Line,
			Msg:  fmt.Sprintf("sign job %q must be a cross-repo workflow_call invocation", cfg.SignJob),
		}}
	}

	if len(cfg.SigningSecrets) == 0 {
		return nil
	}

	secretsNode := mappingValue(&node, "secrets")

	var violations []IsolationViolation

	for _, secret := range cfg.SigningSecrets {
		if secret == "" {
			continue
		}

		if !nodeContainsSecretRef(secretsNode, secret) {
			violations = append(violations, IsolationViolation{
				Line: node.Line,
				Msg:  fmt.Sprintf("sign job %q call site is missing signing secret %s", cfg.SignJob, secret),
			})
		}
	}

	return violations
}

// releaseTagOutput and releaseSHAOutput are the prepare-job output names the
// release-identity check requires and pins the sign job's inputs to.
const (
	releaseTagOutput = "release-tag"
	releaseSHAOutput = "release-sha"
)

func releaseIdentityViolations(jobs map[string]yaml.Node, cfg IsolationConfig) []IsolationViolation {
	if cfg.SignJob == "" || cfg.PrepareJob == "" {
		return nil
	}

	prepare, ok := jobs[cfg.PrepareJob]
	if !ok {
		return nil
	}

	sign, ok := jobs[cfg.SignJob]
	if !ok {
		return nil
	}

	var violations []IsolationViolation

	// A prepare job that is a reusable-workflow call (job-level `uses:`, no
	// steps) gets its outputs from the CALLED workflow's outputs mapping,
	// which this static pass cannot see — the call target is covered by the
	// single-pin invariant instead. Only a step-based prepare job must
	// declare the release identity outputs itself; the sign-side checks
	// below apply to both shapes.
	if mappingValue(&prepare, "uses") == nil {
		prepareOutputs := mappingValue(&prepare, "outputs")
		for _, outputName := range []string{releaseTagOutput, releaseSHAOutput} {
			if mappingValue(prepareOutputs, outputName) == nil {
				violations = append(violations, IsolationViolation{
					Line: prepare.Line,
					Msg:  fmt.Sprintf("prepare job %q does not declare %s output", cfg.PrepareJob, outputName),
				})
			}
		}
	}

	signWith := mappingValue(&sign, "with")

	checks := map[string]string{releaseTagOutput: releaseTagOutput, releaseSHAOutput: releaseSHAOutput}
	for inputName, outputName := range checks {
		val := scalarValue(mappingValue(signWith, inputName))
		if !referencesNeedOutput(val, cfg.PrepareJob, outputName) {
			violations = append(violations, IsolationViolation{
				Line: sign.Line,
				Msg:  fmt.Sprintf("sign job %q must pass %s from needs.%s.outputs.%s", cfg.SignJob, inputName, cfg.PrepareJob, outputName),
			})
		}
	}

	return violations
}

func distDigestViolations(jobs map[string]yaml.Node, cfg IsolationConfig) []IsolationViolation {
	if cfg.SignJob == "" || cfg.BuildJob == "" || cfg.DistDigestOutput == "" {
		return nil
	}

	build, ok := jobs[cfg.BuildJob]
	if !ok {
		return nil
	}

	sign, ok := jobs[cfg.SignJob]
	if !ok {
		return nil
	}

	var violations []IsolationViolation
	if mappingValue(mappingValue(&build, "outputs"), cfg.DistDigestOutput) == nil {
		violations = append(violations, IsolationViolation{
			Line: build.Line,
			Msg:  fmt.Sprintf("build job %q does not declare %q output", cfg.BuildJob, cfg.DistDigestOutput),
		})
	}

	val := scalarValue(mappingValue(mappingValue(&sign, "with"), "dist-digest"))
	if !referencesNeedOutput(val, cfg.BuildJob, cfg.DistDigestOutput) {
		violations = append(violations, IsolationViolation{
			Line: sign.Line,
			Msg:  fmt.Sprintf("sign job %q must pass dist-digest from needs.%s.outputs.%s", cfg.SignJob, cfg.BuildJob, cfg.DistDigestOutput),
		})
	}

	return violations
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

			if !strings.Contains(step.Uses, "actions/checkout@") {
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

func setupToolchainCacheViolations(jobs map[string]yaml.Node) []IsolationViolation {
	var violations []IsolationViolation

	for _, name := range sortedKeys(jobs) {
		for _, step := range jobSteps(jobs[name]) {
			uses := scalarValue(mappingValue(&step, "uses"))
			if !strings.Contains(uses, "setup-toolchain@") {
				continue
			}

			cacheNode := mappingValue(mappingValue(&step, "with"), "cache")
			if scalarValue(cacheNode) != "false" {
				violations = append(violations, IsolationViolation{
					Line: step.Line,
					Msg:  fmt.Sprintf("job %q: setup-toolchain step must set cache: false in release isolation mode", name),
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

func nodeContainsSecretRef(node *yaml.Node, secret string) bool {
	if node == nil {
		return false
	}

	re := regexp.MustCompile(`secrets\.` + regexp.QuoteMeta(secret) + `\b`)
	_, _, found := findSecretRef(node, re)

	return found
}

// findSecretRef walks a job's YAML node tree for the first scalar that matches
// re, returning its source line (accurate, from the original document) and the
// captured secret name.
func findSecretRef(node *yaml.Node, re *regexp.Regexp) (int, string, bool) {
	node = resolveAlias(node)
	if node == nil {
		return 0, "", false
	}

	if node.Kind == yaml.ScalarNode {
		if m := re.FindStringSubmatch(node.Value); m != nil {
			if len(m) > 1 {
				return node.Line, m[1], true
			}

			return node.Line, m[0], true
		}
	}

	for _, child := range node.Content {
		if l, s, ok := findSecretRef(child, re); ok {
			return l, s, true
		}
	}

	return 0, "", false
}

// aliasDepthLimit bounds alias resolution. Valid YAML cannot nest
// aliases indefinitely, but a hand-built node tree could, and this walk
// runs over caller-supplied files.
const aliasDepthLimit = 100

// resolveAlias follows an alias node to the content it names.
//
// The checks in this file walk yaml.Node trees by hand, and a hand-walk
// sees an alias as a childless leaf: the anchored content is never
// visited. That made the same workflow pass or fail depending on how it
// was spelled, in the fail-open direction -- moving a build job's env
// behind an anchor hid its signing secrets from the SLSA Build L3 check.
//
// Anchors are not exotic here. This repository's own consumer-facing
// workflow_call files use them for shared `if:` conditions, so refusing
// them outright was not an option; every reader has to resolve instead.
func resolveAlias(node *yaml.Node) *yaml.Node {
	for depth := 0; node != nil && node.Kind == yaml.AliasNode; depth++ {
		if depth >= aliasDepthLimit {
			return nil
		}

		node = node.Alias
	}

	return node
}

func jobSteps(job yaml.Node) []yaml.Node {
	stepsNode := resolveAlias(mappingValue(&job, "steps"))
	if stepsNode == nil || stepsNode.Kind != yaml.SequenceNode {
		return nil
	}

	steps := make([]yaml.Node, 0, len(stepsNode.Content))

	for _, step := range stepsNode.Content {
		if resolved := resolveAlias(step); resolved != nil {
			steps = append(steps, *resolved)
		}
	}

	return steps
}

// mappingValue returns the value for key, following aliases and honouring
// the merge key (`<<`). A key written directly wins over a merged one,
// which is what the YAML merge-key spec says and what a reader expects.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	node = resolveAlias(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}

	var merged []*yaml.Node

	for idx := 0; idx+1 < len(node.Content); idx += 2 {
		if node.Content[idx].Value == key {
			return resolveAlias(node.Content[idx+1])
		}

		if node.Content[idx].Value == "<<" {
			merged = append(merged, node.Content[idx+1])
		}
	}

	// Merge sources are searched only after every direct key, so a key
	// written on the mapping wins over one pulled in by `<<`.
	return mergedValue(merged, key)
}

// mergedValue searches `<<` merge sources for key, in order. A merge
// value may be one mapping or a sequence of them.
func mergedValue(sources []*yaml.Node, key string) *yaml.Node {
	for _, source := range sources {
		source = resolveAlias(source)
		if source == nil {
			continue
		}

		if source.Kind == yaml.SequenceNode {
			for _, item := range source.Content {
				if found := mappingValue(item, key); found != nil {
					return found
				}
			}

			continue
		}

		if found := mappingValue(source, key); found != nil {
			return found
		}
	}

	return nil
}

func scalarValue(node *yaml.Node) string {
	node = resolveAlias(node)
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}

	return node.Value
}

func referencesNeedOutput(expr, job, outputName string) bool {
	dot := "needs." + job + ".outputs." + outputName
	bracketSingle := "needs." + job + ".outputs['" + outputName + "']"
	bracketDouble := "needs." + job + ".outputs[\"" + outputName + "\"]"

	return strings.Contains(expr, dot) || strings.Contains(expr, bracketSingle) || strings.Contains(expr, bracketDouble)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}
