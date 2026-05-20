// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline

import (
	"cmp"
	"fmt"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// SnapshotReleasePlanVersion is the current snapshot-release plan contract version.
const SnapshotReleasePlanVersion = 1

// SnapshotReleasePlanInput contains workflow inputs plus the parsed config plan.
type SnapshotReleasePlanInput struct {
	ConfigPlan          ConfigPlan
	ProjectType         projecttype.Type
	Branch              string
	ReleaseSHA          string
	ReleaseActor        string
	ReleaseRepository   string
	WorkingDirectory    string
	JavaVersion         string
	NodeVersion         string
	RustToolchain       string
	Registry            string
	ReusableCIBinaryRef string
	NPMRegistry         string
	PackageScope        string
	SBOMs               string
	PublishNPM          bool
	UseCIToken          bool
}

// SnapshotReleasePlan is the top-level typed contract for the dev release setup job.
type SnapshotReleasePlan struct {
	Version           int                       `json:"version"`
	Context           SnapshotReleaseContext    `json:"context"`
	Policy            SnapshotReleasePolicy     `json:"policy"`
	Stages            SnapshotReleaseStagePlans `json:"stages"`
	ArtifactTransfers ArtifactTransferPlan      `json:"artifact_transfers"`
}

// SnapshotReleaseContext is the workflow context consumed by dev release stages.
type SnapshotReleaseContext struct {
	ProjectType         projecttype.Type `json:"project_type"`
	Branch              string           `json:"branch"`
	ReleaseSHA          string           `json:"release_sha"`
	ReleaseActor        string           `json:"release_actor"`
	ReleaseRepository   string           `json:"release_repository"`
	WorkingDirectory    string           `json:"working_directory"`
	JavaVersion         string           `json:"java_version"`
	NodeVersion         string           `json:"node_version"`
	RustToolchain       string           `json:"rust_toolchain"`
	Registry            string           `json:"registry"`
	ReusableCIBinaryRef string           `json:"reusable_ci_binary_ref"`
	NPMRegistry         string           `json:"npm_registry"`
	PackageScope        string           `json:"package_scope"`
}

// SnapshotReleasePolicy is the dev release policy envelope.
type SnapshotReleasePolicy struct {
	PublishNPM bool   `json:"publish_npm"`
	UseCIToken bool   `json:"use_ci_token"`
	SBOMs      string `json:"sboms"`
}

// SnapshotReleaseStagePlans contains the stage-specific plans emitted separately
// as snapshot-build-stage-plan-json and snapshot-publish-stage-plan-json.
type SnapshotReleaseStagePlans struct {
	Build   DevBuildStagePlan   `json:"build"`
	Publish DevPublishStagePlan `json:"publish"`
}

// DevBuildStagePlan describes dev build-stage targets.
type DevBuildStagePlan struct {
	Version int             `json:"version"`
	Stage   string          `json:"stage"`
	Targets DevBuildTargets `json:"targets"`
}

// DevBuildTargets are the artifact-first build jobs in the dev build stage.
type DevBuildTargets struct {
	Maven         TargetPlan[PlannedArtifact] `json:"maven"`
	NPM           TargetPlan[PlannedArtifact] `json:"npm"`
	Gradle        TargetPlan[PlannedArtifact] `json:"gradle"`
	GradleAndroid TargetPlan[PlannedArtifact] `json:"gradle_android"`
	XcodeIOS      TargetPlan[PlannedArtifact] `json:"xcode_ios"`
	Go            TargetPlan[PlannedArtifact] `json:"go"`
	Cargo         TargetPlan[PlannedArtifact] `json:"cargo"`
}

// DevPublishStagePlan describes dev publish-stage targets.
type DevPublishStagePlan struct {
	Version int               `json:"version"`
	Stage   string            `json:"stage"`
	Inputs  DevPublishInputs  `json:"inputs"`
	Targets DevPublishTargets `json:"targets"`
}

// DevPublishInputs are singleton projections for dev publish jobs that are not
// matrix-expanded. This keeps fragile items[0] indexing out of workflow YAML.
type DevPublishInputs struct {
	NPMWorkingDirectory    string `json:"npm_working_directory,omitempty"`
	NPMBuildArtifactName   string `json:"npm_build_artifact_name,omitempty"`
	CargoWorkingDirectory  string `json:"cargo_working_directory,omitempty"`
	GoSBOMWorkingDirectory string `json:"go_sbom_working_directory,omitempty"`
	GoSBOMArtifactName     string `json:"go_sbom_artifact_name,omitempty"`
	ArtifactName           string `json:"artifact_name,omitempty"`
}

// DevPublishTargets are the publish-stage jobs and supporting inputs.
type DevPublishTargets struct {
	NPM                 TargetPlan[PlannedArtifact] `json:"npm"`
	CargoContainerFirst TargetPlan[PlannedArtifact] `json:"cargo_container_first"`
	GoContainerFirst    TargetPlan[PlannedArtifact] `json:"go_container_first"`
	GoArtifactFirst     TargetPlan[PlannedArtifact] `json:"go_artifact_first"`
	CargoArtifactFirst  TargetPlan[PlannedArtifact] `json:"cargo_artifact_first"`
	SBOM                TargetPlan[string]          `json:"sbom"`
}

// TargetPlan describes whether a target can run and the typed matrix items it
// would run over.
type TargetPlan[T any] struct {
	Runs  bool `json:"runs"`
	Items []T  `json:"items"`
}

// NewSnapshotReleasePlan builds the snapshot-release setup contract from a config plan
// and workflow inputs.
func NewSnapshotReleasePlan(in SnapshotReleasePlanInput) (SnapshotReleasePlan, error) {
	if in.ConfigPlan.Version != ConfigPlanVersion {
		return SnapshotReleasePlan{}, fmt.Errorf("unsupported config-plan version %d: %w", in.ConfigPlan.Version, errs.ErrInvalidConfig)
	}

	projectType := in.ProjectType
	if projectType == "" {
		projectType = in.ConfigPlan.FallbackProjectType
	}

	if projectType == "" {
		return SnapshotReleasePlan{}, fmt.Errorf("project-type is empty and no fallback could be derived from config-plan-json: %w", errs.ErrInvalidConfig)
	}

	if !projecttype.IsIn(projectType, config.ValidProjectTypes) {
		return SnapshotReleasePlan{}, fmt.Errorf("unknown project-type %q: %w", projectType, errs.ErrInvalidConfig)
	}

	context := SnapshotReleaseContext{
		ProjectType:         projectType,
		Branch:              in.Branch,
		ReleaseSHA:          in.ReleaseSHA,
		ReleaseActor:        in.ReleaseActor,
		ReleaseRepository:   in.ReleaseRepository,
		WorkingDirectory:    cmp.Or(in.WorkingDirectory, "."),
		JavaVersion:         in.JavaVersion,
		NodeVersion:         in.NodeVersion,
		RustToolchain:       cmp.Or(in.RustToolchain, "stable"),
		Registry:            in.Registry,
		ReusableCIBinaryRef: in.ReusableCIBinaryRef,
		NPMRegistry:         in.NPMRegistry,
		PackageScope:        in.PackageScope,
	}
	policy := SnapshotReleasePolicy{
		PublishNPM: in.PublishNPM,
		UseCIToken: in.UseCIToken,
		SBOMs:      cmp.Or(in.SBOMs, "none"),
	}
	build := NewDevBuildStagePlan(in.ConfigPlan)

	buildSBOM, err := hasSBOMLayer(policy.SBOMs, config.SBOMLayerBuild)
	if err != nil {
		return SnapshotReleasePlan{}, err
	}

	publish := NewDevPublishStagePlan(in.ConfigPlan, policy, buildSBOM, projectType)
	if err := validateDevPublishSingletonTargets(projectType, policy, publish); err != nil {
		return SnapshotReleasePlan{}, err
	}

	return SnapshotReleasePlan{
		Version:           SnapshotReleasePlanVersion,
		Context:           context,
		Policy:            policy,
		ArtifactTransfers: NewSnapshotReleaseArtifactTransferPlan(in.ConfigPlan, policy),
		Stages: SnapshotReleaseStagePlans{
			Build:   build,
			Publish: publish,
		},
	}, nil
}

// NewSnapshotReleaseArtifactTransferPlan lists exact artifact downloads needed by
// the dev SBOM aggregation job. Dev transfers are optional because skipped
// stage legs should not fail the aggregation job.
//
//nolint:cyclop // plans transfers with one branch per artifact category.
func NewSnapshotReleaseArtifactTransferPlan(configPlan ConfigPlan, policy SnapshotReleasePolicy) ArtifactTransferPlan {
	items := make([]ArtifactTransfer, 0)
	includeBuild := policyIncludesSBOMLayer(policy.SBOMs, config.SBOMLayerBuild)

	includeAnalyzedArtifact := policyIncludesSBOMLayer(policy.SBOMs, config.SBOMLayerAnalyzedArtifact)
	for _, artifact := range configPlan.Artifacts.All {
		if includeAnalyzedArtifact && artifact.BuildArtifactName != "" {
			items = append(items, ArtifactTransfer{
				Kind:     ArtifactTransferBuildArtifact,
				Name:     artifact.BuildArtifactName,
				Path:     "./release-artifacts/", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Required: false,
			})
		}

		if includeBuild && artifact.BuildSBOMArtifactName != "" && artifactIncludesSBOMLayer(artifact, config.SBOMLayerBuild) {
			items = append(items, ArtifactTransfer{
				Kind:     ArtifactTransferBuildSBOM,
				Name:     artifact.BuildSBOMArtifactName,
				Path:     "./release-artifacts/",
				Required: false,
			})
		}
	}

	if includeAnalyzedArtifact {
		for _, container := range configPlan.Containers.All {
			if container.ExtractBinaryTarget == "" {
				continue
			}

			for _, suffix := range transferPlatformSuffixes(container.Platforms) {
				items = append(items, ArtifactTransfer{
					Kind:     ArtifactTransferExtractedBinaries,
					Name:     fmt.Sprintf("%s-binaries-%s", transferContainerName(container.Name), suffix),
					Path:     "./release-artifacts/binaries/",
					Required: false,
				})
			}
		}
	}

	return ArtifactTransferPlan{Version: ArtifactTransferPlanVersion, Items: items}
}

// NewDevBuildStagePlan builds the standalone dev build-stage plan.
func NewDevBuildStagePlan(configPlan ConfigPlan) DevBuildStagePlan {
	artifacts := configPlan.Artifacts

	return DevBuildStagePlan{
		Version: SnapshotReleasePlanVersion,
		Stage:   "dev-build",
		Targets: DevBuildTargets{
			Maven:         newTargetPlan(artifacts.Maven),
			NPM:           newTargetPlan(artifacts.NPM),
			Gradle:        newTargetPlan(artifacts.Gradle),
			GradleAndroid: newTargetPlan(artifacts.GradleAndroid),
			XcodeIOS:      newTargetPlan(artifacts.XcodeIOS),
			Go:            newTargetPlan(artifacts.GoArtifactFirst),
			Cargo:         newTargetPlan(artifacts.CargoArtifactFirst),
		},
	}
}

// NewDevPublishStagePlan builds the standalone dev publish-stage plan.
func NewDevPublishStagePlan(configPlan ConfigPlan, policy SnapshotReleasePolicy, buildSBOM bool, projectType projecttype.Type) DevPublishStagePlan {
	artifacts := configPlan.Artifacts
	// The snapshot flow builds no containers: a container is built ONCE on the
	// release path and promoted to :dev by the build-once/promote-many ladder
	// (promote-stage.yml), never rebuilt for the snapshot flow.
	targets := DevPublishTargets{
		NPM:                 targetPlan(artifacts.NPM, len(artifacts.NPM) > 0 && policy.PublishNPM),
		CargoContainerFirst: targetPlan(artifacts.CargoContainerFirst, len(artifacts.CargoContainerFirst) > 0 && buildSBOM),
		GoContainerFirst:    targetPlan(artifacts.GoContainerFirst, len(artifacts.GoContainerFirst) > 0 && buildSBOM),
		GoArtifactFirst:     targetPlan(artifacts.GoArtifactFirst, false),
		CargoArtifactFirst:  targetPlan(artifacts.CargoArtifactFirst, false),
		SBOM:                singletonTargetPlan("dev-sboms", policy.SBOMs != "none"), //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}

	return DevPublishStagePlan{
		Version: SnapshotReleasePlanVersion,
		Stage:   "dev-publish",
		Inputs:  newDevPublishInputs(targets, artifacts.All, projectType),
		Targets: targets,
	}
}

func newDevPublishInputs(targets DevPublishTargets, artifacts []PlannedArtifact, projectType projecttype.Type) DevPublishInputs {
	var out DevPublishInputs
	if len(targets.NPM.Items) > 0 {
		out.NPMWorkingDirectory = targets.NPM.Items[0].WorkingDirectory
		out.NPMBuildArtifactName = targets.NPM.Items[0].BuildArtifactName
	}

	if len(targets.CargoContainerFirst.Items) > 0 {
		out.CargoWorkingDirectory = targets.CargoContainerFirst.Items[0].WorkingDirectory
	}

	if len(targets.GoContainerFirst.Items) > 0 {
		out.GoSBOMWorkingDirectory = targets.GoContainerFirst.Items[0].WorkingDirectory
		out.GoSBOMArtifactName = targets.GoContainerFirst.Items[0].Name
	}

	out.ArtifactName = devArtifactName(targets, artifacts, projectType)

	return out
}

//nolint:cyclop // names artifacts with one branch per project type.
func devArtifactName(targets DevPublishTargets, artifacts []PlannedArtifact, projectType projecttype.Type) string {
	if projectType == projecttype.Go && len(targets.GoArtifactFirst.Items) > 0 {
		return targets.GoArtifactFirst.Items[0].Name
	}

	if projectType == projecttype.Go && len(targets.GoContainerFirst.Items) > 0 {
		return targets.GoContainerFirst.Items[0].Name
	}

	if projectType == projecttype.NPM && len(targets.NPM.Items) > 0 {
		return targets.NPM.Items[0].Name
	}

	for _, artifact := range artifacts {
		if artifact.ProjectType == projectType {
			return artifact.Name
		}
	}

	if len(targets.GoArtifactFirst.Items) > 0 {
		return targets.GoArtifactFirst.Items[0].Name
	}

	if len(targets.GoContainerFirst.Items) > 0 {
		return targets.GoContainerFirst.Items[0].Name
	}

	if len(artifacts) > 0 {
		return artifacts[0].Name
	}

	return ""
}

func hasSBOMLayer(value string, layer config.SBOMLayer) (bool, error) {
	layers, err := config.ExpandSBOMs(value)
	if err != nil {
		return false, err
	}

	for _, got := range layers {
		if got == layer {
			return true, nil
		}
	}

	return false, nil
}

func newTargetPlan[T any](items []T) TargetPlan[T] {
	return targetPlan(items, len(items) > 0)
}

func targetPlan[T any](items []T, runs bool) TargetPlan[T] {
	out := make([]T, 0, len(items))
	out = append(out, items...)

	return TargetPlan[T]{Runs: runs, Items: out}
}

func singletonTargetPlan(name string, runs bool) TargetPlan[string] {
	items := []string{}
	if runs {
		items = []string{name}
	}

	return TargetPlan[string]{Runs: runs, Items: items}
}

func validateDevPublishSingletonTargets(projectType projecttype.Type, policy SnapshotReleasePolicy, plan DevPublishStagePlan) error {
	if err := validateRunnableSingleton("npm", plan.Targets.NPM.Runs, len(plan.Targets.NPM.Items)); err != nil {
		return err
	}

	if err := validateRunnableSingleton("cargo", plan.Targets.CargoContainerFirst.Runs, len(plan.Targets.CargoContainerFirst.Items)); err != nil {
		return err
	}

	if err := validateRunnableSingleton("go_container_first", plan.Targets.GoContainerFirst.Runs, len(plan.Targets.GoContainerFirst.Items)); err != nil {
		return err
	}

	if projectType == projecttype.Go && policy.SBOMs != "none" {
		goItems := len(plan.Targets.GoArtifactFirst.Items) + len(plan.Targets.GoContainerFirst.Items)
		if goItems > 1 {
			return fmt.Errorf("dev publish target go supports exactly one artifact when dev SBOMs are enabled, got %d: %w", goItems, errs.ErrInvalidConfig)
		}
	}

	return nil
}

func validateRunnableSingleton(name string, runs bool, count int) error {
	if runs && count > 1 {
		return fmt.Errorf("dev publish target %s supports exactly one artifact, got %d: %w", name, count, errs.ErrInvalidConfig)
	}

	return nil
}
