// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"gopkg.in/yaml.v3"

	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
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
	root := reporoot.Path(t)
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

func TestWorkflowAttestationTypesUseAcceptedCLIVocabulary(t *testing.T) {
	workflowsDir := filepath.Join(reporoot.Path(t), ".github", "workflows")
	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		t.Fatal(err)
	}

	tokenPattern := regexp.MustCompile(`\bslsaprovenance[0-9]*\b`)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(workflowsDir, entry.Name())) //nolint:gosec // repository fixture.
		if err != nil {
			t.Fatal(err)
		}
		for _, token := range tokenPattern.FindAllString(string(body), -1) {
			if token != domaincontainer.PredicateTypeSLSAProvenance1 {
				t.Errorf("%s uses unsupported attestation type %q; CLI accepts %q", entry.Name(), token, domaincontainer.PredicateTypeSLSAProvenance1)
			}
		}
	}
}

func TestPromoteWorkflowResolvesDryRunAsBoolean(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(reporoot.Path(t), ".github", "workflows", "promote-stage.yml")) //nolint:gosec // repository fixture.
	if err != nil {
		t.Fatal(err)
	}

	text := string(body)
	if strings.Contains(text, `${DRY_RUN:+--dry-run}`) {
		t.Fatal("non-empty string expansion would add --dry-run when DRY_RUN=false")
	}
	if !strings.Contains(text, `[[ "$DRY_RUN" == true ]] && args+=(--dry-run)`) {
		t.Fatal("promote must add --dry-run only for the resolved true boolean")
	}
	if got := strings.Count(text, `[[ "$DRY_RUN" == true ]] && exit 0`); got != 2 {
		t.Fatalf("cleanup and rollback dry-run guards = %d, want 2", got)
	}
}

func TestProductionReleaseCeremonyUsesRequestThenFinalTag(t *testing.T) {
	root := reporoot.Path(t)

	// Walked rather than globbed at a fixed depth: examples/ is grouped
	// (examples/signing/…, examples/gitlab/…), and an example that quietly
	// dropped out of this check by moving one level down would be the exact
	// failure this guard exists to prevent. A new example is picked up with no
	// counter to bump.
	var examples []string

	err := filepath.WalkDir(filepath.Join(root, "examples"), func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if !d.IsDir() && d.Name() == "release-workflow.yml" {
			examples = append(examples, path)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(examples) == 0 {
		t.Fatal("no examples/**/release-workflow.yml found; the ceremony check would pass vacuously")
	}

	paths := make([]string, 1, 1+len(examples))
	paths[0] = filepath.Join(root, ".github", "workflows", "self-release.yml")
	paths = append(paths, examples...)
	for _, path := range paths {
		body, readErr := os.ReadFile(path) //nolint:gosec // repository fixture.
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !strings.Contains(string(body), `"release-request/v*"`) {
			t.Errorf("%s does not trigger on release-request/v*", path)
		}
	}

	orchestrator, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release-orchestrator.yml")) //nolint:gosec // repository fixture.
	if err != nil {
		t.Fatal(err)
	}
	text := string(orchestrator)
	derive := strings.Index(text, "run: reusable-ci version derive-release")
	requireRequest := strings.Index(text, `RELEASE_CONTEXT_REQUIRE_REQUEST: "true"`)
	if derive < 0 || requireRequest < 0 || requireRequest > derive {
		t.Fatal("orchestrator must require a request ref at its first derive-release boundary")
	}
	if got := strings.Count(text, `branch: ${{ needs.parse-config.outputs.release-tag }}`); got != 2 {
		t.Fatalf("post-tag build/publish final-tag checkouts = %d, want 2", got)
	}
	if !strings.Contains(text, `checkout-ref: ${{ needs.parse-config.outputs.release-tag }}`) {
		t.Fatal("release creation must check out the created final tag")
	}

	runtimeWorkflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "self-runtime-container.yml")) //nolint:gosec // repository fixture.
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(runtimeWorkflow), `- "v*.*.*"`) {
		t.Fatal("self-runtime-container must remain triggered by final release tags")
	}
}

func TestReleasePreparationSerializesBumpsAndTagsOnce(t *testing.T) {
	root := reporoot.Path(t)
	body, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release-prepare-stage.yml")) //nolint:gosec // repository fixture.
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "max-parallel: 1") {
		t.Fatal("version-bump matrix must remain serialized")
	}
	if got := strings.Count(text, "run: reusable-ci version tag-release"); got != 1 {
		t.Fatalf("final tag creation steps = %d, want exactly 1", got)
	}
	if !strings.Contains(text, "needs: [version-bump]") {
		t.Fatal("final tag job must wait for every version bump")
	}

	for _, relative := range []string{"docs/artifacts-reference.md", "examples/monorepo/README.md"} {
		doc, readErr := os.ReadFile(filepath.Join(root, relative)) //nolint:gosec // repository fixture.
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(doc), "can race on pushes/tag moves") {
			t.Errorf("%s still claims multi-artifact release refs race", relative)
		}
	}
}

func TestRuntimeContainerfileExternalFromImagesAreDigestPinned(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(reporoot.Path(t), "containers", "runtime", "Containerfile")) //nolint:gosec // repository fixture.
	if err != nil {
		t.Fatal(err)
	}

	args := make(map[string]string)
	stages := make(map[string]bool)
	argPattern := regexp.MustCompile(`^ARG ([A-Za-z_][A-Za-z0-9_]*)=(\S+)$`)
	variablePattern := regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)
	digestPattern := regexp.MustCompile(`@sha256:[a-f0-9]{64}$`)
	for lineNumber, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if match := argPattern.FindStringSubmatch(line); match != nil {
			args[match[1]] = match[2]

			continue
		}
		if !strings.HasPrefix(line, "FROM ") {
			continue
		}
		fields := strings.Fields(line)
		imageIndex := 1
		if strings.HasPrefix(fields[imageIndex], "--platform=") {
			imageIndex++
		}
		image := variablePattern.ReplaceAllStringFunc(fields[imageIndex], func(variable string) string {
			name := strings.Trim(variable, "${}")

			return args[name]
		})
		if image != "scratch" && !stages[image] && !digestPattern.MatchString(image) {
			t.Errorf("Containerfile:%d external FROM %q is not SHA-256 digest-pinned", lineNumber+1, image)
		}
		if len(fields) > imageIndex+2 && strings.EqualFold(fields[imageIndex+1], "AS") {
			stages[fields[imageIndex+2]] = true
		}
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
