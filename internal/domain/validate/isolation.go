// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"gopkg.in/yaml.v3"
)

// IsolationConfig parameterises the static release-isolation checks. Job
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
	// Forgejo release-signing call-site checks, keeping the generic validator
	// backwards-compatible for workflows that only want the core release-isolation
	// checks.
	SignJob string
	// PrepareJob is the job that produces release-tag/release-sha and may run
	// fail-fast signing-secret presence checks before the consumer checkout.
	PrepareJob string
	// DistDigestOutput is the build job output carrying the build-to-sign digest.
	// Empty disables the dist-digest channel check.
	DistDigestOutput string
}

// IsolationViolation is one failed static invariant. Secret diagnostics identify
// the consuming job, secret and start of the containing source scalar (the anchor
// definition for an alias), not an exact expression-token location.
type IsolationViolation struct {
	Line int
	Msg  string
}

// CheckIsolation returns the release-isolation violations in a workflow:
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
		Env  yaml.Node            `yaml:"env"`
	}

	if err := yaml.Unmarshal(workflowYAML, &doc); err != nil {
		return nil, fmt.Errorf("parse workflow yaml: %w", err)
	}

	violations, err := buildJobSecretViolations(doc.Jobs, cfg)
	if err != nil {
		return nil, err
	}

	inheritedViolations, err := workflowEnvSecretViolations(doc.Env, doc.Jobs, cfg)
	if err != nil {
		return nil, err
	}

	violations = append(violations, inheritedViolations...)

	prepareViolations, err := prepareOrderingViolations(doc.Jobs, cfg)
	if err != nil {
		return nil, err
	}

	violations = append(violations, prepareViolations...)
	violations = append(violations, signCallSiteViolations(doc.Jobs, cfg)...)
	violations = append(violations, releaseIdentityViolations(doc.Jobs, cfg)...)
	violations = append(violations, distDigestViolations(doc.Jobs, cfg)...)
	violations = append(violations, persistCredentialViolations(doc.Jobs)...)
	violations = append(violations, setupToolchainCacheViolations(doc.Jobs)...)

	return violations, nil
}

func workflowEnvSecretViolations(env yaml.Node, jobs map[string]yaml.Node, cfg IsolationConfig) ([]IsolationViolation, error) {
	job, exists := jobs[cfg.BuildJob]
	if !exists || env.Kind == 0 {
		return nil, nil
	}

	var inherited, overrides map[string]yaml.Node
	if err := env.Decode(&inherited); err != nil {
		return nil, fmt.Errorf("decode workflow env: %w", err)
	}

	if jobEnv := mappingValue(&job, "env"); jobEnv != nil {
		if err := jobEnv.Decode(&overrides); err != nil {
			return nil, fmt.Errorf("decode build job env: %w", err)
		}
	}

	var violations []IsolationViolation

	for _, key := range sortedKeys(inherited) {
		if _, overridden := overrides[key]; overridden {
			continue
		}

		value := inherited[key]

		refs, err := findSecretRefs(&value, signingSecretPattern(cfg.SigningSecrets))
		if err != nil {
			return nil, fmt.Errorf("inspect workflow env %q: %w", key, err)
		}

		for _, ref := range refs {
			violations = append(violations, IsolationViolation{Line: ref.line, Msg: fmt.Sprintf("build job %q inherits signing secret %s through workflow env %q", cfg.BuildJob, ref.describe(), key)})
		}
	}

	return violations, nil
}

