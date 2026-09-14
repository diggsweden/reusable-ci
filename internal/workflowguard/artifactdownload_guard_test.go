// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package workflowguard

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/reporoot"

	"gopkg.in/yaml.v3"
)

type artifactDownloadWorkflow struct {
	Jobs map[string]artifactDownloadJob `yaml:"jobs"`
}

type artifactDownloadJob struct {
	Permissions map[string]string      `yaml:"permissions"`
	Uses        string                 `yaml:"uses"`
	Steps       []artifactDownloadStep `yaml:"steps"`
}

type artifactDownloadStep struct {
	Run  string            `yaml:"run"`
	Uses string            `yaml:"uses"`
	With map[string]string `yaml:"with"`
}

// TestArtifactDownloadJobsDeclareActionsRead covers both permission boundaries:
// the job that calls the Actions artifact REST API and every repository-internal
// reusable-workflow caller in the chain leading to that job.
func TestArtifactDownloadJobsDeclareActionsRead(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(reporoot.Path(t), ".github", "workflows")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	workflows := make(map[string]artifactDownloadWorkflow)

	for _, entry := range entries {
		if entry.IsDir() || !isWorkflowFile(entry.Name()) {
			continue
		}

		body, readErr := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // repository workflow fixture.
		if readErr != nil {
			t.Fatalf("read %s: %v", entry.Name(), readErr)
		}

		var workflow artifactDownloadWorkflow
		if unmarshalErr := yaml.Unmarshal(body, &workflow); unmarshalErr != nil {
			t.Fatalf("parse %s: %v", entry.Name(), unmarshalErr)
		}

		workflows[entry.Name()] = workflow
	}

	violations := artifactDownloadPermissionViolations(workflows)
	if len(violations) != 0 {
		t.Fatalf("artifact-download permission violations:\n  %s", strings.Join(violations, "\n  "))
	}
}

func TestArtifactDownloadPermissionGuardCoversExecutorsAndCallers(t *testing.T) {
	t.Parallel()

	workflows := map[string]artifactDownloadWorkflow{
		"leaf.yml": {
			Jobs: map[string]artifactDownloadJob{
				"download": {
					Permissions: map[string]string{"contents": "read"},
					Steps:       []artifactDownloadStep{{Run: "reusable-ci\n  artifact download"}},
				},
			},
		},
		"forwarder.yml": {
			Jobs: map[string]artifactDownloadJob{
				"call": {
					Permissions: map[string]string{"actions": "write"},
					Uses:        "./.github/workflows/leaf.yml",
				},
			},
		},
		"root.yml": {
			Jobs: map[string]artifactDownloadJob{
				"call": {
					Permissions: map[string]string{"actions": "read"},
					Uses:        "./.github/workflows/forwarder.yml",
				},
			},
		},
	}

	report := strings.Join(artifactDownloadPermissionViolations(workflows), "\n")
	for _, want := range []string{
		"leaf.yml: job download executes an artifact download",
		"forwarder.yml: job call calls artifact-downloading workflow leaf.yml",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("guard report missing %q:\n%s", want, report)
		}
	}

	if strings.Contains(report, "root.yml") {
		t.Errorf("guard rejected transitive caller with actions: read:\n%s", report)
	}
}

