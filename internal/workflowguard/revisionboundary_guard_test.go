// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"
	"gopkg.in/yaml.v3"
)

func TestReusableWorkflowsDoNotPinNestedReusableCIAction(t *testing.T) {
	t.Parallel()

	workflowsDir := filepath.Join(reporoot.Path(t), ".github", "workflows")

	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yml" {
			continue
		}

		body, err := os.ReadFile(filepath.Join(workflowsDir, entry.Name())) //nolint:gosec // repository contract fixture.
		if err != nil {
			t.Fatal(err)
		}

		text := string(body)
		if strings.Contains(text, "diggsweden/reusable-ci/.github/actions/install-reusable-ci@") {
			t.Errorf("%s pins the nested installer action; sparse-checkout scripts/bootstrap at reusable-ci-binary-ref instead", entry.Name())
		}

		if strings.Contains(text, ".github-shared/scripts/bootstrap/install-reusable-ci.sh") {
			for _, want := range []string{
				"repository: diggsweden/reusable-ci",
				"ref: ${{ inputs['reusable-ci-binary-ref'] }}",
				"sparse-checkout: scripts/bootstrap",
			} {
				if !strings.Contains(text, want) {
					t.Errorf("%s installs reusable-ci without the shared repository/ref boundary %q", entry.Name(), want)
				}
			}
		}
	}
}

func TestPublicWorkflowsDoNotDefaultBranchesToMain(t *testing.T) {
	t.Parallel()

	workflowsDir := filepath.Join(reporoot.Path(t), ".github", "workflows")

	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yml" {
			continue
		}

		body, err := os.ReadFile(filepath.Join(workflowsDir, entry.Name())) //nolint:gosec // repository contract fixture.
		if err != nil {
			t.Fatal(err)
		}

		if branchDefaultIsMain(body) {
			t.Errorf("%s defaults a public input to main; require mutation refs and let read-only work use its triggering ref", entry.Name())
		}
	}
}

// The declared mutation workflows: reusable workflows whose ref input must be
// required and defaultless, so a caller can never let one mutate at whatever
// ref happened to trigger it.
//
// This list is written out rather than discovered, and the reason is worth
// stating because the next reader will want to delete it. Four of these
// (publish-apple-appstore, publish-google-play, publish-maven-central,
// validate-release-prerequisites) mutate systems OUTSIDE GitHub, and two are
// orchestrators that delegate. None of that is visible in a GitHub permissions
// block, so no rule over the workflow files can find them.
//
// What CAN be discovered is the repository/registry half, and
// TestMutationAuthorityDiscoversNewWorkflows below does that and cross-checks
// its result against this list. A new workflow that writes contents or packages
// therefore cannot slip past by not being named here; only the
// external-publisher case still needs a human to add a row, and the comment
// above says so instead of the list silently being the whole story.
func declaredMutationWorkflows() map[string]string {
	return map[string]string{
		"publish-apple-appstore.yml":         "branch",
		"publish-container.yml":              "branch",
		"publish-google-play.yml":            "branch",
		"publish-maven-central.yml":          "branch",
		"publish-maven-github.yml":           "branch",
		"publish-snapshot-npm.yml":           "branch",
		"release-create-github.yml":          "checkout-ref",
		"release-orchestrator.yml":           "branch",
		"release-prepare-stage.yml":          "branch",
		"release-publish-stage.yml":          "branch",
		"release-snapshot-orchestrator.yml":  "branch",
		"validate-release-prerequisites.yml": "branch",
		"version-bump.yml":                   "branch",
	}
}

func TestMutationWorkflowsRequireExplicitRef(t *testing.T) {
	t.Parallel()

	for workflow, input := range declaredMutationWorkflows() {
		body, err := os.ReadFile(filepath.Join(reporoot.Path(t), ".github", "workflows", workflow)) //nolint:gosec // repository contract fixture.
		if err != nil {
			t.Fatal(err)
		}

		var doc yaml.Node
		if err := yaml.Unmarshal(body, &doc); err != nil {
			t.Fatalf("parse %s: %v", workflow, err)
		}

		spec := mappingChild(mappingChild(mappingChild(mappingChild(doc.Content[0], "on"), "workflow_call"), "inputs"), input)
		if spec == nil || scalarChild(spec, "required") != "true" {
			t.Errorf("%s input %s must be required", workflow, input)
		}

		if mappingChild(spec, "default") != nil {
			t.Errorf("%s input %s must not have a default", workflow, input)
		}
	}
}

