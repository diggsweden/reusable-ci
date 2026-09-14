// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/stretchr/testify/require"
)

func TestConfigPlanExecution_ArtifactProjections(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, reason string
		mutate       func(*pipeline.PlannedArtifact)
	}{
		{"invalid_sboms", "sboms", func(a *pipeline.PlannedArtifact) { a.SBOMs = "build,unknown" }},
		{"missing_sboms", "sboms", func(a *pipeline.PlannedArtifact) { a.SBOMs = "" }},
		{"suppressed_layers", "effective_sboms", func(a *pipeline.PlannedArtifact) { a.EffectiveSBOMs = nil }},
		{"different_layers", "effective_sboms", func(a *pipeline.PlannedArtifact) { a.EffectiveSBOMs = []config.SBOMLayer{"build"} }},
		{"unknown_layer", "effective_sboms", func(a *pipeline.PlannedArtifact) { a.EffectiveSBOMs = []config.SBOMLayer{"unknown"} }},
		{"optout_with_layers", "effective_sboms", func(a *pipeline.PlannedArtifact) { a.SBOMs = "none" }},
		{"build_name", "build_artifact_name", func(a *pipeline.PlannedArtifact) { a.BuildArtifactName = "foreign-upload" }},
		{"missing_build_name", "build_artifact_name", func(a *pipeline.PlannedArtifact) { a.BuildArtifactName = "" }},
		{"sbom_name", "build_sbom_artifact_name", func(a *pipeline.PlannedArtifact) { a.BuildSBOMArtifactName = "foreign-sbom" }},
		{"missing_sbom_name", "build_sbom_artifact_name", func(a *pipeline.PlannedArtifact) { a.BuildSBOMArtifactName = "" }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			plan := invariantConfigPlan(t)
			// The earlier web artifact keeps the pipeline union at all. Every
			// copy of lib changes, including both publishing projections.
			mutateExecutionArtifact(t, &plan, "lib", testCase.mutate)
			assertInvariantRefusal(t, plan, "artifacts.all[2]."+testCase.reason)
		})
	}
}

func TestConfigPlanExecution_ContainerProjections(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, reason string
		mutate       func(*pipeline.PlannedContainer)
	}{
		{"late_unknown_from", "from[10]", func(c *pipeline.PlannedContainer) { c.From = append(c.From, "missing") }},
		{"types_missing", "artifact_types", func(c *pipeline.PlannedContainer) { c.ArtifactTypes = nil }},
		{"types_wrong", "artifact_types", func(c *pipeline.PlannedContainer) { c.ArtifactTypes = []projecttype.Type{projecttype.Meta} }},
		{"analyzed_gate", "enable_analyzed_container_sbom", func(c *pipeline.PlannedContainer) { c.EnableAnalyzedContainerSBOM = false }},
		{"go_upload_not_logical_name", "go_artifact_name", func(c *pipeline.PlannedContainer) { c.GoArtifactName = "go-cli-go-build-artifacts" }},
		{"go_container_first_not_downloaded", "go_artifact_name", func(c *pipeline.PlannedContainer) { c.GoArtifactName = "go-service" }},
		{"cargo_missing", "cargo_artifact_name", func(c *pipeline.PlannedContainer) { c.CargoArtifactName = "" }},
		{"cargo_container_first_not_downloaded", "cargo_artifact_name", func(c *pipeline.PlannedContainer) { c.CargoArtifactName = "cargo-service" }},
		{"maven_second_not_first", "maven_artifact_name", func(c *pipeline.PlannedContainer) { c.MavenArtifactName = "maven-a-build-artifacts" }},
		{"npm_second_not_first", "npm_artifact_name", func(c *pipeline.PlannedContainer) { c.NPMArtifactName = "npm-a-build-artifacts" }},
		{"gradle_second_not_first", "gradle_artifact_name", func(c *pipeline.PlannedContainer) { c.GradleArtifactName = "gradle-a-build-artifacts" }},
		{"multiple_go_artifact_first", "from has multiple artifact-first Go", func(c *pipeline.PlannedContainer) { c.From = append(c.From, "go-other") }},
		{"multiple_cargo_artifact_first", "from has multiple artifact-first Cargo", func(c *pipeline.PlannedContainer) { c.From = append(c.From, "cargo-other") }},
		{"repeated_go_dependency", "from has multiple artifact-first Go", func(c *pipeline.PlannedContainer) { c.From = append(c.From, "go-cli") }},
		{"repeated_cargo_dependency", "from has multiple artifact-first Cargo", func(c *pipeline.PlannedContainer) { c.From = append(c.From, "cargo-cli") }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			plan := executionContainerPlan(t)
			testCase.mutate(&plan.Containers.All[1])
			assertInvariantRefusal(t, plan, "containers.all[1]."+testCase.reason)
		})
	}

	for _, slot := range []struct{ field, wire string }{
		{"GoArtifactName", "go_artifact_name"}, {"CargoArtifactName", "cargo_artifact_name"},
		{"MavenArtifactName", "maven_artifact_name"}, {"NPMArtifactName", "npm_artifact_name"}, {"GradleArtifactName", "gradle_artifact_name"},
	} {
		t.Run("bare_container_"+slot.wire, func(t *testing.T) {
			t.Parallel()
			plan := executionContainerPlan(t)
			reflect.ValueOf(&plan.Containers.All[0]).Elem().FieldByName(slot.field).SetString("foreign")
			assertInvariantRefusal(t, plan, "containers.all[0]."+slot.wire)
		})
	}

	t.Run("bare_analyzed_gate", func(t *testing.T) {
		t.Parallel()
		plan := executionContainerPlan(t)
		plan.Containers.All[0].EnableAnalyzedContainerSBOM = true
		assertInvariantRefusal(t, plan, "containers.all[0].enable_analyzed_container_sbom")
	})
}

