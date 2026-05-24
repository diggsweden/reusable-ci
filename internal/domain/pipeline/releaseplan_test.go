// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package pipeline_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

//nolint:cyclop // exercises many invariants on one ReleasePlan.
func TestNewReleasePlan_ComputesPolicyAndStagePlans(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Artifacts: []config.Artifact{
			{
				Name:                 "lib", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				ProjectType:          projecttype.Maven,
				BuildType:            config.BuildTypeLibrary,
				PublishTo:            []config.PublishTarget{config.PublishMavenCentral},
				RequireAuthorization: true,
			},
			{Name: "web", ProjectType: projecttype.NPM, PublishTo: []config.PublishTarget{config.PublishGitHubPackages}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			{Name: "rust-service", ProjectType: projecttype.Cargo, Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			{Name: "go-service", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Containers: []config.Container{{Name: "image"}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}
	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}

	plan, err := pipeline.NewReleasePlan(pipeline.ReleasePlanInput{
		ConfigPlan:           pipeline.NewConfigPlan(cfg),
		Branch:               "main", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		RefName:              "v1.2.3", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ReleasePublisher:     "github-cli",
		ReleaseSBOMs:         "build,analyzed-artifact",
		ReleaseSignArtifacts: true,
		ChangelogCreator:     "git-cliff",
	})
	if err != nil {
		t.Fatal(err)
	}

	if plan.Version != pipeline.ReleasePlanVersion {
		t.Errorf("version = %d", plan.Version)
	}

	if !plan.Policy.MakeLatest || !plan.Policy.CreateRelease || !plan.Policy.SignArtifacts {
		t.Errorf("policy = %+v", plan.Policy)
	}

	if !plan.Policy.RequireAllowlistedSigner || !plan.Policy.RunVersionBump || !plan.Policy.HasContainers {
		t.Errorf("policy = %+v", plan.Policy)
	}

	if plan.Policy.SBOMs != "build,analyzed-artifact" {
		t.Errorf("sboms = %q", plan.Policy.SBOMs)
	}

	if !plan.Stages.Prepare.Targets.VersionBump.Runs {
		t.Errorf("prepare = %+v", plan.Stages.Prepare)
	}

	if !plan.Stages.Build.Targets.Maven.Runs || plan.Stages.Build.Targets.Go.Runs {
		t.Errorf("build = %+v", plan.Stages.Build.Targets)
	}

	if !plan.Stages.Publish.Targets.MavenCentral.Runs || !plan.Stages.Publish.Targets.GitHubPackages.Runs {
		t.Errorf("publish package targets = %+v", plan.Stages.Publish.Targets)
	}

	if !plan.Stages.Publish.Targets.Containers.Runs || !plan.Stages.Publish.Targets.CargoContainerFirst.Runs || !plan.Stages.Publish.Targets.GoContainerFirst.Runs {
		t.Errorf("publish container-first targets = %+v", plan.Stages.Publish.Targets)
	}

	if len(plan.ArtifactTransfers.Items) == 0 {
		t.Fatal("artifact transfer plan is empty")
	}

	if !hasTransfer(plan.ArtifactTransfers.Items, "build_artifact", "lib-build-artifacts") {
		t.Errorf("missing maven build transfer: %+v", plan.ArtifactTransfers.Items)
	}
}

func TestNewReleasePlan_ComputesExplicitContainerTransfers(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Artifacts: []config.Artifact{{Name: "go-service", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}}},
		Containers: []config.Container{{
			Name:                        "api", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			From:                        []string{"go-service"},
			Platforms:                   "linux/amd64,linux/arm64", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			EnableAnalyzedContainerSBOM: true,
			Extract:                     &config.ContainerExtract{Binary: &config.ContainerExtractBinary{Target: "export"}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		}},
	}
	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}

	plan, err := pipeline.NewReleasePlan(pipeline.ReleasePlanInput{
		ConfigPlan:   pipeline.NewConfigPlan(cfg),
		RefName:      "v1.2.3",
		ReleaseSBOMs: "analyzed-container",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !hasTransferTemplate(plan.ArtifactTransfers.Items, "analyzed_container_sbom", "analyzed-container-sbom-{run_id}-api-amd64") {
		t.Errorf("missing analyzed container transfer: %+v", plan.ArtifactTransfers.Items)
	}

	if !hasTransfer(plan.ArtifactTransfers.Items, "extracted_binaries", "api-binaries-arm64") {
		t.Errorf("missing binary transfer: %+v", plan.ArtifactTransfers.Items)
	}
}

func TestNewReleasePlan_RecordsSBOMConflict(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Artifacts: []config.Artifact{{Name: "app", ProjectType: projecttype.NPM, SBOMs: "analyzed-container"}}} //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}

	plan, err := pipeline.NewReleasePlan(pipeline.ReleasePlanInput{
		ConfigPlan:   pipeline.NewConfigPlan(cfg),
		RefName:      "v1.2.3",
		ReleaseSBOMs: "build", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		t.Fatal(err)
	}

	if plan.Policy.SBOMs != "none" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("sboms = %q", plan.Policy.SBOMs)
	}

	if plan.Policy.SBOMConflict == nil || plan.Policy.SBOMConflict.ReleaseSBOMs != "build" {
		t.Errorf("sbom conflict = %+v", plan.Policy.SBOMConflict)
	}
}

func TestNewReleasePlan_DoesNotRunCargoOrGoSBOMTargetsWhenBuildSBOMNotEffective(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Artifacts: []config.Artifact{
		{Name: "rust-service", ProjectType: projecttype.Cargo, Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}},
		{Name: "go-service", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}},
	}}
	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}

	plan, err := pipeline.NewReleasePlan(pipeline.ReleasePlanInput{
		ConfigPlan:   pipeline.NewConfigPlan(cfg),
		RefName:      "v1.2.3",
		ReleaseSBOMs: "analyzed-artifact", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		t.Fatal(err)
	}

	if plan.Stages.Publish.Targets.CargoContainerFirst.Runs || plan.Stages.Publish.Targets.GoContainerFirst.Runs {
		t.Errorf("build SBOM targets should not run: %+v", plan.Stages.Publish.Targets)
	}
}