// buildJobSecretViolations flags the build job referencing a signing secret:
// the artifact-producing job must have no access to signing keys.
func buildJobSecretViolations(jobs map[string]yaml.Node, cfg IsolationConfig) ([]IsolationViolation, error) {
	if cfg.BuildJob == "" || len(cfg.SigningSecrets) == 0 {
		return nil, nil
	}

	node, ok := jobs[cfg.BuildJob]
	if !ok {
		return []IsolationViolation{{
			Line: 1,
			Msg:  fmt.Sprintf("build job %q is missing from workflow", cfg.BuildJob),
		}}, nil
	}

	refs, err := findSecretRefs(&node, signingSecretPattern(cfg.SigningSecrets))
	if err != nil {
		return nil, fmt.Errorf("inspect build job %q: %w", cfg.BuildJob, err)
	}

	var violations []IsolationViolation
	for _, ref := range refs {
		violations = append(violations, IsolationViolation{
			Line: ref.line,
			Msg: fmt.Sprintf("build job %q references signing secret %s; the artifact-producing job must have no access to signing keys (release isolation)",
				cfg.BuildJob, ref.describe()),
		})
	}

	// A reusable-workflow build job that inherits the caller's secrets holds
	// every signing secret without naming one.
	if scalarValue(mappingValue(&node, "secrets")) == "inherit" {
		violations = append(violations, IsolationViolation{
			Line: node.Line,
			Msg:  fmt.Sprintf("build job %q inherits every caller secret (secrets: inherit); pass the build job only the secrets it needs (release isolation)", cfg.BuildJob),
		})
	}

	return violations, nil
}

