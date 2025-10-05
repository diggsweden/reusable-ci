// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlabpipeline_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	gitlabpipeline "github.com/diggsweden/reusable-ci/v3/internal/app/gitlabpipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

const valuesBase = "catalog.example/components"

func valuesOptions() gitlabpipeline.BuildPipelineOptions {
	return gitlabpipeline.BuildPipelineOptions{ComponentBase: valuesBase, ComponentRef: "2.0.1", Version: "3.4.5", Stage: "compile"}
}

func boolPtr(value bool) *bool { return &value }

// multiItemBuildPlan has two items per ecosystem, each with settings that
// differ from its sibling and from the component defaults, explicit false
// values included.
func multiItemBuildPlan() pipeline.ReleaseBuildStagePlan {
	build := []config.SBOMLayer{config.SBOMLayerBuild}

	return pipeline.ReleaseBuildStagePlan{Version: 1, Stage: "build", Targets: pipeline.ReleaseBuildTargets{
		Maven: runningTarget(
			pipeline.PlannedArtifact{Name: "core-lib", ProjectType: projecttype.Maven, WorkingDirectory: "core", BuildType: config.BuildTypeLibrary, EffectiveSBOMs: build},
			pipeline.PlannedArtifact{Name: "svc", ProjectType: projecttype.Maven, WorkingDirectory: "svc", BuildType: config.BuildTypeApplication, Maven: &config.MavenConfig{MavenProfile: "ignored-for-apps", JavaVersion: "21"}},
			pipeline.PlannedArtifact{Name: "api-lib", ProjectType: projecttype.Maven, WorkingDirectory: "api", BuildType: config.BuildTypeLibrary, Maven: &config.MavenConfig{MavenProfile: "ossrh", JavaVersion: "17"}},
		),
		NPM: runningTarget(
			pipeline.PlannedArtifact{Name: "web", ProjectType: projecttype.NPM, WorkingDirectory: "ui", NPM: &config.NPMConfig{NodeVersion: "22"}},
			pipeline.PlannedArtifact{Name: "docs", ProjectType: projecttype.NPM, EffectiveSBOMs: build},
		),
		Gradle: runningTarget(
			pipeline.PlannedArtifact{Name: "jvm", ProjectType: projecttype.Gradle, WorkingDirectory: "jvm", Gradle: &config.GradleConfig{GradleTasks: "build publish", JavaVersion: "21"}},
			pipeline.PlannedArtifact{Name: "tools", ProjectType: projecttype.Gradle, WorkingDirectory: "tools"},
		),
		GradleAndroid: runningTarget(
			pipeline.PlannedArtifact{Name: "app", ProjectType: projecttype.GradleAndroid, WorkingDirectory: "android", GradleAndroid: &config.GradleAndroidConfig{
				BuildModule: "mobile", ProductFlavor: "prod", BuildTypes: "release", IncludeAAB: boolPtr(false), EnableAndroidSigning: boolPtr(true),
			}},
			pipeline.PlannedArtifact{Name: "wear", ProjectType: projecttype.GradleAndroid, WorkingDirectory: "wear", GradleAndroid: &config.GradleAndroidConfig{}},
		),
		Go: runningTarget(
			pipeline.PlannedArtifact{Name: "cli", ProjectType: projecttype.Go, Go: &config.GoConfig{Platforms: "linux/riscv64", SkipTests: false}},
			pipeline.PlannedArtifact{Name: "agent", ProjectType: projecttype.Go, WorkingDirectory: "agent", Go: &config.GoConfig{SkipTests: true}, EffectiveSBOMs: build},
		),
		Cargo: runningTarget(
			pipeline.PlannedArtifact{Name: "rs", ProjectType: projecttype.Cargo, WorkingDirectory: "rs", Cargo: &config.CargoConfig{Platforms: "linux/arm64", SkipTests: true}},
			pipeline.PlannedArtifact{Name: "rs2", ProjectType: projecttype.Cargo, WorkingDirectory: "rs2"},
		),
	}}
}