// TestStepLeavesTheRunForAnArtifact_JudgesTheReachNotTheSpelling is the case
// table for the detector itself. The `uses:` rows are the ones the previous
// guard could not express at all: it read only `run:`, so every
// actions/download-artifact step in the repository was invisible to it.
func TestStepLeavesTheRunForAnArtifact_JudgesTheReachNotTheSpelling(t *testing.T) {
	t.Parallel()

	const action = "actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c"

	for _, tc := range []struct {
		name string
		step artifactDownloadStep
		want bool
		why  string
	}{
		{
			name: "CLI artifact download", step: artifactDownloadStep{Run: "reusable-ci artifact download"}, want: true,
			why: "the REST artifacts API is permission-gated",
		},
		{
			name: "CLI release download-artifacts", step: artifactDownloadStep{Run: "reusable-ci release download-artifacts"}, want: true,
			why: "same API",
		},
		{
			name: "CLI verb wrapped across lines", step: artifactDownloadStep{Run: "reusable-ci \\\n  artifact download"}, want: true,
			why: "a line continuation is not a different command",
		},
		{
			name: "gh run download", step: artifactDownloadStep{Run: "gh run download 123 -n dist"}, want: true,
			why: "the gh CLI reaches the same API",
		},
		{
			name: "gh api against the artifacts endpoint", step: artifactDownloadStep{Run: "gh api /repos/o/r/actions/artifacts"}, want: true,
			why: "calling the endpoint directly is the same reach",
		},
		{
			name: "download-artifact for another run", want: true,
			step: artifactDownloadStep{Uses: action, With: map[string]string{"name": "dist", "run-id": "${{ github.event.workflow_run.id }}"}},
			why:  "run-id switches the action to the REST API",
		},
		{
			name: "download-artifact with an explicit token", want: true,
			//nolint:gosec // G101: a workflow expression in a fixture, not a credential.
			step: artifactDownloadStep{Uses: action, With: map[string]string{"name": "dist", "github-token": "${{ github.token }}"}},
			why:  "a supplied token means the REST API too",
		},
		{
			name: "download-artifact for this run", want: false,
			step: artifactDownloadStep{Uses: action, With: map[string]string{"name": "cli-dist", "path": "cli-dist"}},
			why:  "v4+ uses the run's own runtime token and needs no permission; demanding one would widen four jobs for nothing",
		},
		{
			name: "upload, not download", want: false,
			step: artifactDownloadStep{Uses: "actions/upload-artifact@v4", With: map[string]string{"name": "dist"}},
			why:  "the upload path is the results service, not the REST API",
		},
		{
			name: "an unrelated action", want: false,
			step: artifactDownloadStep{Uses: "actions/checkout@v6"},
			why:  "not a download",
		},
		{
			name: "prose mentioning the verb", want: true,
			step: artifactDownloadStep{Run: "echo 'see reusable-ci artifact download'"},
			why: "the run-block match is textual, so a mention in an echo counts. That is over-reporting, " +
				"and the direction matters: it asks for a permission that is not needed, rather than missing " +
				"one that is. Parsing the block instead would mean parsing ${{ }} as shell, which it is not.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := stepLeavesTheRunForAnArtifact(tc.step); got != tc.want {
				t.Errorf("got %v, want %v: %s", got, tc.want, tc.why)
			}
		})
	}
}

// The repository must actually contain both kinds of step, or the distinction
// above is theory and the walk is measuring an empty set.
func TestArtifactDownloadGuard_SeesBothKindsOfDownloadInTheTree(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(reporoot.Path(t), ".github", "workflows")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	sameRun, crossRun := 0, 0

	for _, entry := range entries {
		if entry.IsDir() || !isWorkflowFile(entry.Name()) {
			continue
		}

		body, readErr := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // repository workflow fixture.
		if readErr != nil {
			t.Fatalf("read %s: %v", entry.Name(), readErr)
		}

		var workflow artifactDownloadWorkflow
		if unmarshalErr := yaml.Unmarshal(body, &workflow); unmarshalErr != nil {
			t.Fatalf("parse %s: %v", entry.Name(), unmarshalErr)
		}

		for _, job := range workflow.Jobs {
			for _, step := range job.Steps {
				switch {
				case stepLeavesTheRunForAnArtifact(step):
					crossRun++
				case strings.HasPrefix(step.Uses, "actions/download-artifact@"):
					sameRun++
				}
			}
		}
	}

	if crossRun == 0 || sameRun == 0 {
		t.Fatalf("found %d run-leaving and %d same-run downloads; the guard needs both present to be distinguishing anything", crossRun, sameRun)
	}
}

