// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

//nolint:cyclop // exercises many invariants on one SnapshotReleasePlan.
func TestNewSnapshotReleasePlan_UsesFallbackProjectTypeAndBuildsStagePlans(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Artifacts: []config.Artifact{
			{Name: "api", ProjectType: projecttype.NPM}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			{Name: "rust-service", ProjectType: projecttype.Cargo, Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			{Name: "go-cli", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeArtifactFirst}},                    //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			{Name: "go-service", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}},               //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Containers: []config.Container{{Name: "image"}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}
	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}

	configPlan := pipeline.NewConfigPlan(cfg)

	plan, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{
		ConfigPlan: configPlan,
		Branch:     "feature/dev",
		PublishNPM: true,
		UseCIToken: true,
		SBOMs:      "build", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		t.Fatal(err)
	}

	if plan.Version != pipeline.SnapshotReleasePlanVersion {
		t.Errorf("version = %d", plan.Version)
	}

	if plan.Context.ProjectType != projecttype.NPM {
		t.Errorf("project type = %q", plan.Context.ProjectType)
	}

	if plan.Context.WorkingDirectory != "." || plan.Context.RustToolchain != "stable" {
		t.Errorf("context defaults = %+v", plan.Context)
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

func TestNewSnapshotReleasePlan_ComputesExplicitBinaryTransfers(t *testing.T) {
	t.Parallel()

	configPlan := pipeline.NewConfigPlan(&config.Config{
		Artifacts: []config.Artifact{{Name: "go-service", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}}},
		Containers: []config.Container{{
			Name:      "api",
			From:      []string{"go-service"},
			Platforms: "linux/amd64,linux/arm64",                                                          //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			Extract:   &config.ContainerExtract{Binary: &config.ContainerExtractBinary{Target: "export"}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		}},
	})

	plan, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{
		ConfigPlan:  configPlan,
		ProjectType: projecttype.Go,
		SBOMs:       "analyzed-artifact", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		t.Fatal(err)
	}

	if !hasTransfer(plan.ArtifactTransfers.Items, "extracted_binaries", "api-binaries-arm64") {
		t.Errorf("missing dev extracted binary transfer: %+v", plan.ArtifactTransfers.Items)
	}
}

func TestNewSnapshotReleasePlan_ProjectTypeOverrideWins(t *testing.T) {
	t.Parallel()

	configPlan := pipeline.NewConfigPlan(&config.Config{Artifacts: []config.Artifact{{Name: "api", ProjectType: projecttype.NPM}}})

	plan, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{
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

func TestNewSnapshotReleasePlan_RejectsUnsupportedConfigPlanVersion(t *testing.T) {
	t.Parallel()

	_, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{
		ConfigPlan: pipeline.ConfigPlan{Version: pipeline.ConfigPlanVersion + 1},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported config-plan version") {
		t.Errorf("err = %v", err)
	}
}

func TestNewSnapshotReleasePlan_RejectsInvalidSBOMInput(t *testing.T) {
	t.Parallel()

	configPlan := pipeline.NewConfigPlan(&config.Config{Artifacts: []config.Artifact{{Name: "api", ProjectType: projecttype.NPM}}})

	_, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{
		ConfigPlan: configPlan,
		SBOMs:      "bogus",
	})
	if err == nil || !strings.Contains(err.Error(), "sboms: unknown token") {
		t.Errorf("err = %v", err)
	}
}

func TestNewSnapshotReleasePlan_EmptyProjectTypeErrors(t *testing.T) {
	t.Parallel()

	_, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{
		ConfigPlan: pipeline.ConfigPlan{Version: pipeline.ConfigPlanVersion},
	})
	if err == nil || !strings.Contains(err.Error(), "project-type is empty") {
		t.Errorf("err = %v", err)
	}
}

func TestNewSnapshotReleasePlan_RejectsUnknownProjectType(t *testing.T) {
	t.Parallel()

	configPlan := pipeline.NewConfigPlan(&config.Config{Artifacts: []config.Artifact{{Name: "api", ProjectType: projecttype.NPM}}})

	_, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{
		ConfigPlan:  configPlan,
		ProjectType: "rust",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown project-type") {
		t.Errorf("err = %v", err)
	}
}

func TestNewSnapshotReleasePlan_RejectsMultipleNPMDevPublishTargets(t *testing.T) {
	t.Parallel()

	configPlan := pipeline.NewConfigPlan(&config.Config{Artifacts: []config.Artifact{
		{Name: "api", ProjectType: projecttype.NPM},
		{Name: "web", ProjectType: projecttype.NPM}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}})

	_, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{
		ConfigPlan:  configPlan,
		ProjectType: projecttype.NPM,
		PublishNPM:  true,
	})
	if err == nil || !strings.Contains(err.Error(), "dev publish target npm supports exactly one artifact") {
		t.Errorf("err = %v", err)
	}
}

func TestNewSnapshotReleasePlan_RejectsMultipleCargoDevSBOMTargets(t *testing.T) {
	t.Parallel()

	configPlan := pipeline.NewConfigPlan(&config.Config{Artifacts: []config.Artifact{
		{Name: "worker", ProjectType: projecttype.Cargo, Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}},
		{Name: "cli", ProjectType: projecttype.Cargo, Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}},
	}})

	_, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{
		ConfigPlan:  configPlan,
		ProjectType: projecttype.Cargo,
		SBOMs:       "build",
	})
	if err == nil || !strings.Contains(err.Error(), "dev publish target cargo supports exactly one artifact") {
		t.Errorf("err = %v", err)
	}
}

