// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package plan_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cli"
	plancmd "github.com/diggsweden/reusable-ci/v3/internal/cli/commands/plan"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/ghaenv"
)

func TestReleaseCmd_UsesConfigPlanAndEmitsTypedPlans(t *testing.T) {
	env := ghaenv.Setup(t)

	cfg := &config.Config{
		Sign: config.SignConfig{Method: domainrelease.SignMethodSigstore},
		Artifacts: []config.Artifact{
			{Name: "web", ProjectType: projecttype.NPM, PublishTo: []config.PublishTarget{config.PublishForgePackages}},
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

	// Derived like every real config plan: the plan contract requires the
	// defaults Derive fills in (sboms among them), and this fixture skipped it.
	cfg := &config.Config{
		Artifacts: []config.Artifact{
			{Name: "web", ProjectType: projecttype.NPM},
			{Name: "worker", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeArtifactFirst}},
		},
		Containers: []config.Container{{Name: "image"}},
	}
	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}

	env.Setenv("CONFIG_PLAN_JSON", mustConfigPlanJSON(t, pipeline.NewConfigPlan(cfg)))
	env.Setenv("BRANCH", "feature/dev-plan")
	env.Setenv("PUBLISH_NPM", "true")
	env.Setenv("USE_CI_TOKEN", "true")
	env.Setenv("WORKING_DIRECTORY", "svc/api")

	cmd := plancmd.New()
	if err := cmd.Run(context.Background(), []string{"plan", "snapshot-release"}); err != nil {
		t.Fatal(err)
	}

	if got := env.Output("snapshot-release-plan-json"); !strings.Contains(got, `"branch":"feature/dev-plan"`) {
		t.Errorf("snapshot-release-plan-json = %s", got)
	}

	// Regression: the --working-dir flag was declared but read under a
	// different name, so the plan silently recorded ".".
	if got := env.Output("snapshot-release-plan-json"); !strings.Contains(got, `"working_directory":"svc/api"`) {
		t.Errorf("working-dir flag not wired into the plan: %s", got)
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
	env.Setenv("LINT_ENGINE", "nanolinter")

	cmd := plancmd.New()
	if err := cmd.Run(context.Background(), []string{"plan", "pr"}); err != nil {
		t.Fatal(err)
	}

	if got := env.Output("pr-plan-json"); !strings.Contains(got, `"project_type":"go"`) || !strings.Contains(got, `"engine":"nanolinter"`) {
		t.Errorf("pr-plan-json = %s", got)
	}

	if got := env.Output("quality-stage-plan-json"); !strings.Contains(got, `"stage":"pr-quality"`) || !strings.Contains(got, `"nanolinter":{"runs":true`) {
		t.Errorf("quality-stage-plan-json = %s", got)
	}
}

func TestGitlabPipelineCmds_RejectStalePlanVersion(t *testing.T) {
	env := ghaenv.Setup(t)
	env.Setenv("BUILD_STAGE_PLAN_JSON", `{"version":0,"stage":"build","targets":{}}`)
	env.Setenv("PUBLISH_STAGE_PLAN_JSON", `{"version":0,"stage":"publish","targets":{}}`)

	for _, tc := range []struct {
		verb string
		want string
	}{
		{verb: "gitlab-build-pipeline", want: "unsupported build-stage plan version"},
		{verb: "gitlab-publish-pipeline", want: "unsupported publish-stage plan version"},
	} {
		err := plancmd.New().Run(context.Background(), []string{
			"plan", tc.verb,
			"--component-base", "catalog.example/group/components",
			"--component-ref", "1.0.0",
		})
		// A plan from a different build of this tool is skew, not operator
		// misuse: ErrInvalidConfig, so the exit code points at the pipeline
		// rather than at the command line.
		if !errors.Is(err, errs.ErrInvalidConfig) {
			t.Errorf("%s: err = %v, want ErrInvalidConfig", tc.verb, err)

			continue
		}

		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to name the stale version", tc.verb, err)
		}
	}
}

