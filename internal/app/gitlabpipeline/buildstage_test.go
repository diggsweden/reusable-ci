// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlabpipeline_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gitlabpipeline "github.com/diggsweden/reusable-ci/v3/internal/app/gitlabpipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
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
			Maven:  runningTarget(pipeline.PlannedArtifact{Name: "lib-core", ProjectType: projecttype.Maven, WorkingDirectory: "core", BuildType: config.BuildTypeLibrary}),
			NPM:    runningTarget(pipeline.PlannedArtifact{Name: "web", ProjectType: projecttype.NPM, WorkingDirectory: "ui"}),
			Gradle: runningTarget(pipeline.PlannedArtifact{Name: "jvm", ProjectType: projecttype.Gradle, WorkingDirectory: "svc"}),
			Go: runningTarget(pipeline.PlannedArtifact{
				Name: "cli", ProjectType: projecttype.Go, WorkingDirectory: ".",
				Go: &config.GoConfig{SkipTests: true}, EffectiveSBOMs: []config.SBOMLayer{config.SBOMLayerBuild},
			}),
			Cargo: runningTarget(pipeline.PlannedArtifact{Name: "rustcli", ProjectType: projecttype.Cargo, WorkingDirectory: "rs"}),
		},
	}

	child, err := gitlabpipeline.BuildStagePipeline(plan, gitlabpipeline.BuildPipelineOptions{
		ComponentBase: "$CI_SERVER_FQDN/diggsweden/reusable-ci",
		ComponentRef:  "1.0.0",
		Version:       "2.3.4",
	})
	if err != nil {
		t.Fatal(err)
	}

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

	if maven.Inputs["job-name"] != "build-maven-lib-core" || maven.Inputs["working-directory"] != "core" {
		t.Errorf("maven inputs = %v", maven.Inputs)
	}

	// These inputs are not in the Maven component contract; the unique job-name
	// carries item identity while the prepared pom carries the version.
	if _, ok := maven.Inputs["artifact-name"]; ok {
		t.Errorf("maven inputs contain undeclared artifact-name: %v", maven.Inputs)
	}

	if _, ok := maven.Inputs["version"]; ok {
		t.Errorf("maven inputs contain undeclared version: %v", maven.Inputs)
	}

	// Go include: working-directory "." preserved, no build-type.
	gobuild := byComponent[base+"/build-go@1.0.0"]
	if gobuild.Inputs["job-name"] != "build-go-cli" || gobuild.Inputs["working-directory"] != "." ||
		gobuild.Inputs["artifact-name"] != "cli" || gobuild.Inputs["version"] != "2.3.4" {
		t.Errorf("go inputs = %v", gobuild.Inputs)
	}

	if _, ok := gobuild.Inputs["build-type"]; ok {
		t.Errorf("go should not carry build-type: %v", gobuild.Inputs)
	}

	if got, ok := gobuild.Inputs["enable-build-sbom"].(bool); !ok || !got {
		t.Errorf("go enable-build-sbom = %#v, want boolean true", gobuild.Inputs["enable-build-sbom"])
	}

	if got, ok := gobuild.Inputs["skip-tests"].(bool); !ok || !got {
		t.Errorf("go skip-tests = %#v, want boolean true", gobuild.Inputs["skip-tests"])
	}

	if got := gobuild.Inputs["platforms"]; got != "linux/amd64,linux/arm64,darwin/amd64,darwin/arm64" {
		t.Errorf("go platforms = %q, want the release-build-stage default", got)
	}

	if got := byComponent[base+"/build-cargo@1.0.0"].Inputs["platforms"]; got != "linux/amd64" {
		t.Errorf("cargo platforms = %q, want the release-build-stage default", got)
	}
}

func TestBuildStagePipeline_MavenBuildTypeDefaultFromYAML(t *testing.T) {
	t.Parallel()

	cfg, err := config.Parse([]byte(`artifacts:
  - name: omitted
    project-type: maven
  - name: application
    project-type: maven
    build-type: application
  - name: library
    project-type: maven
    build-type: library
`))
	require.NoError(t, err)
	require.NoError(t, config.Validate(cfg))
	require.NoError(t, config.Derive(cfg))
	plan := pipeline.NewConfigPlan(cfg)
	child, err := gitlabpipeline.BuildStagePipeline(pipeline.ReleaseBuildStagePlan{
		Stage: "build",
		Targets: pipeline.ReleaseBuildTargets{
			Maven: runningTarget(plan.Artifacts.Maven...),
		},
	}, gitlabpipeline.BuildPipelineOptions{
		ComponentBase: "$CI_SERVER_FQDN/diggsweden/reusable-ci",
		ComponentRef:  "1.0.0",
	})
	require.NoError(t, err)
	require.Len(t, child.Include, 3)

	for i, want := range []struct{ name, buildType string }{
		{"omitted", "app"}, {"application", "app"}, {"library", "lib"},
	} {
		require.Equal(t, "$CI_SERVER_FQDN/diggsweden/reusable-ci/build-maven@1.0.0", child.Include[i].Component)
		require.Equal(t, "build-maven-"+want.name, child.Include[i].Inputs["job-name"])
		require.Equal(t, want.buildType, child.Include[i].Inputs["build-type"])
	}

	require.Empty(t, plan.Artifacts.Maven[0].BuildType, "build defaults must not normalize the plan's omitted field")
}