func TestNewSnapshotReleasePlan_RejectsAmbiguousGoDevSBOMArtifactName(t *testing.T) {
	t.Parallel()

	configPlan := pipeline.NewConfigPlan(&config.Config{Artifacts: []config.Artifact{
		{Name: "go-cli", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeArtifactFirst}},
		{Name: "go-service", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}},
	}})

	_, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{
		ConfigPlan:  configPlan,
		ProjectType: projecttype.Go,
		SBOMs:       "analyzed-artifact",
	})
	if err == nil || !strings.Contains(err.Error(), "dev publish target go supports exactly one artifact") {
		t.Errorf("err = %v", err)
	}
}

// gradleSnapshotConfig is an android-library config routed to both gradle
// publish buckets, which is the shape the snapshot gradle legs plan over.
func gradleSnapshotConfig(t *testing.T) pipeline.ConfigPlan {
	t.Helper()

	cfg := &config.Config{
		Artifacts: []config.Artifact{{
			Name:        "android-lib",
			ProjectType: projecttype.GradleAndroid,
			BuildType:   config.BuildTypeLibrary,
			PublishTo:   []config.PublishTarget{config.PublishMavenCentral, config.PublishForgePackages},
			GradleAndroid: &config.GradleAndroidConfig{
				BuildModule: "access-mechanism",
			},
		}},
	}
	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}

	return pipeline.NewConfigPlan(cfg)
}

// The gradle snapshot legs are the only credentialed jobs in the snapshot
// flow, so they are opt-in: a caller that does not ask for them must not
// get a job holding Maven Central credentials and a signing key merely
// because its config declares a gradle publish target.
func TestNewSnapshotReleasePlan_GradlePublishIsOptIn(t *testing.T) {
	t.Parallel()

	plan, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{
		ConfigPlan: gradleSnapshotConfig(t),
		Branch:     "feature/dev",
	})
	if err != nil {
		t.Fatal(err)
	}

	targets := plan.Stages.Publish.Targets
	if targets.MavenCentralGradle.Runs || targets.ForgePackagesGradle.Runs {
		t.Errorf("gradle publish runs without publish-gradle: maven_central=%v forge_packages=%v",
			targets.MavenCentralGradle.Runs, targets.ForgePackagesGradle.Runs)
	}
}

func TestNewSnapshotReleasePlan_GradlePublishRunsWhenRequested(t *testing.T) {
	t.Parallel()

	plan, err := pipeline.NewSnapshotReleasePlan(pipeline.SnapshotReleasePlanInput{
		ConfigPlan:    gradleSnapshotConfig(t),
		Branch:        "feature/dev",
		PublishGradle: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	targets := plan.Stages.Publish.Targets
	if !targets.MavenCentralGradle.Runs || !targets.ForgePackagesGradle.Runs {
		t.Fatalf("gradle publish did not run: maven_central=%v forge_packages=%v",
			targets.MavenCentralGradle.Runs, targets.ForgePackagesGradle.Runs)
	}

	// The workflow reads needs_android_sdk off each matrix item to pick the
	// runtime image; an Android library planned without it would publish
	// from the plain-JVM image and fail resolving the Android plugin.
	if len(targets.MavenCentralGradle.Items) != 1 {
		t.Fatalf("maven_central_gradle items = %d, want 1", len(targets.MavenCentralGradle.Items))
	}

	if !targets.MavenCentralGradle.Items[0].NeedsAndroidSDK {
		t.Error("android library planned without needs_android_sdk")
	}
}
