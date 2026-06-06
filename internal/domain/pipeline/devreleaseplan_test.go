// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

//nolint:cyclop // exercises many invariants on one DevReleasePlan.
func TestNewDevReleasePlan_UsesFallbackProjectTypeAndBuildsStagePlans(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Artifacts: []config.Artifact{
			{Name: "api", ProjectType: projecttype.NPM}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			{Name: "rust-service", ProjectType: projecttype.Cargo, Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			{Name: "go-cli", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeArtifactFirst}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			{Name: "go-service", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Containers: []config.Container{{Name: "image"}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}
	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}

	configPlan := pipeline.NewConfigPlan(cfg)

	plan, err := pipeline.NewDevReleasePlan(pipeline.DevReleasePlanInput{
		ConfigPlan:       configPlan,
		Branch:           "feature/dev",
		PublishNPM:       true,
		UseCIToken:       true,
		PublishContainer: true,
		SBOMs:            "build", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		t.Fatal(err)
	}

	if plan.Version != pipeline.DevReleasePlanVersion {
		t.Errorf("version = %d", plan.Version)
	}

	if plan.Context.ProjectType != projecttype.NPM {
		t.Errorf("project type = %q", plan.Context.ProjectType)
	}

	if plan.Context.WorkingDirectory != "." || plan.Context.RustToolchain != "stable" {
		t.Errorf("context defaults = %+v", plan.Context)
	}

	if !plan.HasContainers || !plan.Stages.Publish.Targets.Containers.Runs {
		t.Errorf("container targets = %+v", plan.Stages.Publish.Targets.Containers)
	}

	if !plan.Stages.Build.Targets.NPM.Runs || !plan.Stages.Build.Targets.Go.Runs {
		t.Errorf("build targets = %+v", plan.Stages.Build.Targets)
	}

	if got := len(plan.Stages.Publish.Targets.GoContainerFirst.Items); got != 1 {
		t.Errorf("go container-first items = %d", got)
	}

	if !plan.Stages.Publish.Targets.CargoContainerFirst.Runs || !plan.Stages.Publish.Targets.GoContainerFirst.Runs {
		t.Errorf("build SBOM publish targets = %+v", plan.Stages.Publish.Targets)
	}

	if plan.Stages.Publish.Inputs.NPMWorkingDirectory != "." {
		t.Errorf("npm working directory = %q", plan.Stages.Publish.Inputs.NPMWorkingDirectory)
	}

	if plan.Stages.Publish.Inputs.NPMBuildArtifactName != "api-build-artifacts" {
		t.Errorf("npm build artifact name = %q", plan.Stages.Publish.Inputs.NPMBuildArtifactName)
	}

	if plan.Stages.Publish.Inputs.ArtifactName != "api" {
		t.Errorf("artifact name = %q", plan.Stages.Publish.Inputs.ArtifactName)
	}

	if plan.Stages.Publish.Inputs.CargoWorkingDirectory != "." {
		t.Errorf("cargo working directory = %q", plan.Stages.Publish.Inputs.CargoWorkingDirectory)
	}

	if plan.Stages.Publish.Inputs.GoSBOMArtifactName != "go-service" || plan.Stages.Publish.Inputs.GoSBOMWorkingDirectory != "." {
		t.Errorf("go sbom inputs = %+v", plan.Stages.Publish.Inputs)
	}

	if !plan.Stages.Publish.Targets.SBOM.Runs {
		t.Errorf("dev SBOM target should run when sboms requests a layer: %+v", plan.Stages.Publish.Targets.SBOM)
	}

	if plan.Stages.Publish.Targets.GoArtifactFirst.Runs {
		t.Errorf("go artifact-first publish support target should not run directly")
	}

	if !hasTransfer(plan.ArtifactTransfers.Items, "build_sbom", "api-build-sbom") {
		t.Errorf("missing dev build SBOM transfer: %+v", plan.ArtifactTransfers.Items)
	}
}

func TestNewDevReleasePlan_ComputesExplicitBinaryTransfers(t *testing.T) {
	t.Parallel()

	configPlan := pipeline.NewConfigPlan(&config.Config{
		Artifacts: []config.Artifact{{Name: "go-service", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}}},
		Containers: []config.Container{{
			Name:      "api",
			From:      []string{"go-service"},
			Platforms: "linux/amd64,linux/arm64", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			Extract:   &config.ContainerExtract{Binary: &config.ContainerExtractBinary{Target: "export"}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		}},
	})

	plan, err := pipeline.NewDevReleasePlan(pipeline.DevReleasePlanInput{
		ConfigPlan:       configPlan,
		ProjectType:      projecttype.Go,
		PublishContainer: true,
		SBOMs:            "analyzed-artifact", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		t.Fatal(err)
	}

	if !hasTransfer(plan.ArtifactTransfers.Items, "extracted_binaries", "api-binaries-arm64") {
		t.Errorf("missing dev extracted binary transfer: %+v", plan.ArtifactTransfers.Items)
	}
}

func TestNewDevReleasePlan_ProjectTypeOverrideWins(t *testing.T) {
	t.Parallel()

	configPlan := pipeline.NewConfigPlan(&config.Config{Artifacts: []config.Artifact{{Name: "api", ProjectType: projecttype.NPM}}})

	plan, err := pipeline.NewDevReleasePlan(pipeline.DevReleasePlanInput{
		ConfigPlan:  configPlan,
		ProjectType: projecttype.Maven,
	})
	if err != nil {
		t.Fatal(err)
	}

	if plan.Context.ProjectType != projecttype.Maven {
		t.Errorf("project type = %q", plan.Context.ProjectType)
	}
}

func TestNewDevReleasePlan_RejectsUnsupportedConfigPlanVersion(t *testing.T) {
	t.Parallel()

	_, err := pipeline.NewDevReleasePlan(pipeline.DevReleasePlanInput{
		ConfigPlan: pipeline.ConfigPlan{Version: pipeline.ConfigPlanVersion + 1},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported config-plan version") {
		t.Errorf("err = %v", err)
	}
}

func TestNewDevReleasePlan_RejectsInvalidSBOMInput(t *testing.T) {
	t.Parallel()

	configPlan := pipeline.NewConfigPlan(&config.Config{Artifacts: []config.Artifact{{Name: "api", ProjectType: projecttype.NPM}}})

	_, err := pipeline.NewDevReleasePlan(pipeline.DevReleasePlanInput{
		ConfigPlan: configPlan,
		SBOMs:      "bogus",
	})
	if err == nil || !strings.Contains(err.Error(), "sboms: unknown token") {
		t.Errorf("err = %v", err)
	}
}

func TestNewDevReleasePlan_EmptyProjectTypeErrors(t *testing.T) {
	t.Parallel()

	_, err := pipeline.NewDevReleasePlan(pipeline.DevReleasePlanInput{
		ConfigPlan: pipeline.ConfigPlan{Version: pipeline.ConfigPlanVersion},
	})
	if err == nil || !strings.Contains(err.Error(), "project-type is empty") {
		t.Errorf("err = %v", err)
	}
}

func TestNewDevReleasePlan_RejectsUnknownProjectType(t *testing.T) {
	t.Parallel()

	configPlan := pipeline.NewConfigPlan(&config.Config{Artifacts: []config.Artifact{{Name: "api", ProjectType: projecttype.NPM}}})

	_, err := pipeline.NewDevReleasePlan(pipeline.DevReleasePlanInput{
		ConfigPlan:  configPlan,
		ProjectType: "rust",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown project-type") {
		t.Errorf("err = %v", err)
	}
}

func TestNewDevReleasePlan_RejectsMultipleNPMDevPublishTargets(t *testing.T) {
	t.Parallel()

	configPlan := pipeline.NewConfigPlan(&config.Config{Artifacts: []config.Artifact{
		{Name: "api", ProjectType: projecttype.NPM},
		{Name: "web", ProjectType: projecttype.NPM}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}})

	_, err := pipeline.NewDevReleasePlan(pipeline.DevReleasePlanInput{
		ConfigPlan:  configPlan,
		ProjectType: projecttype.NPM,
		PublishNPM:  true,
	})
	if err == nil || !strings.Contains(err.Error(), "dev publish target npm supports exactly one artifact") {
		t.Errorf("err = %v", err)
	}
}

func TestNewDevReleasePlan_RejectsMultipleCargoDevSBOMTargets(t *testing.T) {
	t.Parallel()

	configPlan := pipeline.NewConfigPlan(&config.Config{Artifacts: []config.Artifact{
		{Name: "worker", ProjectType: projecttype.Cargo, Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}},
		{Name: "cli", ProjectType: projecttype.Cargo, Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}},
	}})

	_, err := pipeline.NewDevReleasePlan(pipeline.DevReleasePlanInput{
		ConfigPlan:  configPlan,
		ProjectType: projecttype.Cargo,
		SBOMs:       "build",
	})
	if err == nil || !strings.Contains(err.Error(), "dev publish target cargo supports exactly one artifact") {
		t.Errorf("err = %v", err)
	}
}

func TestNewDevReleasePlan_RejectsAmbiguousGoDevSBOMArtifactName(t *testing.T) {
	t.Parallel()

	configPlan := pipeline.NewConfigPlan(&config.Config{Artifacts: []config.Artifact{
		{Name: "go-cli", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeArtifactFirst}},
		{Name: "go-service", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}},
	}})

	_, err := pipeline.NewDevReleasePlan(pipeline.DevReleasePlanInput{
		ConfigPlan:  configPlan,
		ProjectType: projecttype.Go,
		SBOMs:       "analyzed-artifact",
	})
	if err == nil || !strings.Contains(err.Error(), "dev publish target go supports exactly one artifact") {
		t.Errorf("err = %v", err)
	}
}