// TestMutationAuthorityDiscoversNewWorkflows is the half that does not depend
// on anyone remembering to add a row.
//
// A reusable workflow that writes repository or package state and checks out
// the CALLER's repository decides, by that checkout, which code runs with that
// authority. Leaving the ref implicit hands that decision to whatever ref
// triggered the caller. So: find those jobs by their effective properties, and
// require both an explicit ref and a declared row.
//
// The checkout of `diggsweden/reusable-ci` for the bootstrap scripts is a
// different thing — it names its own repository and its own ref input — and is
// excluded on that basis, not by filename.
func TestMutationAuthorityDiscoversNewWorkflows(t *testing.T) {
	t.Parallel()

	declared := declaredMutationWorkflows()
	discovered, scanned := discoverMutationAuthority(t)

	if scanned < 20 {
		t.Fatalf("scanned %d reusable workflows; the walk, not the tree, is what was measured", scanned)
	}

	if len(discovered) == 0 {
		t.Fatal("no workflow was found to write repository or package state; the discovery rule matches nothing")
	}

	names := make([]string, 0, len(discovered))
	for name := range discovered {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		for _, finding := range discovered[name] {
			if finding.ref == "" {
				t.Errorf("%s job %s holds %s and checks out the caller's repository with no ref; "+
					"pin it to a required input so the caller cannot choose the code that runs with that authority",
					name, finding.job, finding.authority)
			}
		}

		if _, ok := declared[name]; !ok {
			t.Errorf("%s writes repository or package state but is not in declaredMutationWorkflows; "+
				"add it with the name of the input its checkout ref comes from", name)
		}
	}
}

type mutationFinding struct {
	job       string
	authority string
	ref       string
}

// discoverMutationAuthority returns, per reusable workflow, the jobs that both
// hold a write permission over repository or package state and check out the
// caller's own repository.
func discoverMutationAuthority(t *testing.T) (map[string][]mutationFinding, int) {
	t.Helper()

	dir := filepath.Join(reporoot.Path(t), ".github", "workflows")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	found := map[string][]mutationFinding{}
	scanned := 0

	for _, entry := range entries {
		if entry.IsDir() || !isWorkflowFile(entry.Name()) {
			continue
		}

		body, readErr := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // repository contract fixture.
		if readErr != nil {
			t.Fatalf("read %s: %v", entry.Name(), readErr)
		}

		var doc yaml.Node
		if unmarshalErr := yaml.Unmarshal(body, &doc); unmarshalErr != nil {
			t.Fatalf("parse %s: %v", entry.Name(), unmarshalErr)
		}

		root := doc.Content[0]
		if mappingChild(mappingChild(root, "on"), "workflow_call") == nil {
			continue
		}

		scanned++

		if findings := mutationAuthorityIn(root); len(findings) > 0 {
			found[entry.Name()] = findings
		}
	}

	return found, scanned
}

func mutationAuthorityIn(root *yaml.Node) []mutationFinding {
	jobs := mappingChild(root, "jobs")
	if jobs == nil {
		return nil
	}

	var findings []mutationFinding

	for i := 0; i+1 < len(jobs.Content); i += 2 {
		jobName, job := jobs.Content[i].Value, jobs.Content[i+1]

		authority := writeAuthorityOf(mappingChild(job, "permissions"))
		if authority == "" {
			continue
		}

		steps := mappingChild(job, "steps")
		if steps == nil {
			continue
		}

		for _, step := range steps.Content {
			if !strings.HasPrefix(scalarChild(step, "uses"), "actions/checkout@") {
				continue
			}

			with := mappingChild(step, "with")

			// A checkout naming another repository is not the caller's code;
			// the bootstrap sparse-checkout of diggsweden/reusable-ci is the
			// only such case here and carries its own pinned ref input.
			if scalarChild(with, "repository") != "" {
				continue
			}

			findings = append(findings, mutationFinding{job: jobName, authority: authority, ref: scalarChild(with, "ref")})
		}
	}

	return findings
}

// writeAuthorityOf names the write permissions that mutate durable state.
// id-token: write is deliberately not one of them: it mints an OIDC token bound
// to the workflow identity rather than changing anything, and treating it as
// mutation authority would drag every keyless-signing job into a rule about
// checkout refs that answers a different question.
func writeAuthorityOf(permissions *yaml.Node) string {
	var held []string

	for _, key := range []string{"contents", "packages"} {
		if scalarChild(permissions, key) == "write" {
			held = append(held, key+": write")
		}
	}

	return strings.Join(held, ", ")
}