func TestGitlabPipelineCmds_RequireExplicitComponentCoordinate(t *testing.T) {
	env := ghaenv.Setup(t)
	env.Setenv("BUILD_STAGE_PLAN_JSON", `{"version":1,"stage":"build","targets":{}}`)
	env.Setenv("PUBLISH_STAGE_PLAN_JSON", `{"version":1,"stage":"publish","targets":{}}`)

	for _, tc := range []struct {
		verb string
		args []string
		want string
	}{
		{verb: "gitlab-build-pipeline", args: []string{"--component-ref", "1.0.0"}, want: `Required flag "component-base" not set`},
		{verb: "gitlab-publish-pipeline", args: []string{"--component-ref", "1.0.0"}, want: `Required flag "component-base" not set`},
		{verb: "gitlab-build-pipeline", args: []string{"--component-base", "catalog.example/components"}, want: `Required flag "component-ref" not set`},
		{verb: "gitlab-publish-pipeline", args: []string{"--component-base", "catalog.example/components"}, want: `Required flag "component-ref" not set`},
	} {
		err := plancmd.New().Run(context.Background(), append([]string{"plan", tc.verb}, tc.args...))
		if err == nil {
			t.Fatalf("%s accepted an implicit component boundary", tc.verb)
		}

		// --component-base is Required, so this is urfave's own refusal; it
		// carries no sentinel until main.go classifies it, which is the exit
		// code that actually reaches the operator.
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.verb, err, tc.want)
		}

		if got := errs.ExitCodeFromError(cli.ClassifyError(err)); got != errs.ExitCodeUsage {
			t.Errorf("%s: exit code = %d, want usage (%d)", tc.verb, got, errs.ExitCodeUsage)
		}
	}
}

func TestGitlabPipelineCmds_RejectUnknownAndTrailingContractJSON(t *testing.T) {
	env := ghaenv.Setup(t)

	for _, tc := range []struct {
		name string
		verb string
		env  string
		raw  string
	}{
		{name: "build unknown", verb: "gitlab-build-pipeline", env: "BUILD_STAGE_PLAN_JSON", raw: `{"version":1,"stage":"build","future_semantic":true,"targets":{}}`},
		{name: "build trailing", verb: "gitlab-build-pipeline", env: "BUILD_STAGE_PLAN_JSON", raw: `{"version":1,"stage":"build","targets":{}} {}`},
		{name: "publish unknown", verb: "gitlab-publish-pipeline", env: "PUBLISH_STAGE_PLAN_JSON", raw: `{"version":1,"stage":"publish","targets":{},"future_semantic":true}`},
		{name: "publish trailing", verb: "gitlab-publish-pipeline", env: "PUBLISH_STAGE_PLAN_JSON", raw: `{"version":1,"stage":"publish","targets":{}} {}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env.Setenv(tc.env, tc.raw)

			out := filepath.Join(t.TempDir(), "child.yml")

			err := plancmd.New().Run(context.Background(), []string{
				"plan", tc.verb,
				"--component-base", "catalog.example/components",
				"--component-ref", "1.0.0",
				"--output", out,
			})
			if !errors.Is(err, errs.ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}

			if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
				t.Fatalf("invalid contract mutated output: %v", statErr)
			}
		})
	}
}

func TestGitlabPipelineCmd_DoesNotWriteUnsupportedPlan(t *testing.T) {
	env := ghaenv.Setup(t)
	env.Setenv("BUILD_STAGE_PLAN_JSON", `{"version":1,"stage":"build","targets":{"xcode_ios":{"runs":true,"items":[{"name":"ios"}]}}}`)

	out := filepath.Join(t.TempDir(), "child.yml")

	err := plancmd.New().Run(context.Background(), []string{
		"plan", "gitlab-build-pipeline",
		"--component-base", "catalog.example/group/components",
		"--component-ref", "1.0.0",
		"--output", out,
	})
	// An unsupported target in an otherwise well-formed plan is configuration
	// the generator cannot honour, not a bad flag.
	if !errors.Is(err, errs.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}

	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("unsupported plan mutated output: %v", statErr)
	}
}

func TestGitlabPipelineCmds_DoNotWriteZeroJobPipeline(t *testing.T) {
	env := ghaenv.Setup(t)

	for _, tc := range []struct {
		verb    string
		envName string
		stage   string
	}{
		{verb: "gitlab-build-pipeline", envName: "BUILD_STAGE_PLAN_JSON", stage: "build"},
		{verb: "gitlab-publish-pipeline", envName: "PUBLISH_STAGE_PLAN_JSON", stage: "publish"},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			env.Setenv(tc.envName, `{"version":1,"stage":"`+tc.stage+`","targets":{}}`)

			out := filepath.Join(t.TempDir(), "child.yml")

			err := plancmd.New().Run(context.Background(), []string{
				"plan", tc.verb,
				"--component-base", "catalog.example/components",
				"--component-ref", "1.0.0",
				"--output", out,
			})
			if !errors.Is(err, errs.ErrInvalidConfig) || !strings.Contains(err.Error(), "no generated jobs") {
				t.Fatalf("err = %v, want ErrInvalidConfig naming the zero-job pipeline", err)
			}

			if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
				t.Fatalf("zero-job plan mutated output: %v", statErr)
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