func artifactDownloadPermissionViolations(workflows map[string]artifactDownloadWorkflow) []string {
	requiresActionsRead := make(map[string]bool)

	for name, workflow := range workflows {
		for _, job := range workflow.Jobs {
			if jobDownloadsArtifact(job) {
				requiresActionsRead[name] = true

				break
			}
		}
	}

	for changed := true; changed; {
		changed = false

		for name, workflow := range workflows {
			if requiresActionsRead[name] {
				continue
			}

			for _, job := range workflow.Jobs {
				if requiresActionsRead[internalWorkflowTarget(job.Uses)] {
					requiresActionsRead[name] = true
					changed = true

					break
				}
			}
		}
	}

	var violations []string

	for workflowName, workflow := range workflows {
		for jobName, job := range workflow.Jobs {
			reason := ""

			switch target := internalWorkflowTarget(job.Uses); {
			case jobDownloadsArtifact(job):
				reason = "executes an artifact download"
			case requiresActionsRead[target]:
				reason = "calls artifact-downloading workflow " + target
			}

			if reason != "" && job.Permissions["actions"] != "read" {
				violations = append(violations, fmt.Sprintf(
					"%s: job %s %s but declares actions: %q; want read",
					workflowName, jobName, reason, job.Permissions["actions"],
				))
			}
		}
	}

	slices.Sort(violations)

	return violations
}

// jobDownloadsArtifact reports whether a job reaches OUTSIDE the current run
// for an artifact, which is what `actions: read` actually gates.
//
// The distinction is the whole point and is easy to get backwards. Two things
// here download artifacts:
//
//   - `reusable-ci artifact download` and `release download-artifacts` use the
//     plain REST artifacts API (see internal/adapters/github/runartifact.go,
//     where the comment contrasts it with the upload path's results service).
//     The REST API is permission-gated, so these always need `actions: read`.
//   - `actions/download-artifact` v4+ uses the run's own ACTIONS_RUNTIME_TOKEN
//     for a same-run artifact and needs no permission at all. It switches to the
//     REST API only when told to look at another run, through `run-id` or an
//     explicit `github-token`.
//
// A guard that demanded `actions: read` for every download-artifact step would
// be wrong, and would push four jobs in self-runtime-container.yml to widen
// their permissions for no reason. A guard that only reads `run:` — which is
// what this one used to do — cannot see those steps at all, so it would also
// miss the day one of them gains a `run-id`. Hence: see every step, and judge
// it by whether it leaves the run.
func jobDownloadsArtifact(job artifactDownloadJob) bool {
	for _, step := range job.Steps {
		if stepLeavesTheRunForAnArtifact(step) {
			return true
		}
	}

	return false
}

func stepLeavesTheRunForAnArtifact(step artifactDownloadStep) bool {
	// A backslash line continuation is not a different command. Removing it
	// before normalising whitespace is the difference between seeing
	// `reusable-ci \<newline>  artifact download` and not.
	run := strings.Join(strings.Fields(strings.ReplaceAll(step.Run, "\\\n", " ")), " ")
	for _, verb := range []string{
		"reusable-ci artifact download",
		"reusable-ci release download-artifacts",
		"gh run download",
	} {
		if strings.Contains(run, verb) {
			return true
		}
	}

	if strings.Contains(run, "gh api") && strings.Contains(run, "/actions/artifacts") {
		return true
	}

	if !strings.HasPrefix(step.Uses, "actions/download-artifact@") {
		return false
	}

	// Either input makes the action query the REST API for another run's
	// artifacts instead of using this run's runtime token.
	return step.With["run-id"] != "" || step.With["github-token"] != ""
}

func isWorkflowFile(name string) bool {
	return strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")
}

func internalWorkflowTarget(uses string) string {
	const prefix = "./.github/workflows/"
	if !strings.HasPrefix(uses, prefix) {
		return ""
	}

	return strings.TrimPrefix(uses, prefix)
}