func TestConfigPlanExecution_ProducerControls(t *testing.T) {
	t.Parallel()
	plan := executionContainerPlan(t)
	container := plan.Containers.All[1]
	require.Equal(t, []config.SBOMLayer{"build", "analyzed-artifact", "analyzed-container"}, plan.Artifacts.All[0].EffectiveSBOMs)
	require.Equal(t, []projecttype.Type{"maven", "npm", "gradle", "go", "cargo"}, container.ArtifactTypes)
	require.True(t, container.EnableAnalyzedContainerSBOM)
	require.Equal(t, []string{"go-cli", "cargo-cli", "maven-b-build-artifacts", "npm-b-build-artifacts", "gradle-b-build-artifacts"}, []string{
		container.GoArtifactName, container.CargoArtifactName, container.MavenArtifactName, container.NPMArtifactName, container.GradleArtifactName,
	})
	// Set equivalence is intentional for SBOMs; source order is intentional
	// for the first-match container slots. Neither normalizes the input.
	mutateExecutionArtifact(t, &plan, "maven-b", func(a *pipeline.PlannedArtifact) {
		a.SBOMs = " analyzed-container, build, analyzed-artifact,build "
		a.EffectiveSBOMs = []config.SBOMLayer{"analyzed-artifact", "build", "analyzed-container", "build"}
	})
	plan.Containers.All[0].ArtifactTypes = []projecttype.Type{}
	plan.Containers.All[1].From = append(plan.Containers.All[1].From, "maven-b", "npm-b", "gradle-b")
	before, err := json.Marshal(plan)
	require.NoError(t, err)
	require.NoError(t, pipeline.ValidateConfigPlan(plan))
	release, err := pipeline.NewReleasePlan(pipeline.ReleasePlanInput{ConfigPlan: plan, ReleaseSBOMs: "all"})
	require.NoError(t, err)
	require.True(t, release.Stages.Publish.Targets.Containers.Runs)

	snapshot, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{ConfigPlan: plan, ProjectType: projecttype.Maven, SBOMs: "none"})
	require.NoError(t, err)
	require.False(t, snapshot.Stages.Publish.Targets.NPM.Runs)
	require.Len(t, snapshot.Stages.Publish.Targets.NPM.Items, 2)
	require.Empty(t, snapshot.ArtifactTransfers.Items)

	after, err := json.Marshal(plan)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestConfigPlanExecution_ConditionalNames(t *testing.T) {
	t.Parallel()

	no := false
	for _, testCase := range []struct {
		name       string
		artifact   config.Artifact
		build, bom string
	}{
		{"maven", config.Artifact{ProjectType: projecttype.Maven}, "app-build-artifacts", "app-build-sbom"},
		{"npm", config.Artifact{ProjectType: projecttype.NPM}, "app-build-artifacts", "app-build-sbom"},
		{"gradle", config.Artifact{ProjectType: projecttype.Gradle}, "app-build-artifacts", "app-build-sbom"},
		{"go_default", config.Artifact{ProjectType: projecttype.Go}, "app-go-build-artifacts", "app-go-build-sbom"},
		{"go_container", config.Artifact{ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}}, "", "app-go-build-sbom"},
		{"cargo_default", config.Artifact{ProjectType: projecttype.Cargo}, "app-cargo-build-artifacts", "app-cargo-build-sbom"},
		{"cargo_container", config.Artifact{ProjectType: projecttype.Cargo, Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}}, "", "app-cargo-build-sbom"},
		{"android_default", config.Artifact{ProjectType: projecttype.GradleAndroid}, "app", "app-sbom"},
		{"android_debug", config.Artifact{ProjectType: projecttype.GradleAndroid, GradleAndroid: &config.GradleAndroidConfig{BuildTypes: "debug"}}, "", "app-sbom"},
		{"android_no_aab", config.Artifact{ProjectType: projecttype.GradleAndroid, GradleAndroid: &config.GradleAndroidConfig{IncludeAAB: &no}}, "", "app-sbom"},
		{"ios_signed", config.Artifact{ProjectType: projecttype.XcodeIOS}, "app", ""},
		{"ios_archive", config.Artifact{ProjectType: projecttype.XcodeIOS, XcodeIOS: &config.XcodeIOSConfig{EnableCodeSigning: &no}}, "app-archive", ""},
		{"meta", config.Artifact{ProjectType: projecttype.Meta}, "", ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			artifact := testCase.artifact
			artifact.Name, artifact.SBOMs = "app", "none"
			plan := executionConfigPlan(t, &config.Config{Artifacts: []config.Artifact{artifact}})
			require.Equal(t, testCase.build, plan.Artifacts.All[0].BuildArtifactName)
			require.Equal(t, testCase.bom, plan.Artifacts.All[0].BuildSBOMArtifactName)
			require.Empty(t, plan.Artifacts.All[0].EffectiveSBOMs)
			require.NoError(t, pipeline.ValidateConfigPlan(plan))

			for _, field := range []struct{ name, wire string }{{"BuildArtifactName", "build_artifact_name"}, {"BuildSBOMArtifactName", "build_sbom_artifact_name"}} {
				t.Run(field.wire, func(t *testing.T) {
					bad := executionConfigPlan(t, &config.Config{Artifacts: []config.Artifact{artifact}})
					mutateExecutionArtifact(t, &bad, "app", func(a *pipeline.PlannedArtifact) {
						reflect.ValueOf(a).Elem().FieldByName(field.name).SetString("wrong")
					})
					assertInvariantRefusal(t, bad, "artifacts.all[0]."+field.wire)
				})
			}
		})
	}
}