func include(component string, inputs map[string]any) gitlabpipeline.IncludeEntry {
	return gitlabpipeline.IncludeEntry{Component: valuesBase + "/" + component + "@2.0.1", Inputs: inputs}
}

// TestBuildStagePipeline_EveryItemCarriesItsOwnInputs compares the whole
// generated pipeline for two or three items per ecosystem. Includes follow the
// ecosystem order and then plan order, each item carries its own values rather
// than its sibling's, explicit false values survive, and Maven follows
// release-build-stage.yml: a library activates its own profile or
// central-release even without a maven block, and an application activates
// none. A library without a maven block used to get no profile, and an
// application used to activate its maven-profile.
func TestBuildStagePipeline_EveryItemCarriesItsOwnInputs(t *testing.T) {
	t.Parallel()

	child, err := gitlabpipeline.BuildStagePipeline(multiItemBuildPlan(), valuesOptions())
	require.NoError(t, err)

	want := gitlabpipeline.ChildPipeline{Stages: []string{"compile"}, Include: []gitlabpipeline.IncludeEntry{
		include("build-maven", map[string]any{"job-name": "build-maven-core-lib", "stage": "compile", "working-directory": "core", "enable-build-sbom": true, "build-type": "lib", "profile": "central-release"}),
		include("build-maven", map[string]any{"job-name": "build-maven-svc", "stage": "compile", "working-directory": "svc", "enable-build-sbom": false, "build-type": "app", "java-version": "21"}),
		include("build-maven", map[string]any{"job-name": "build-maven-api-lib", "stage": "compile", "working-directory": "api", "enable-build-sbom": false, "build-type": "lib", "profile": "ossrh", "java-version": "17"}),
		include("build-npm", map[string]any{"job-name": "build-npm-web", "stage": "compile", "working-directory": "ui", "enable-build-sbom": false, "node-version": "22"}),
		include("build-npm", map[string]any{"job-name": "build-npm-docs", "stage": "compile", "working-directory": ".", "enable-build-sbom": true}),
		include("build-gradle", map[string]any{"job-name": "build-gradle-jvm", "stage": "compile", "working-directory": "jvm", "enable-build-sbom": false, "gradle-tasks": "build publish", "java-version": "21"}),
		include("build-gradle", map[string]any{"job-name": "build-gradle-tools", "stage": "compile", "working-directory": "tools", "enable-build-sbom": false}),
		include("build-gradle-android", map[string]any{
			"job-name": "build-gradle-android-app", "stage": "compile", "working-directory": "android", "enable-build-sbom": false,
			"build-module": "mobile", "product-flavor": "prod", "build-types": "release", "include-aab": false, "enable-signing": true,
		}),
		include("build-gradle-android", map[string]any{"job-name": "build-gradle-android-wear", "stage": "compile", "working-directory": "wear", "enable-build-sbom": false, "include-aab": true, "enable-signing": false}),
		include("build-go", map[string]any{"job-name": "build-go-cli", "stage": "compile", "working-directory": ".", "enable-build-sbom": false, "artifact-name": "cli", "version": "3.4.5", "platforms": "linux/riscv64", "skip-tests": false}),
		include("build-go", map[string]any{"job-name": "build-go-agent", "stage": "compile", "working-directory": "agent", "enable-build-sbom": true, "artifact-name": "agent", "version": "3.4.5", "platforms": "linux/amd64,linux/arm64,darwin/amd64,darwin/arm64", "skip-tests": true}),
		include("build-cargo", map[string]any{"job-name": "build-cargo-rs", "stage": "compile", "working-directory": "rs", "enable-build-sbom": false, "artifact-name": "rs", "version": "3.4.5", "platforms": "linux/arm64", "skip-tests": true}),
		include("build-cargo", map[string]any{"job-name": "build-cargo-rs2", "stage": "compile", "working-directory": "rs2", "enable-build-sbom": false, "artifact-name": "rs2", "version": "3.4.5", "platforms": "linux/amd64"}),
	}}

	require.Equal(t, want, child)

	for _, entry := range child.Include {
		assertCatalogueContract(t, entry)
	}
}