func TestNewReleasePlan_FiltersCargoAndGoSBOMTargetsByArtifactSBOMPolicy(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Artifacts: []config.Artifact{
		{Name: "rust-build", ProjectType: projecttype.Cargo, SBOMs: "build", Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}},
		{Name: "rust-none", ProjectType: projecttype.Cargo, SBOMs: "none", Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}},
		{Name: "go-build", ProjectType: projecttype.Go, SBOMs: "build", Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}},
		{Name: "go-none", ProjectType: projecttype.Go, SBOMs: "none", Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}},
	}}
	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}

	plan, err := pipeline.NewReleasePlan(pipeline.ReleasePlanInput{
		ConfigPlan:   pipeline.NewConfigPlan(cfg),
		RefName:      "v1.2.3",
		ReleaseSBOMs: "build",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := names(plan.Stages.Publish.Targets.CargoContainerFirst.Items); len(got) != 1 || got[0] != "rust-build" {
		t.Errorf("cargo items = %v", got)
	}

	if got := names(plan.Stages.Publish.Targets.GoContainerFirst.Items); len(got) != 1 || got[0] != "go-build" {
		t.Errorf("go items = %v", got)
	}
}

func TestNewReleasePlan_RejectsUnsupportedConfigPlanVersion(t *testing.T) {
	t.Parallel()

	_, err := pipeline.NewReleasePlan(pipeline.ReleasePlanInput{
		ConfigPlan: pipeline.ConfigPlan{Version: pipeline.ConfigPlanVersion + 1},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported config-plan version") {
		t.Errorf("err = %v", err)
	}
}

func TestNewReleasePlan_RejectsInvalidSBOMInput(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Artifacts: []config.Artifact{{Name: "app", ProjectType: projecttype.NPM}}}
	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}

	_, err := pipeline.NewReleasePlan(pipeline.ReleasePlanInput{
		ConfigPlan:   pipeline.NewConfigPlan(cfg),
		ReleaseSBOMs: "bad",
	})
	if err == nil || !strings.Contains(err.Error(), "sboms: unknown token") {
		t.Errorf("err = %v", err)
	}
}

func hasTransfer(items []pipeline.ArtifactTransfer, kind, name string) bool {
	for _, item := range items {
		if string(item.Kind) == kind && item.Name == name {
			return true
		}
	}

	return false
}

func hasTransferTemplate(items []pipeline.ArtifactTransfer, kind, tmpl string) bool {
	for _, item := range items {
		if string(item.Kind) == kind && item.NameTemplate == tmpl {
			return true
		}
	}

	return false
}