func TestReleaseExecution_SelectedConcreteTransferCollisions(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, sboms, collision string
		artifacts              []config.Artifact
		containers             []config.Container
	}{
		{"cross_ecosystem_build", "analyzed-artifact", "api-go-build-artifacts", []config.Artifact{{Name: "api", ProjectType: projecttype.Go}, {Name: "api-go", ProjectType: projecttype.Maven}}, nil},
		{"cross_ecosystem_sbom", "build", "api-go-build-sbom", []config.Artifact{{Name: "api", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}}, {Name: "api-go", ProjectType: projecttype.Maven}}, nil},
		{"build_vs_sbom", "build,analyzed-artifact", "lib-build-sbom", []config.Artifact{{Name: "lib", ProjectType: projecttype.Maven}, {Name: "lib-build-sbom", ProjectType: projecttype.XcodeIOS}}, nil},
		{"extraction_vs_build", "analyzed-artifact", "image-binaries-amd64", []config.Artifact{{Name: "image-binaries-amd64", ProjectType: projecttype.XcodeIOS}}, []config.Container{{Name: "image", EnableSLSA: new(false), Extract: &config.ContainerExtract{Binary: &config.ContainerExtractBinary{Target: "export"}}}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Config{Artifacts: testCase.artifacts, Containers: testCase.containers}
			require.NoError(t, config.Validate(cfg))
			plan := executionConfigPlan(t, cfg)
			// Names remain canonical. Collision checks belong to the actual
			// selected transfer list, not to ConfigPlan's dormant fields.
			require.NoError(t, pipeline.ValidateConfigPlan(plan))
			before, err := json.Marshal(plan)
			require.NoError(t, err)
			release, err := pipeline.NewReleasePlan(pipeline.ReleasePlanInput{ConfigPlan: plan, ReleaseSBOMs: "all"})
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			require.ErrorContains(t, err, "artifact_transfers.items[")
			require.ErrorContains(t, err, "duplicate concrete name \""+testCase.collision+"\"")
			require.Equal(t, pipeline.ReleasePlan{}, release)

			snapshot, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{ConfigPlan: plan, ProjectType: projecttype.Maven, SBOMs: testCase.sboms})
			require.ErrorIs(t, err, errs.ErrInvalidConfig)
			require.ErrorContains(t, err, "duplicate concrete name \""+testCase.collision+"\"")
			require.Equal(t, pipeline.SnapshotReleasePlan{}, snapshot)
			snapshot, err = pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{ConfigPlan: plan, ProjectType: projecttype.Maven, SBOMs: "none"})
			require.NoError(t, err)
			require.Empty(t, snapshot.ArtifactTransfers.Items)

			after, err := json.Marshal(plan)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestReleaseExecution_TransferSelectionControls(t *testing.T) {
	t.Parallel()
	plan := executionConfigPlan(t, &config.Config{Artifacts: []config.Artifact{
		{Name: "api", ProjectType: projecttype.Go, SBOMs: "none", Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}},
		{Name: "api-go", ProjectType: projecttype.Maven},
	}})
	release, err := pipeline.NewReleasePlan(pipeline.ReleasePlanInput{ConfigPlan: plan, ReleaseSBOMs: "all"})
	require.NoError(t, err)
	require.Equal(t, []pipeline.ArtifactTransfer{
		{Kind: "build_artifact", Name: "api-go-build-artifacts", Path: "./release-artifacts/", Required: true},
		{Kind: "build_sbom", Name: "api-go-build-sbom", Path: "./release-artifacts/", Required: true},
	}, release.ArtifactTransfers.Items)

	snapshot, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{ConfigPlan: plan, ProjectType: projecttype.Maven, SBOMs: "build"})
	require.NoError(t, err)
	require.Equal(t, []pipeline.ArtifactTransfer{{Kind: "build_sbom", Name: "api-go-build-sbom", Path: "./release-artifacts/"}}, snapshot.ArtifactTransfers.Items)
}