// TestChildPipeline_MarshalRoundTripKeepsTypesAndStructure marshals a pipeline
// whose names and directories carry YAML syntax. The bytes are deterministic,
// decode to exactly the stages and includes that were marshalled, with native
// booleans, and no value adds a key or a document.
func TestChildPipeline_MarshalRoundTripKeepsTypesAndStructure(t *testing.T) {
	t.Parallel()

	plan := multiItemBuildPlan()
	plan.Targets.NPM.Items[0].WorkingDirectory = "ui\nextra: injected\n# comment"
	plan.Targets.Go.Items[1].Name = "agent: {skip-tests: false} --- &anchor"

	child, err := gitlabpipeline.BuildStagePipeline(plan, valuesOptions())
	require.NoError(t, err)

	body, err := child.Marshal()
	require.NoError(t, err)

	again, err := child.Marshal()
	require.NoError(t, err)
	require.Equal(t, body, again)

	var decoded struct {
		Stages  []string `yaml:"stages"`
		Include []struct {
			Component string         `yaml:"component"`
			Inputs    map[string]any `yaml:"inputs"`
		} `yaml:"include"`
	}

	decoder := yaml.NewDecoder(bytes.NewReader(body))
	decoder.KnownFields(true)
	require.NoError(t, decoder.Decode(&decoded))

	var trailing any
	require.Error(t, decoder.Decode(&trailing), "the pipeline must be one YAML document")

	require.Equal(t, child.Stages, decoded.Stages)
	require.Len(t, decoded.Include, len(child.Include))

	for i, entry := range child.Include {
		require.Equal(t, entry.Component, decoded.Include[i].Component)
		require.Equal(t, entry.Inputs, decoded.Include[i].Inputs, "include %d", i)
	}

	require.Equal(t, "ui\nextra: injected\n# comment", decoded.Include[3].Inputs["working-directory"])
	require.Equal(t, "agent: {skip-tests: false} --- &anchor", decoded.Include[10].Inputs["artifact-name"])
	require.IsType(t, true, decoded.Include[10].Inputs["skip-tests"])
}

