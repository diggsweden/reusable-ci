// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package plan_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	plancmd "github.com/diggsweden/reusable-ci/internal/cli/commands/plan"
	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/internal/testutil/ghaenv"
)

func TestReleaseCmd_UsesConfigPlanAndEmitsTypedPlans(t *testing.T) {
	env := ghaenv.Setup(t)

	cfg := &config.Config{
		Artifacts: []config.Artifact{
			{Name: "web", ProjectType: projecttype.NPM, PublishTo: []config.PublishTarget{config.PublishGitHubPackages}},
			{Name: "go-cli", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeArtifactFirst}},
		},
		Containers: []config.Container{{Name: "image"}},
	}
	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}

	env.Setenv("CONFIG_PLAN_JSON", mustConfigPlanJSON(t, pipeline.NewConfigPlan(cfg)))
	env.Setenv("GITHUB_REF_NAME", "v1.2.3")
	env.Setenv("BRANCH", "main")
	env.Setenv("RELEASE_PUBLISHER", "github-cli")
	env.Setenv("RELEASE_SBOMS", "all")
	env.Setenv("RELEASE_SIGN_ARTIFACTS", "true")
	env.Setenv("CHANGELOG_CREATOR", "git-cliff")

	cmd := plancmd.New()
	if err := cmd.Run(context.Background(), []string{"plan", "release"}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	if got := env.Output("release-plan-json"); !strings.Contains(got, `"version":1`) || !strings.Contains(got, `"make_latest":true`) {
		t.Errorf("release-plan-json = %s", got)
	}

	if got := env.Output("release-policy-json"); got != "" {
		t.Errorf("release-policy-json compatibility output should not be emitted, got %s", got)
	}

	if got := env.Output("build-stage-plan-json"); !strings.Contains(got, `"stage":"build"`) || !strings.Contains(got, `"go":{"runs":true`) {
		t.Errorf("build-stage-plan-json = %s", got)
	}

	if got := env.Output("publish-stage-plan-json"); !strings.Contains(got, `"containers":{"runs":true`) {
		t.Errorf("publish-stage-plan-json = %s", got)
	}
}

func TestSnapshotReleaseCmd_UsesConfigPlanFallbackAndEmitsStagePlans(t *testing.T) {
	env := ghaenv.Setup(t)
	env.Setenv("CONFIG_PLAN_JSON", mustConfigPlanJSON(t, pipeline.NewConfigPlan(&config.Config{
		Artifacts: []config.Artifact{
			{Name: "web", ProjectType: projecttype.NPM},
			{Name: "worker", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeArtifactFirst}},
		},
		Containers: []config.Container{{Name: "image"}},
	})))
	env.Setenv("BRANCH", "feature/dev-plan")
	env.Setenv("PUBLISH_NPM", "true")
	env.Setenv("USE_CI_TOKEN", "true")

	cmd := plancmd.New()
	if err := cmd.Run(context.Background(), []string{"plan", "snapshot-release"}); err != nil {
		t.Fatal(err)
	}

	if got := env.Output("snapshot-release-plan-json"); !strings.Contains(got, `"branch":"feature/dev-plan"`) {
		t.Errorf("snapshot-release-plan-json = %s", got)
	}

	if got := env.Output("dev-context-json"); got != "" {
		t.Errorf("dev-context-json compatibility output should not be emitted, got %s", got)
	}

	if got := env.Output("snapshot-build-stage-plan-json"); !strings.Contains(got, `"stage":"dev-build"`) || !strings.Contains(got, `"go":{"runs":true`) {
		t.Errorf("snapshot-build-stage-plan-json = %s", got)
	}

	// The snapshot flow builds no containers — there is no containers target in
	// the publish plan; it is promoted to :dev on the release path by the
	// build-once/promote-many ladder.
	if got := env.Output("snapshot-publish-stage-plan-json"); strings.Contains(got, `"containers"`) {
		t.Errorf("snapshot-publish-stage-plan-json = %s", got)
	}
}

func TestPRCmd_EmitsPlanAndQualityStagePlan(t *testing.T) {
	env := ghaenv.Setup(t)
	env.Setenv("PROJECT_TYPE", "go")
	env.Setenv("LINTER_NANOLINTER", "true")

	cmd := plancmd.New()
	if err := cmd.Run(context.Background(), []string{"plan", "pr"}); err != nil {
		t.Fatal(err)
	}

	if got := env.Output("pr-plan-json"); !strings.Contains(got, `"project_type":"go"`) || !strings.Contains(got, `"nanolinter":true`) {
		t.Errorf("pr-plan-json = %s", got)
	}

	if got := env.Output("quality-stage-plan-json"); !strings.Contains(got, `"stage":"pr-quality"`) || !strings.Contains(got, `"nanolinter":{"runs":true`) {
		t.Errorf("quality-stage-plan-json = %s", got)
	}
}

func TestGetFilePatternCmd_EnvMode(t *testing.T) {
	tests := []struct {
		name            string
		projectType     string
		explicitPattern string
		want            string
	}{
		{name: "explicit_pattern", projectType: "maven", explicitPattern: "pom.xml package.json", want: "pom.xml package.json"},
		{name: "project_type_fallback", projectType: "npm", want: "CHANGELOG.md package.json package-lock.json"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			env := ghaenv.Setup(t)
			env.Setenv("PROJECT_TYPE", testCase.projectType)
			env.Setenv("EXPLICIT_FILE_PATTERN", testCase.explicitPattern)

			cmd := plancmd.New()
			if err := cmd.Run(context.Background(), []string{"plan", "file-pattern"}); err != nil {
				t.Fatal(err)
			}

			if got := env.Output("pattern"); got != testCase.want {
				t.Errorf("pattern = %q, want %q", got, testCase.want)
			}
		})
	}
}

func mustConfigPlanJSON(t *testing.T, plan pipeline.ConfigPlan) string {
	t.Helper()

	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}