func TestReleaseExecution_ReleaseCapExcludesDormantCollisions(t *testing.T) {
	t.Parallel()
	plan := executionConfigPlan(t, &config.Config{Artifacts: []config.Artifact{
		{Name: "api", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}},
		{Name: "api-go", ProjectType: projecttype.Maven},
	}})
	release, err := pipeline.NewReleasePlan(pipeline.ReleasePlanInput{ConfigPlan: plan, ReleaseSBOMs: "none"})
	require.NoError(t, err)
	require.Equal(t, []pipeline.ArtifactTransfer{{Kind: "build_artifact", Name: "api-go-build-artifacts", Path: "./release-artifacts/", Required: true}}, release.ArtifactTransfers.Items)

	snapshot, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{ConfigPlan: plan, ProjectType: projecttype.Maven, SBOMs: "analyzed-artifact"})
	require.NoError(t, err)
	require.Equal(t, []pipeline.ArtifactTransfer{{Kind: "build_artifact", Name: "api-go-build-artifacts", Path: "./release-artifacts/"}}, snapshot.ArtifactTransfers.Items)
}

func executionConfigPlan(t *testing.T, cfg *config.Config) pipeline.ConfigPlan {
	t.Helper()
	require.NoError(t, config.Derive(cfg))

	return pipeline.NewConfigPlan(cfg)
}

func executionContainerPlan(t *testing.T) pipeline.ConfigPlan {
	t.Helper()
	plan := executionConfigPlan(t, &config.Config{
		Artifacts: []config.Artifact{
			{Name: "maven-a", ProjectType: projecttype.Maven}, {Name: "maven-b", ProjectType: projecttype.Maven},
			{Name: "npm-a", ProjectType: projecttype.NPM}, {Name: "npm-b", ProjectType: projecttype.NPM},
			{Name: "gradle-a", ProjectType: projecttype.Gradle}, {Name: "gradle-b", ProjectType: projecttype.Gradle},
			{Name: "go-cli", ProjectType: projecttype.Go}, {Name: "go-other", ProjectType: projecttype.Go},
			{Name: "go-service", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}},
			{Name: "cargo-cli", ProjectType: projecttype.Cargo}, {Name: "cargo-other", ProjectType: projecttype.Cargo},
			{Name: "cargo-service", ProjectType: projecttype.Cargo, Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}},
		},
		Containers: []config.Container{
			{Name: "bare", EnableSLSA: new(false), EnableScan: new(false)},
			{Name: "mixed", From: []string{"cargo-service", "go-service", "npm-b", "maven-b", "gradle-b", "go-cli", "cargo-cli", "maven-a", "npm-a", "gradle-a"}, EnableSLSA: new(false)},
		},
	})
	require.NoError(t, pipeline.ValidateConfigPlan(plan))

	return plan
}

func mutateExecutionArtifact(t *testing.T, plan *pipeline.ConfigPlan, name string, mutate func(*pipeline.PlannedArtifact)) {
	t.Helper()

	groups := reflect.ValueOf(&plan.Artifacts).Elem()
	for index := range groups.NumField() {
		items, ok := groups.Field(index).Interface().([]pipeline.PlannedArtifact)
		require.True(t, ok, "artifact group fixture must be a list of copies")

		for item := range items {
			if items[item].Name == name {
				mutate(&items[item])
			}
		}
	}
}