// TestPipelines_RefusalsReturnNoPipeline covers the inconsistent and
// unrepresentable plans each generator refuses, each otherwise valid: a target
// whose runs flag and items disagree, a bad item after a good one, container
// work that has no component, App Store settings the component cannot express,
// and a component coordinate with whitespace. Every refusal has its class and
// returns no pipeline.
func TestPipelines_RefusalsReturnNoPipeline(t *testing.T) {
	t.Parallel()

	good := pipeline.PlannedArtifact{Name: "web", ProjectType: projecttype.NPM}
	app := func(xcode *config.XcodeIOSConfig) pipeline.TargetPlan[pipeline.PlannedArtifact] {
		return runningTarget(pipeline.PlannedArtifact{Name: "ios", ProjectType: projecttype.XcodeIOS, XcodeIOS: xcode})
	}

	build := func(mutate func(*pipeline.ReleaseBuildTargets), opts gitlabpipeline.BuildPipelineOptions) func() (gitlabpipeline.ChildPipeline, error) {
		targets := pipeline.ReleaseBuildTargets{NPM: runningTarget(good)}
		mutate(&targets)

		return func() (gitlabpipeline.ChildPipeline, error) {
			return gitlabpipeline.BuildStagePipeline(pipeline.ReleaseBuildStagePlan{Targets: targets}, opts)
		}
	}

	publish := func(targets pipeline.ReleasePublishTargets) func() (gitlabpipeline.ChildPipeline, error) {
		return func() (gitlabpipeline.ChildPipeline, error) {
			return gitlabpipeline.PublishStagePipeline(pipeline.ReleasePublishStagePlan{Targets: targets}, valuesOptions())
		}
	}

	none := func(*pipeline.ReleaseBuildTargets) {}

	for _, tc := range []struct {
		name string
		run  func() (gitlabpipeline.ChildPipeline, error)
		want error
	}{
		{"build runs without items", build(func(t *pipeline.ReleaseBuildTargets) {
			t.Maven = pipeline.TargetPlan[pipeline.PlannedArtifact]{Runs: true}
		}, valuesOptions()), errs.ErrInvalidConfig},
		{"build items without runs", build(func(t *pipeline.ReleaseBuildTargets) {
			t.Gradle.Items = []pipeline.PlannedArtifact{{Name: "jvm", ProjectType: projecttype.Gradle}}
		}, valuesOptions()), errs.ErrInvalidConfig},
		{"later item of another project type", build(func(t *pipeline.ReleaseBuildTargets) {
			t.NPM.Items = append(t.NPM.Items, pipeline.PlannedArtifact{Name: "jvm", ProjectType: projecttype.Gradle})
		}, valuesOptions()), errs.ErrInvalidConfig},
		{"later item with a blank name", build(func(t *pipeline.ReleaseBuildTargets) {
			t.NPM.Items = append(t.NPM.Items, pipeline.PlannedArtifact{Name: " \t", ProjectType: projecttype.NPM})
		}, valuesOptions()), errs.ErrInvalidConfig},
		{"later go item that is container-first", build(func(t *pipeline.ReleaseBuildTargets) {
			t.Go = runningTarget(pipeline.PlannedArtifact{Name: "a", ProjectType: projecttype.Go}, pipeline.PlannedArtifact{Name: "b", ProjectType: projecttype.Go, GoBuildMode: config.GoBuildModeContainerFirst})
		}, valuesOptions()), errs.ErrInvalidConfig},
		{"xcode build", build(func(t *pipeline.ReleaseBuildTargets) {
			t.XcodeIOS = runningTarget(pipeline.PlannedArtifact{Name: "ios", ProjectType: projecttype.XcodeIOS})
		}, valuesOptions()), errs.ErrUnsupported},
		{"padded component base", build(none, gitlabpipeline.BuildPipelineOptions{ComponentBase: " " + valuesBase, ComponentRef: "2.0.1"}), errs.ErrUsage},
		{"component ref with a tab", build(none, gitlabpipeline.BuildPipelineOptions{ComponentBase: valuesBase, ComponentRef: "2.0.1\t"}), errs.ErrUsage},
		{"publish runs without items", publish(pipeline.ReleasePublishTargets{MavenCentral: pipeline.TargetPlan[pipeline.PlannedArtifact]{Runs: true}}), errs.ErrInvalidConfig},
		{"publish containers", publish(pipeline.ReleasePublishTargets{Containers: pipeline.TargetPlan[pipeline.PlannedContainer]{Runs: true, Items: []pipeline.PlannedContainer{{Name: "img"}}}}), errs.ErrUnsupported},
		{"publish cargo container-first", publish(pipeline.ReleasePublishTargets{CargoContainerFirst: runningTarget(pipeline.PlannedArtifact{Name: "rs", ProjectType: projecttype.Cargo})}), errs.ErrUnsupported},
		{"publish go container-first", publish(pipeline.ReleasePublishTargets{GoContainerFirst: runningTarget(pipeline.PlannedArtifact{Name: "cli", ProjectType: projecttype.Go})}), errs.ErrUnsupported},
		{"publish unsigned app store build", publish(pipeline.ReleasePublishTargets{XcodeIOS: app(&config.XcodeIOSConfig{EnableCodeSigning: boolPtr(false)})}), errs.ErrUnsupported},
		{"publish app store review submission", publish(pipeline.ReleasePublishTargets{XcodeIOS: app(&config.XcodeIOSConfig{SubmitForReview: true})}), errs.ErrUnsupported},
		{"publish padded component ref", func() (gitlabpipeline.ChildPipeline, error) {
			return gitlabpipeline.PublishStagePipeline(pipeline.ReleasePublishStagePlan{}, gitlabpipeline.BuildPipelineOptions{ComponentBase: valuesBase, ComponentRef: " 2.0.1"})
		}, errs.ErrUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			child, err := tc.run()
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}

			require.Zero(t, child)
		})
	}
}
