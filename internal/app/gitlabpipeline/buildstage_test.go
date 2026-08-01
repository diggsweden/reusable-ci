// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlabpipeline_test

import (
	"strings"
	"testing"

	gitlabpipeline "github.com/diggsweden/reusable-ci/v3/internal/app/gitlabpipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
)

func runningTarget(items ...pipeline.PlannedArtifact) pipeline.TargetPlan[pipeline.PlannedArtifact] {
	return pipeline.TargetPlan[pipeline.PlannedArtifact]{Runs: true, Items: items}
}

func TestBuildStagePipeline_FansOutRunningTargets(t *testing.T) {
	t.Parallel()

	plan := pipeline.ReleaseBuildStagePlan{
		Version: 1,
		Stage:   "build",
		Targets: pipeline.ReleaseBuildTargets{
			Maven:  runningTarget(pipeline.PlannedArtifact{Name: "lib-core", WorkingDirectory: "core", BuildType: config.BuildTypeLibrary}),
			NPM:    runningTarget(pipeline.PlannedArtifact{Name: "web", WorkingDirectory: "ui"}),
			Gradle: runningTarget(pipeline.PlannedArtifact{Name: "jvm", WorkingDirectory: "svc"}),
			Go:     runningTarget(pipeline.PlannedArtifact{Name: "cli", WorkingDirectory: "."}),
			Cargo:  runningTarget(pipeline.PlannedArtifact{Name: "rustcli", WorkingDirectory: "rs"}),
		},
	}

	child := gitlabpipeline.BuildStagePipeline(plan, gitlabpipeline.BuildPipelineOptions{
		ComponentBase: "$CI_SERVER_FQDN/diggsweden/reusable-ci",
		ComponentRef:  "1.0.0",
		Version:       "2.3.4",
	})

	if len(child.Include) != 5 {
		t.Fatalf("includes = %d, want 5 (maven + npm + gradle + go + cargo)", len(child.Include))
	}

	// Every ecosystem maps to its build-<eco> component with the per-item dir.
	byComponent := map[string]gitlabpipeline.IncludeEntry{}
	for _, inc := range child.Include {
		byComponent[inc.Component] = inc
	}

	base := "$CI_SERVER_FQDN/diggsweden/reusable-ci"
	for eco, wantDir := range map[string]string{"npm": "ui", "gradle": "svc", "cargo": "rs"} {
		comp := base + "/build-" + eco + "@1.0.0"

		inc, ok := byComponent[comp]
		if !ok {
			t.Fatalf("missing include for %s", comp)
		}

		if inc.Inputs["job-name"] != "build-"+eco+"-"+map[string]string{"npm": "web", "gradle": "jvm", "cargo": "rustcli"}[eco] {
			t.Errorf("%s job-name = %q", eco, inc.Inputs["job-name"])
		}

		if inc.Inputs["working-directory"] != wantDir {
			t.Errorf("%s working-directory = %q, want %q", eco, inc.Inputs["working-directory"], wantDir)
		}

		if _, has := inc.Inputs["build-type"]; has {
			t.Errorf("%s should not carry build-type: %v", eco, inc.Inputs)
		}
	}

	// Maven include: build-type mapped library → lib, per-item job-name + dir.
	maven := byComponent[base+"/build-maven@1.0.0"]
	if maven.Inputs["build-type"] != "lib" {
		t.Errorf("maven build-type = %q, want lib", maven.Inputs["build-type"])
	}

	if maven.Inputs["job-name"] != "build-maven-lib-core" || maven.Inputs["working-directory"] != "core" || maven.Inputs["version"] != "2.3.4" {
		t.Errorf("maven inputs = %v", maven.Inputs)
	}

	// artifact-name scopes uploads per item (avoids multi-artifact collision).
	if maven.Inputs["artifact-name"] != "lib-core" {
		t.Errorf("maven artifact-name = %q, want lib-core", maven.Inputs["artifact-name"])
	}

	// Go include: working-directory "." preserved, no build-type.
	gobuild := byComponent[base+"/build-go@1.0.0"]
	if gobuild.Inputs["job-name"] != "build-go-cli" || gobuild.Inputs["working-directory"] != "." {
		t.Errorf("go inputs = %v", gobuild.Inputs)
	}

	if _, ok := gobuild.Inputs["build-type"]; ok {
		t.Errorf("go should not carry build-type: %v", gobuild.Inputs)
	}
}

func TestBuildStagePipeline_SkipsNonRunningAndEmits(t *testing.T) {
	t.Parallel()

	plan := pipeline.ReleaseBuildStagePlan{Stage: "build"} // nothing runs

	child := gitlabpipeline.BuildStagePipeline(plan, gitlabpipeline.BuildPipelineOptions{ComponentRef: "1.0.0"})
	if len(child.Include) != 0 {
		t.Errorf("includes = %d, want 0", len(child.Include))
	}

	body, err := child.Marshal()
	if err != nil {
		t.Fatal(err)
	}

	// Deterministic, valid YAML with the stage.
	if !strings.Contains(string(body), "stages:") || !strings.Contains(string(body), "- build") {
		t.Errorf("marshalled child pipeline missing stages:\n%s", body)
	}
}

func TestBuildStagePipeline_SanitisesJobNames(t *testing.T) {
	t.Parallel()

	plan := pipeline.ReleaseBuildStagePlan{
		Stage:   "build",
		Targets: pipeline.ReleaseBuildTargets{NPM: runningTarget(pipeline.PlannedArtifact{Name: "@scope/My_Pkg"})},
	}

	child := gitlabpipeline.BuildStagePipeline(plan, gitlabpipeline.BuildPipelineOptions{ComponentRef: "1.0.0"})
	if got := child.Include[0].Inputs["job-name"]; got != "build-npm-scope-my-pkg" {
		t.Errorf("job-name = %q, want build-npm-scope-my-pkg", got)
	}
}