// TestBuildStagePipeline_RefusesUnrepresentableBuildSettings covers every
// setting the Catalog's build-go and build-cargo components cannot express.
//
// One row per setting, because each is a separate guard and the refusal is the
// only thing standing between a caller and a pipeline that silently drops the
// setting: a plan carrying ldflags would otherwise generate a build with no
// ldflags, and nothing would say so. Only MainPackage was exercised before, so
// deleting any of the other three refusals broke no test.
func TestBuildStagePipeline_RefusesUnrepresentableBuildSettings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		targets pipeline.ReleaseBuildTargets
	}{
		{
			name: "go main-package",
			targets: pipeline.ReleaseBuildTargets{
				Go: runningTarget(pipeline.PlannedArtifact{Name: "cli", ProjectType: projecttype.Go, Go: &config.GoConfig{MainPackage: "./cmd/cli"}}),
			},
		},
		{
			name: "go binary-name distinct from the artifact name",
			targets: pipeline.ReleaseBuildTargets{
				Go: runningTarget(pipeline.PlannedArtifact{Name: "cli", ProjectType: projecttype.Go, Go: &config.GoConfig{BinaryName: "other"}}),
			},
		},
		{
			name: "go build-tags",
			targets: pipeline.ReleaseBuildTargets{
				Go: runningTarget(pipeline.PlannedArtifact{Name: "cli", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildTags: "netgo"}}),
			},
		},
		{
			name: "go ldflags",
			targets: pipeline.ReleaseBuildTargets{
				Go: runningTarget(pipeline.PlannedArtifact{Name: "cli", ProjectType: projecttype.Go, Go: &config.GoConfig{LDFlags: "-s -w"}}),
			},
		},
		{
			name: "cargo binary-name distinct from the artifact name",
			targets: pipeline.ReleaseBuildTargets{
				Cargo: runningTarget(pipeline.PlannedArtifact{Name: "cli", ProjectType: projecttype.Cargo, Cargo: &config.CargoConfig{BinaryName: "other"}}),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := gitlabpipeline.BuildStagePipeline(
				pipeline.ReleaseBuildStagePlan{Targets: tc.targets},
				gitlabpipeline.BuildPipelineOptions{
					ComponentBase: "$CI_SERVER_FQDN/diggsweden/reusable-ci",
					ComponentRef:  "1.0.0",
				})
			// ErrUnsupported, not a usage error: the config is valid, and it is
			// the GitLab component that cannot express it — a caller can tell
			// "fix your config" from "this backend cannot do that" only by the
			// sentinel.
			if !errors.Is(err, errs.ErrUnsupported) {
				t.Fatalf("err = %v, want ErrUnsupported for a setting the component cannot represent", err)
			}
		})
	}
}

func TestBuildStagePipeline_RejectsZeroJobChildPipeline(t *testing.T) {
	t.Parallel()

	plan := pipeline.ReleaseBuildStagePlan{Stage: "build"} // nothing runs

	_, err := gitlabpipeline.BuildStagePipeline(plan, gitlabpipeline.BuildPipelineOptions{
		ComponentBase: "$CI_SERVER_FQDN/diggsweden/reusable-ci",
		ComponentRef:  "1.0.0",
	})
	if !errors.Is(err, errs.ErrInvalidConfig) || !strings.Contains(err.Error(), "no generated jobs") {
		t.Fatalf("err = %v, want ErrInvalidConfig naming the zero-job pipeline", err)
	}
}

