// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
	"gopkg.in/yaml.v3"
)

// A download names an artifact and trusts what comes back. Two things have to
// be true for that trust to be earned: something in this run produced that
// artifact, and the producing job finished first. GitHub enforces neither. A
// download of a name nothing uploads returns an error at the end of a long
// build; a download of a name produced by a job that is not in `needs` is a
// race that passes until the day the timing changes.
//
// This resolves the one shape that can be resolved statically here: literal
// names, including through one level of indirection: `needs: [build-cli]`
// where build-cli is `uses: ./.github/workflows/build-cli.yml` with
// `artifact-name: cli-dist`, and that workflow uploads `${{
// inputs.artifact-name }}`. That indirection is not an edge case; it is how
// every download step in this repository is fed.
//
// What is deliberately NOT attempted: evaluating format(), input defaults or
// step outputs. Those would be a reimplementation of the expression language,
// and a wrong one would report confident nonsense. A download step this cannot
// resolve (a run-time name, a pattern, or no name at all, which fetches every
// artifact of the run) is therefore a finding rather than a silent skip:
// release transfers whose names are run-time data go through the engine's
// typed transfer plan instead.

type lineageWorkflow struct {
	name string
	jobs map[string]*yaml.Node
}

func TestArtifactDownloadsHaveANeedsReachableProducer(t *testing.T) {
	t.Parallel()

	findings, resolved := lineageFindings(loadLineageWorkflows(t))
	if len(findings) > 0 {
		t.Fatalf("artifact lineage:\n  %s", strings.Join(findings, "\n  "))
	}

	if resolved == 0 {
		t.Fatal("no literal-named download was resolved to a producer; this guard measured nothing")
	}
}

// lineageFindings reports every download that is unresolvable or has no
// needs-reachable producer, and counts the downloads it resolved.
func lineageFindings(workflows map[string]lineageWorkflow) ([]string, int) {
	var (
		findings []string
		resolved int
	)

	for _, workflow := range workflows {
		for jobName, job := range workflow.jobs {
			names, unresolved := downloadNames(job)
			for _, reason := range unresolved {
				findings = append(findings, fmt.Sprintf(
					"%s: job %s has a download whose producer cannot be proven: %s; use a literal name",
					workflow.name, jobName, reason))
			}

			for _, want := range names {
				producers := reachableProducers(workflow, jobName, workflows)
				if producers[want] {
					resolved++

					continue
				}

				findings = append(findings, fmt.Sprintf(
					"%s: job %s downloads %q, which no job it needs produces (reachable producers: %s)",
					workflow.name, jobName, want, strings.Join(sortedKeys(producers), ", ")))
			}
		}
	}

	sort.Strings(findings)

	return findings, resolved
}

func TestLineageFindings_ReportsUnresolvableAndUnproducedDownloads(t *testing.T) {
	t.Parallel()

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(`
build:
  steps:
    - uses: actions/upload-artifact@v7
      with:
        name: dist
image:
  needs: [build]
  steps:
    - uses: actions/download-artifact@v8
      with:
        name: dist
    - uses: actions/download-artifact@v8
      with:
        name: ${{ inputs.name }}
racing:
  steps:
    - uses: actions/download-artifact@v8
      with:
        name: dist
`), &doc); err != nil {
		t.Fatal(err)
	}

	jobs := map[string]*yaml.Node{}
	for i := 0; i+1 < len(doc.Content[0].Content); i += 2 {
		jobs[doc.Content[0].Content[i].Value] = doc.Content[0].Content[i+1]
	}

	findings, resolved := lineageFindings(map[string]lineageWorkflow{"w.yml": {name: "w.yml", jobs: jobs}})

	if resolved != 1 {
		t.Errorf("resolved = %d, want 1", resolved)
	}

	want := []string{
		`w.yml: job image has a download whose producer cannot be proven: run-time name "${{ inputs.name }}"; use a literal name`,
		`w.yml: job racing downloads "dist", which no job it needs produces (reachable producers: )`,
	}
	if !slices.Equal(findings, want) {
		t.Errorf("findings = %q\nwant %q", findings, want)
	}
}

func TestDownloadNames_ReportsEveryStepItCannotResolve(t *testing.T) {
	t.Parallel()

	var job yaml.Node
	if err := yaml.Unmarshal([]byte(`
steps:
  - uses: actions/download-artifact@v8
    with:
      name: cli-dist
  - uses: actions/download-artifact@v8
    with:
      name: sbom-${{ steps.arch.outputs.suffix }}
  - uses: actions/download-artifact@v8
    with:
      pattern: dist-*
  - uses: actions/download-artifact@v8
  - uses: actions/upload-artifact@v7
    with:
      name: ${{ github.run_id }}
`), &job); err != nil {
		t.Fatal(err)
	}

	names, unresolved := downloadNames(job.Content[0])

	if want := []string{"cli-dist"}; !slices.Equal(names, want) {
		t.Errorf("names = %q, want %q", names, want)
	}

	want := []string{
		`run-time name "sbom-${{ steps.arch.outputs.suffix }}"`,
		`pattern "dist-*"`,
		"no name, which downloads every artifact of the run",
	}
	if !slices.Equal(unresolved, want) {
		t.Errorf("unresolved = %q, want %q", unresolved, want)
	}
}