func prepareOrderingViolations(jobs map[string]yaml.Node, cfg IsolationConfig) ([]IsolationViolation, error) { //nolint:cyclop // Detect checkout, then attribute each secret-bearing scalar without losing traversal errors.
	if cfg.PrepareJob == "" || len(cfg.SigningSecrets) == 0 {
		return nil, nil
	}

	node, ok := jobs[cfg.PrepareJob]
	if !ok {
		return nil, nil
	}

	steps := jobSteps(node)
	checkoutIndex := -1

	for i, step := range steps {
		uses := scalarValue(mappingValue(&step, "uses"))
		if usesAction(uses, "actions/checkout@") || usesAction(uses, "checkout-consumer") {
			checkoutIndex = i

			break
		}
	}

	if checkoutIndex < 0 {
		return nil, nil
	}

	var violations []IsolationViolation

	re := signingSecretPattern(cfg.SigningSecrets)
	for _, step := range steps[checkoutIndex:] {
		refs, err := findSecretRefs(&step, re)
		if err != nil {
			return nil, fmt.Errorf("inspect prepare job %q: %w", cfg.PrepareJob, err)
		}

		for _, ref := range refs {
			violations = append(violations, IsolationViolation{
				Line: ref.line,
				Msg:  fmt.Sprintf("prepare job %q references signing secret %s at/after checkout; signing-secret checks must run before consumer code is checked out", cfg.PrepareJob, ref.describe()),
			})
		}
	}

	return violations, nil
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
// set persist-credentials to the literal false (boolean or string). Identifying
// checkout must not depend on successfully decoding its credential input.
func persistCredentialViolations(jobs map[string]yaml.Node) []IsolationViolation {
	var violations []IsolationViolation

	for _, name := range sortedKeys(jobs) {
		for _, step := range jobSteps(jobs[name]) {
			uses := scalarValue(mappingValue(&step, "uses"))
			if !usesAction(uses, "actions/checkout@") {
				continue
			}

			persist := mappingValue(mappingValue(&step, "with"), "persist-credentials")
			if scalarValue(persist) != "false" || (persist.ShortTag() != "!!bool" && persist.ShortTag() != "!!str") {
				violations = append(violations, IsolationViolation{
					Line: step.Line,
					Msg:  fmt.Sprintf("job %q: actions/checkout step must set persist-credentials: false (static literal required for release isolation)", name),
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
			if !usesAction(uses, "setup-toolchain@") {
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

// usesAction reports whether a step's `uses:` names the action. Forges resolve
// repository names case-insensitively, so `Actions/Checkout@` is the same
// action as `actions/checkout@` and must not slip past a check by spelling.
func usesAction(uses, action string) bool {
	return strings.Contains(strings.ToLower(uses), action)
}

// signingSecretPattern indexes exact configured names for literal member matching.
func signingSecretPattern(secrets []string) map[string]bool {
	names := make(map[string]bool, len(secrets))
	for _, secret := range secrets {
		if secret != "" {
			names[secret] = true
		}
	}

	return names
}

func nodeContainsSecretRef(node *yaml.Node, secret string) bool {
	if node == nil {
		return false
	}

	re := signingSecretPattern([]string{secret})
	refs, err := findSecretRefs(node, re)

	return err == nil && len(refs) > 0
}

type signingSecretRef struct {
	line   int
	secret string // empty when the whole secrets context is referenced
}

// describe names the reference in a diagnostic: the secret, or the construct
// that reaches every secret at once.
func (r signingSecretRef) describe() string {
	if r.secret == "" {
		return "through the whole secrets context (toJSON(secrets) or a computed index)"
	}

	return fmt.Sprintf("%q", r.secret)
}

// findSecretRefs reports distinct literal secret names per scalar, not token
// occurrences. yaml.v3 resolves effective mapping values before traversal, so
// direct keys and earlier merge sources shadow defaults just as in env decoding.
func findSecretRefs(node *yaml.Node, names map[string]bool) ([]signingSecretRef, error) { //nolint:cyclop,gocognit // Keep bounded traversal, cycle state and effective-value diagnostics in one walk.
	var refs []signingSecretRef

	active, seen := map[*yaml.Node]bool{}, map[*yaml.Node]bool{}

	var walk func(*yaml.Node, int) error

	walk = func(node *yaml.Node, depth int) error {
		if node == nil {
			return nil
		}

		if depth >= aliasDepthLimit || (node.Kind == yaml.AliasNode && resolveAlias(node) == nil) {
			return fmt.Errorf("workflow alias/nesting limit %d exceeded at line %d: %w", aliasDepthLimit, node.Line, errs.ErrMalformedInput)
		}

		node = resolveAlias(node)
		if active[node] {
			return fmt.Errorf("cyclic workflow alias at line %d: %w", node.Line, errs.ErrMalformedInput)
		}

		if seen[node] {
			return nil
		}

		active[node], seen[node] = true, true
		defer delete(active, node)

		if node.Kind == yaml.ScalarNode {
			matched := map[string]bool{}

			for _, ref := range expressionReferences(node.Value, "secrets", false) {
				// toJSON(secrets), secrets[format(...)] and a bare `secrets`
				// reach every secret without naming one. They are a
				// reference to each configured secret, reported once.
				secret := ""
				if len(ref.members) > 0 {
					secret = ref.members[0]
				}

				if (secret == "" || names[secret]) && !matched[secret] {
					refs = append(refs, signingSecretRef{line: node.Line, secret: secret})
					matched[secret] = true
				}
			}

			return nil
		}

		children := node.Content
		if node.Kind == yaml.MappingNode {
			var values map[string]yaml.Node
			if err := node.Decode(&values); err != nil {
				return fmt.Errorf("decode workflow mapping at line %d: %w", node.Line, err)
			}

			children = make([]*yaml.Node, 0, len(values))
			for _, key := range sortedKeys(values) {
				value := values[key]
				children = append(children, &value)
			}
		}

		for _, child := range children {
			if err := walk(child, depth+1); err != nil {
				return err
			}
		}

		return nil
	}
	if err := walk(node, 0); err != nil {
		return nil, err
	}

	return refs, nil
}

// aliasDepthLimit bounds alias chains and secret-walk nesting. Recursive aliases
// and excessive nesting must not turn a partial inspection into a passing check.
const aliasDepthLimit = 100

// resolveAlias follows an alias node to the content it names.
//
// The checks in this file walk yaml.Node trees by hand, and a hand-walk
// sees an alias as a childless leaf: the anchored content is never
// visited. That made the same workflow pass or fail depending on how it
// was spelled, in the fail-open direction -- moving a build job's env
// behind an anchor hid its signing secrets from the release-isolation check.
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

// mappingValue uses the same YAML merge precedence as the secret walk. Decoding
// also bounds recursive merge aliases rather than recursively hand-walking them.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	node = resolveAlias(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}

	var values map[string]yaml.Node
	if err := node.Decode(&values); err != nil {
		return nil
	}

	value, ok := values[key]
	if !ok {
		return nil
	}

	return resolveAlias(&value)
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