func TestBuildStagePipeline_SanitisesJobNames(t *testing.T) {
	t.Parallel()

	plan := pipeline.ReleaseBuildStagePlan{
		Stage:   "build",
		Targets: pipeline.ReleaseBuildTargets{NPM: runningTarget(pipeline.PlannedArtifact{Name: "@scope/My_Pkg", ProjectType: projecttype.NPM})},
	}

	child, err := gitlabpipeline.BuildStagePipeline(plan, gitlabpipeline.BuildPipelineOptions{
		ComponentBase: "$CI_SERVER_FQDN/diggsweden/reusable-ci",
		ComponentRef:  "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := child.Include[0].Inputs["job-name"]; got != "build-npm-scope-my-pkg" {
		t.Errorf("job-name = %q, want build-npm-scope-my-pkg", got)
	}
}

func TestBuildStagePipeline_RejectsSanitisedJobNameCollision(t *testing.T) {
	t.Parallel()

	plan := pipeline.ReleaseBuildStagePlan{Targets: pipeline.ReleaseBuildTargets{
		NPM: runningTarget(
			pipeline.PlannedArtifact{Name: "scope/pkg", ProjectType: projecttype.NPM},
			pipeline.PlannedArtifact{Name: "scope-pkg", ProjectType: projecttype.NPM},
		),
	}}

	_, err := gitlabpipeline.BuildStagePipeline(plan, gitlabpipeline.BuildPipelineOptions{
		ComponentBase: "catalog.example/components",
		ComponentRef:  "1.0.0",
	})
	if !errors.Is(err, errs.ErrInvalidConfig) || !strings.Contains(err.Error(), "duplicate job name") {
		t.Fatalf("err = %v, want ErrInvalidConfig naming the sanitized collision", err)
	}
}

func TestChildPipeline_MarshalRejectsZeroJobs(t *testing.T) {
	t.Parallel()

	_, err := (gitlabpipeline.ChildPipeline{Stages: []string{"build"}}).Marshal()
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestBuildStagePipeline_RefusesMissingComponent(t *testing.T) {
	t.Parallel()

	plan := pipeline.ReleaseBuildStagePlan{
		Targets: pipeline.ReleaseBuildTargets{XcodeIOS: runningTarget(pipeline.PlannedArtifact{Name: "ios"})},
	}

	_, err := gitlabpipeline.BuildStagePipeline(plan, gitlabpipeline.BuildPipelineOptions{
		ComponentBase: "$CI_SERVER_FQDN/diggsweden/reusable-ci",
		ComponentRef:  "1.0.0",
	})
	if !errors.Is(err, errs.ErrUnsupported) || !strings.Contains(err.Error(), "build-xcode-ios") {
		t.Fatalf("err = %v, want ErrUnsupported naming the absent component", err)
	}
}

func TestBuildStagePipeline_RequiresExplicitCatalogueCoordinate(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		opts gitlabpipeline.BuildPipelineOptions
		want string
	}{
		{name: "catalogue coordinate", opts: gitlabpipeline.BuildPipelineOptions{ComponentRef: "1.0.0"}, want: "catalogue coordinate"},
		{name: "component ref", opts: gitlabpipeline.BuildPipelineOptions{ComponentBase: "catalog.example/components"}, want: "component ref"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := gitlabpipeline.BuildStagePipeline(pipeline.ReleaseBuildStagePlan{}, tc.opts)
			if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want ErrUsage naming the missing %s", err, tc.want)
			}
		})
	}
}

func TestBuildStagePipeline_RefusesWrongProjectType(t *testing.T) {
	t.Parallel()

	plan := pipeline.ReleaseBuildStagePlan{Targets: pipeline.ReleaseBuildTargets{
		Go: runningTarget(pipeline.PlannedArtifact{Name: "cli", ProjectType: projecttype.NPM}),
	}}

	_, err := gitlabpipeline.BuildStagePipeline(plan, gitlabpipeline.BuildPipelineOptions{
		ComponentBase: "catalog.example/components",
		ComponentRef:  "1.0.0",
	})
	if !errors.Is(err, errs.ErrInvalidConfig) || !strings.Contains(err.Error(), "build-go") {
		t.Fatalf("err = %v, want ErrInvalidConfig naming the mismatched component", err)
	}
}

func TestBuildStagePipeline_RefusesContainerFirstItemInBuildTarget(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		plan pipeline.ReleaseBuildStagePlan
	}{
		{
			name: "go",
			plan: pipeline.ReleaseBuildStagePlan{Targets: pipeline.ReleaseBuildTargets{
				Go: runningTarget(pipeline.PlannedArtifact{Name: "go", ProjectType: projecttype.Go, GoBuildMode: config.GoBuildModeContainerFirst}),
			}},
		},
		{
			name: "cargo",
			plan: pipeline.ReleaseBuildStagePlan{Targets: pipeline.ReleaseBuildTargets{
				Cargo: runningTarget(pipeline.PlannedArtifact{Name: "cargo", ProjectType: projecttype.Cargo, Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}}),
			}},
		},
		{
			name: "go config build mode",
			plan: pipeline.ReleaseBuildStagePlan{Targets: pipeline.ReleaseBuildTargets{
				Go: runningTarget(pipeline.PlannedArtifact{Name: "go", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}}),
			}},
		},
		{
			name: "cargo artifact build mode",
			plan: pipeline.ReleaseBuildStagePlan{Targets: pipeline.ReleaseBuildTargets{
				Cargo: runningTarget(pipeline.PlannedArtifact{Name: "cargo", ProjectType: projecttype.Cargo, CargoBuildMode: config.CargoBuildModeContainerFirst}),
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := gitlabpipeline.BuildStagePipeline(tc.plan, gitlabpipeline.BuildPipelineOptions{
				ComponentBase: "catalog.example/components",
				ComponentRef:  "1.0.0",
			})
			if !errors.Is(err, errs.ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}
		})
	}
}