// downloadNames returns the literal artifact names a job's download steps
// fetch, and a reason for each download step whose name is not a literal.
func downloadNames(job *yaml.Node) ([]string, []string) {
	steps := mappingChild(job, "steps")
	if steps == nil {
		return nil, nil
	}

	var names, unresolved []string

	for _, step := range steps.Content {
		if !strings.HasPrefix(scalarChild(step, "uses"), "actions/download-artifact@") {
			continue
		}

		with := mappingChild(step, "with")
		name := scalarChild(with, "name")

		switch {
		case scalarChild(with, "pattern") != "":
			unresolved = append(unresolved, fmt.Sprintf("pattern %q", scalarChild(with, "pattern")))
		case name == "":
			unresolved = append(unresolved, "no name, which downloads every artifact of the run")
		case expressionPattern.MatchString(name):
			unresolved = append(unresolved, fmt.Sprintf("run-time name %q", name))
		default:
			names = append(names, name)
		}
	}

	return names, unresolved
}

var expressionPattern = regexp.MustCompile(`\$\{\{`)

// reachableProducers walks the job's transitive `needs` and collects every
// literal artifact name those jobs produce, directly or through one local
// reusable-workflow call.
func reachableProducers(workflow lineageWorkflow, from string, all map[string]lineageWorkflow) map[string]bool {
	produced := map[string]bool{}
	seen := map[string]bool{from: true}
	queue := needsOf(workflow.jobs[from])

	for len(queue) > 0 {
		jobName := queue[0]
		queue = queue[1:]

		if seen[jobName] {
			continue
		}

		seen[jobName] = true

		job, ok := workflow.jobs[jobName]
		if !ok {
			continue
		}

		for name := range producedNames(job, all) {
			produced[name] = true
		}

		queue = append(queue, needsOf(job)...)
	}

	return produced
}

// producedNames returns the literal artifact names a job uploads. For a job
// that calls a local reusable workflow, the call's `with:` values are
// substituted into that workflow's upload names, which is the single level of
// indirection this resolver supports.
func producedNames(job *yaml.Node, all map[string]lineageWorkflow) map[string]bool {
	produced := map[string]bool{}

	if steps := mappingChild(job, "steps"); steps != nil {
		for _, step := range steps.Content {
			if !strings.HasPrefix(scalarChild(step, "uses"), "actions/upload-artifact@") {
				continue
			}

			if name := scalarChild(mappingChild(step, "with"), "name"); name != "" && !expressionPattern.MatchString(name) {
				produced[name] = true
			}
		}

		return produced
	}

	target, ok := all[internalWorkflowTarget(scalarChild(job, "uses"))]
	if !ok {
		return produced
	}

	with := mappingChild(job, "with")

	for _, called := range target.jobs {
		steps := mappingChild(called, "steps")
		if steps == nil {
			continue
		}

		for _, step := range steps.Content {
			if !strings.HasPrefix(scalarChild(step, "uses"), "actions/upload-artifact@") {
				continue
			}

			if name, resolvedOK := resolveInputPassthrough(scalarChild(mappingChild(step, "with"), "name"), with); resolvedOK {
				produced[name] = true
			}
		}
	}

	return produced
}

// inputPassthroughPattern matches an upload name that is exactly one input
// reference, e.g. `${{ inputs.artifact-name }}`. Anything more complex is a
// computation this resolver will not guess at.
var inputPassthroughPattern = regexp.MustCompile(`^\$\{\{\s*inputs(?:\.|\['|\[")([A-Za-z0-9_-]+)(?:'\]|"\])?\s*\}\}$`)

func resolveInputPassthrough(uploadName string, with *yaml.Node) (string, bool) {
	if uploadName == "" {
		return "", false
	}

	if !expressionPattern.MatchString(uploadName) {
		return uploadName, true
	}

	match := inputPassthroughPattern.FindStringSubmatch(strings.TrimSpace(uploadName))
	if match == nil {
		return "", false
	}

	value := scalarChild(with, match[1])
	if value == "" || expressionPattern.MatchString(value) {
		return "", false
	}

	return value, true
}

func needsOf(job *yaml.Node) []string {
	needs := mappingChild(job, "needs")
	if needs == nil {
		return nil
	}

	if needs.Kind == yaml.ScalarNode {
		return []string{needs.Value}
	}

	names := make([]string, 0, len(needs.Content))
	for _, entry := range needs.Content {
		names = append(names, entry.Value)
	}

	return names
}

func loadLineageWorkflows(t *testing.T) map[string]lineageWorkflow {
	t.Helper()

	dir := filepath.Join(reporoot.Path(t), ".github", "workflows")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	workflows := map[string]lineageWorkflow{}

	for _, entry := range entries {
		if entry.IsDir() || !isWorkflowFile(entry.Name()) {
			continue
		}

		body, readErr := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // repository workflow fixture.
		if readErr != nil {
			t.Fatalf("read %s: %v", entry.Name(), readErr)
		}

		var doc yaml.Node
		if unmarshalErr := yaml.Unmarshal(body, &doc); unmarshalErr != nil {
			t.Fatalf("parse %s: %v", entry.Name(), unmarshalErr)
		}

		workflow := lineageWorkflow{name: entry.Name(), jobs: map[string]*yaml.Node{}}

		if jobs := mappingChild(doc.Content[0], "jobs"); jobs != nil {
			for i := 0; i+1 < len(jobs.Content); i += 2 {
				workflow.jobs[jobs.Content[i].Value] = jobs.Content[i+1]
			}
		}

		workflows[entry.Name()] = workflow
	}

	return workflows
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}
