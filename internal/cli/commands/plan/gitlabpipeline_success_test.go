// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package plan_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	gitlabpipeline "github.com/diggsweden/reusable-ci/v3/internal/app/gitlabpipeline"
	plancmd "github.com/diggsweden/reusable-ci/v3/internal/cli/commands/plan"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/ghaenv"
)

// TestGitlabBuildPipelineCmd_WritesTheGeneratedPipeline runs the command on a
// valid two-ecosystem build-stage plan and requires the destination to hold
// exactly the bytes the generator marshals for the same plan, component,
// version and stage. A symlink planted at the destination is replaced by a
// regular file and its target left alone; the file used to be written through
// the link. No successful publish child pipeline exists to compare: every
// publish target is refused until artifact handoff can be generated.
func TestGitlabBuildPipelineCmd_WritesTheGeneratedPipeline(t *testing.T) {
	env := ghaenv.Setup(t)

	plan := pipeline.ReleaseBuildStagePlan{Version: pipeline.ReleasePlanVersion, Stage: "build", Targets: pipeline.ReleaseBuildTargets{
		Maven: pipeline.TargetPlan[pipeline.PlannedArtifact]{Runs: true, Items: []pipeline.PlannedArtifact{
			{Name: "core", ProjectType: projecttype.Maven, WorkingDirectory: "core", BuildType: config.BuildTypeLibrary},
		}},
		Go: pipeline.TargetPlan[pipeline.PlannedArtifact]{Runs: true, Items: []pipeline.PlannedArtifact{
			{Name: "cli", ProjectType: projecttype.Go, Go: &config.GoConfig{SkipTests: true}},
			{Name: "agent", ProjectType: projecttype.Go, WorkingDirectory: "agent"},
		}},
	}}

	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}

	env.Setenv("BUILD_STAGE_PLAN_JSON", string(raw))

	child, err := gitlabpipeline.BuildStagePipeline(plan, gitlabpipeline.BuildPipelineOptions{
		ComponentBase: "catalog.example/components", ComponentRef: "1.0.0", Version: "2.3.4", Stage: "build",
	})
	if err != nil {
		t.Fatal(err)
	}

	want, err := child.Marshal()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere.yml")
	out := filepath.Join(dir, "child.yml")

	if writeErr := os.WriteFile(target, []byte("untouched\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	if linkErr := os.Symlink(target, out); linkErr != nil {
		t.Fatal(linkErr)
	}

	if runErr := plancmd.New().Run(context.Background(), []string{
		"plan", "gitlab-build-pipeline",
		"--component-base", "catalog.example/components",
		"--component-ref", "1.0.0",
		"--version", "2.3.4",
		"--output", out,
	}); runErr != nil {
		t.Fatal(runErr)
	}

	info, err := os.Lstat(out)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
		t.Fatalf("destination = %v (err %v), want a regular 0644 file", info, err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("written pipeline:\n%s\nwant:\n%s", got, want)
	}

	if body, _ := os.ReadFile(target); string(body) != "untouched\n" {
		t.Errorf("the symlink target was written: %q", body)
	}
}
