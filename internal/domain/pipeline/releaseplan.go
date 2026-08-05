// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/listval"
)

// ReleasePlanVersion is the current release plan contract version.
const ReleasePlanVersion = 1

// ArtifactTransferPlanVersion is the current artifact-transfer plan contract
// version. It is shared by production and dev release flows.
const ArtifactTransferPlanVersion = 1

// ReleasePlanInput contains workflow inputs plus the parsed config plan.
type ReleasePlanInput struct {
	ConfigPlan                      ConfigPlan
	Branch                          string
	RefName                         string
	FilePattern                     string
	ReleaseType                     string
	ReleasePublisher                string
	ReleaseRequireAllowlistedSigner bool
	ReleaseDraft                    bool
	ReleaseSBOMs                    string
	ReleaseSignArtifacts            bool
	ChangelogCreator                string
	ChangelogSkipVersionBump        bool
}

// ReleasePlan is the top-level typed contract for production release setup.
type ReleasePlan struct {
	Version           int                  `json:"version"`
	Context           ReleaseContext       `json:"context"`
	Policy            ReleasePolicy        `json:"policy"`
	Stages            ReleaseStagePlans    `json:"stages"`
	ArtifactTransfers ArtifactTransferPlan `json:"artifact_transfers"`
}

// ReleaseContext records workflow context used to derive release policy.
type ReleaseContext struct {
	Branch           string `json:"branch"`
	RefName          string `json:"ref_name"`
	FilePattern      string `json:"file_pattern,omitempty"`
	ReleaseType      string `json:"release_type,omitempty"`
	ReleasePublisher string `json:"release_publisher,omitempty"`
	ChangelogCreator string `json:"changelog_creator,omitempty"`
}

// ReleasePolicy is the release policy envelope carried by the typed plan.
type ReleasePolicy struct {
	SignArtifacts            bool                 `json:"sign_artifacts"`
	RequireAllowlistedSigner bool                 `json:"require_allowlisted_signer"`
	RunVersionBump           bool                 `json:"run_version_bump"`
	CreateRelease            bool                 `json:"create_release"`
	CreateDraftRelease       bool                 `json:"create_draft_release"`
	SBOMs                    string               `json:"sboms"`
	MakeLatest               bool                 `json:"make_latest"`
	HasContainers            bool                 `json:"has_containers"`
	SBOMConflict             *ReleaseSBOMConflict `json:"sbom_conflict,omitempty"`
}

// ReleaseSBOMConflict records a non-empty release/pipeline SBOM mismatch.
type ReleaseSBOMConflict struct {
	ReleaseSBOMs  string `json:"release_sboms"`
	PipelineSBOMs string `json:"pipeline_sboms"`
}

// ReleaseStagePlans contains the stage-specific plans emitted separately.
type ReleaseStagePlans struct {
	Prepare ReleasePrepareStagePlan `json:"prepare"`
	Build   ReleaseBuildStagePlan   `json:"build"`
	Publish ReleasePublishStagePlan `json:"publish"`
}

// ArtifactTransferPlan lists exact CI artifacts that downstream jobs download
// before SBOM generation or release attachment.
type ArtifactTransferPlan struct {
	Version int                `json:"version"`
	Items   []ArtifactTransfer `json:"items"`
}

// ArtifactTransferKind classifies why a transfer is needed.
type ArtifactTransferKind string

// Recognised ArtifactTransferKind values.
const (
	ArtifactTransferBuildArtifact         ArtifactTransferKind = "build_artifact"
	ArtifactTransferBuildSBOM             ArtifactTransferKind = "build_sbom"
	ArtifactTransferAnalyzedContainerSBOM ArtifactTransferKind = "analyzed_container_sbom"
	ArtifactTransferExtractedBinaries     ArtifactTransferKind = "extracted_binaries"
)

// ArtifactTransfer is one explicit artifact download operation. Name is used
// as-is; NameTemplate uses {run_id} replacement by transfer executors for
// artifacts whose upload name contains the current run id.
type ArtifactTransfer struct {
	Kind         ArtifactTransferKind `json:"kind"`
	Name         string               `json:"name,omitempty"`
	NameTemplate string               `json:"name_template,omitempty"`
	Path         string               `json:"path"`
	Required     bool                 `json:"required"`
}

// ValidateArtifactTransferItem validates the static transfer contract before a
// provider-specific downloader resolves templates or performs I/O.
func ValidateArtifactTransferItem(item ArtifactTransfer) error {
	if !IsArtifactTransferKind(item.Kind) {
		return fmt.Errorf("artifact transfer kind %q is invalid: %w", item.Kind, errs.ErrInvalidConfig)
	}

	name := strings.TrimSpace(item.Name)

	tmpl := strings.TrimSpace(item.NameTemplate)
	switch {
	case name == "" && tmpl == "":
		return fmt.Errorf("artifact transfer missing name/name_template: %w", errs.ErrInvalidConfig)
	case name != "" && tmpl != "":
		return fmt.Errorf("artifact transfer must set exactly one of name/name_template: %w", errs.ErrInvalidConfig)
	}

	if strings.TrimSpace(item.Path) == "" {
		return fmt.Errorf("artifact transfer %q has empty path: %w", cmp.Or(name, tmpl, "artifact"), errs.ErrInvalidConfig)
	}

	return nil
}

// IsArtifactTransferKind reports whether kind is a known transfer kind.
func IsArtifactTransferKind(kind ArtifactTransferKind) bool {
	switch kind {
	case ArtifactTransferBuildArtifact, ArtifactTransferBuildSBOM, ArtifactTransferAnalyzedContainerSBOM, ArtifactTransferExtractedBinaries:
		return true
	default:
		return false
	}
}

// ReleasePrepareStagePlan describes release prepare-stage targets.
type ReleasePrepareStagePlan struct {
	Version     int                   `json:"version"`
	Stage       string                `json:"stage"`
	FilePattern string                `json:"file_pattern,omitempty"`
	Targets     ReleasePrepareTargets `json:"targets"`
}

// ReleasePrepareTargets are the release prepare-stage jobs.
type ReleasePrepareTargets struct {
	VersionBump TargetPlan[PlannedArtifact] `json:"version_bump"`
}

// ReleaseBuildStagePlan describes release build-stage targets.
type ReleaseBuildStagePlan struct {
	Version int                 `json:"version"`
	Stage   string              `json:"stage"`
	Targets ReleaseBuildTargets `json:"targets"`
}

// ReleaseBuildTargets are the artifact-first build jobs in release build stage.
type ReleaseBuildTargets struct {
	Maven         TargetPlan[PlannedArtifact] `json:"maven"`
	NPM           TargetPlan[PlannedArtifact] `json:"npm"`
	Gradle        TargetPlan[PlannedArtifact] `json:"gradle"`
	GradleAndroid TargetPlan[PlannedArtifact] `json:"gradle_android"`
	XcodeIOS      TargetPlan[PlannedArtifact] `json:"xcode_ios"`
	Go            TargetPlan[PlannedArtifact] `json:"go"`
	Cargo         TargetPlan[PlannedArtifact] `json:"cargo"`
}

// ReleasePublishStagePlan describes release publish-stage targets.
type ReleasePublishStagePlan struct {
	Version int                   `json:"version"`
	Stage   string                `json:"stage"`
	Targets ReleasePublishTargets `json:"targets"`
}

// ReleasePublishTargets are the release publish-stage jobs.
type ReleasePublishTargets struct {
	ForgePackages       TargetPlan[PlannedArtifact]  `json:"forge_packages"`
	MavenCentral        TargetPlan[PlannedArtifact]  `json:"maven_central"`
	GooglePlay          TargetPlan[PlannedArtifact]  `json:"google_play"`
	XcodeIOS            TargetPlan[PlannedArtifact]  `json:"xcode_ios"`
	Containers          TargetPlan[PlannedContainer] `json:"containers"`
	CargoContainerFirst TargetPlan[PlannedArtifact]  `json:"cargo_container_first"`
	GoContainerFirst    TargetPlan[PlannedArtifact]  `json:"go_container_first"`
}

// NewReleasePlan builds the production release setup contract.
func NewReleasePlan(in ReleasePlanInput) (ReleasePlan, error) {
	if in.ConfigPlan.Version != ConfigPlanVersion {
		return ReleasePlan{}, fmt.Errorf("unsupported config-plan version %d: %w", in.ConfigPlan.Version, errs.ErrInvalidConfig)
	}

	policy, err := resolveReleasePolicy(releasePolicyInputs{
		ReleaseType:                     in.ReleaseType,
		ReleasePublisher:                in.ReleasePublisher,
		ReleaseRequireAllowlistedSigner: in.ReleaseRequireAllowlistedSigner,
		ReleaseDraft:                    in.ReleaseDraft,
		ReleaseSBOMs:                    in.ReleaseSBOMs,
		ReleaseSignArtifacts:            in.ReleaseSignArtifacts,
		ChangelogCreator:                in.ChangelogCreator,
		ChangelogSkipVersionBump:        in.ChangelogSkipVersionBump,
		RefName:                         in.RefName,
		PipelineSBOMs:                   in.ConfigPlan.PipelineSBOMs,
		AnyRequireAuthorization:         in.ConfigPlan.AnyRequireAuthorization,
		HasContainers:                   in.ConfigPlan.Containers.HasContainers,
	})
	if err != nil {
		return ReleasePlan{}, err
	}

	for _, container := range in.ConfigPlan.Containers.All {
		if container.EnableSLSA && (!policy.SignArtifacts || !in.ConfigPlan.Sign.SignsContainers) {
			return ReleasePlan{}, fmt.Errorf(
				"container %q enables pushed SLSA provenance but signing is not enabled with a cosign-capable method; enable release artifact signing with sign.method sigstore or kms, or set enable-slsa: false: %w",
				container.Name, errs.ErrInvalidConfig,
			)
		}
	}

	buildSBOM, err := hasSBOMLayer(policy.SBOMs, config.SBOMLayerBuild)
	if err != nil {
		return ReleasePlan{}, err
	}

	return ReleasePlan{
		Version: ReleasePlanVersion,
		Context: ReleaseContext{
			Branch:           in.Branch,
			RefName:          in.RefName,
			FilePattern:      in.FilePattern,
			ReleaseType:      in.ReleaseType,
			ReleasePublisher: in.ReleasePublisher,
			ChangelogCreator: in.ChangelogCreator,
		},
		Policy: policy,
		Stages: ReleaseStagePlans{
			Prepare: NewReleasePrepareStagePlan(in.ConfigPlan, policy, in.FilePattern),
			Build:   NewReleaseBuildStagePlan(in.ConfigPlan),
			Publish: NewReleasePublishStagePlan(in.ConfigPlan, buildSBOM),
		},
		ArtifactTransfers: NewReleaseArtifactTransferPlan(in.ConfigPlan, policy),
	}, nil
}

// NewReleasePrepareStagePlan builds the standalone release prepare-stage plan.
func NewReleasePrepareStagePlan(configPlan ConfigPlan, policy ReleasePolicy, filePattern string) ReleasePrepareStagePlan {
	return ReleasePrepareStagePlan{
		Version:     ReleasePlanVersion,
		Stage:       "prepare",
		FilePattern: filePattern,
		Targets: ReleasePrepareTargets{
			VersionBump: targetPlan(configPlan.Artifacts.All, len(configPlan.Artifacts.All) > 0 && policy.RunVersionBump),
		},
	}
}

// NewReleaseBuildStagePlan builds the standalone release build-stage plan.
func NewReleaseBuildStagePlan(configPlan ConfigPlan) ReleaseBuildStagePlan {
	artifacts := configPlan.Artifacts

	return ReleaseBuildStagePlan{
		Version: ReleasePlanVersion,
		Stage:   "build", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Targets: ReleaseBuildTargets{
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

// NewReleasePublishStagePlan builds the standalone release publish-stage plan.
func NewReleasePublishStagePlan(configPlan ConfigPlan, buildSBOM bool) ReleasePublishStagePlan {
	artifacts := configPlan.Artifacts
	cargoContainerFirst := []PlannedArtifact{}
	goContainerFirst := []PlannedArtifact{}

	if buildSBOM {
		// Container-first cargo / go only — artifact-first variants
		// emit their Build SBOMs inline in build-cargo.yml / build-go.yml
		// at the build stage. Listing them here would double-emit the
		// same bom.json under two upload-artifact paths.
		cargoContainerFirst = filterArtifactsBySBOMLayer(artifacts.CargoContainerFirst, config.SBOMLayerBuild)
		goContainerFirst = filterArtifactsBySBOMLayer(artifacts.GoContainerFirst, config.SBOMLayerBuild)
	}

	return ReleasePublishStagePlan{
		Version: ReleasePlanVersion,
		Stage:   "publish",
		Targets: ReleasePublishTargets{
			ForgePackages:       newTargetPlan(artifacts.ForgePackages),
			MavenCentral:        newTargetPlan(artifacts.MavenCentral),
			GooglePlay:          newTargetPlan(artifacts.GooglePlay),
			XcodeIOS:            newTargetPlan(artifacts.XcodeIOS),
			Containers:          targetPlan(configPlan.Containers.All, configPlan.Containers.HasContainers),
			CargoContainerFirst: newTargetPlan(cargoContainerFirst),
			GoContainerFirst:    newTargetPlan(goContainerFirst),
		},
	}
}

// NewReleaseArtifactTransferPlan builds the release-create artifact download
// plan without wildcard artifact names.
//
//nolint:cyclop // plans transfers with one branch per artifact category.
func NewReleaseArtifactTransferPlan(configPlan ConfigPlan, policy ReleasePolicy) ArtifactTransferPlan {
	items := make([]ArtifactTransfer, 0)

	for _, artifact := range configPlan.Artifacts.All {
		if artifact.BuildArtifactName != "" {
			items = append(items, ArtifactTransfer{
				Kind:     ArtifactTransferBuildArtifact,
				Name:     artifact.BuildArtifactName,
				Path:     "./release-artifacts/", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Required: true,
			})
		}

		if policyIncludesSBOMLayer(policy.SBOMs, config.SBOMLayerBuild) && artifact.BuildSBOMArtifactName != "" && artifactIncludesSBOMLayer(artifact, config.SBOMLayerBuild) {
			items = append(items, ArtifactTransfer{
				Kind:     ArtifactTransferBuildSBOM,
				Name:     artifact.BuildSBOMArtifactName,
				Path:     "./release-artifacts/",
				Required: false,
			})
		}
	}

	if policyIncludesSBOMLayer(policy.SBOMs, config.SBOMLayerAnalyzedContainer) {
		for _, container := range configPlan.Containers.All {
			if !container.EnableAnalyzedContainerSBOM {
				continue
			}

			for _, suffix := range transferPlatformSuffixes(container.Platforms) {
				items = append(items, ArtifactTransfer{
					Kind:         ArtifactTransferAnalyzedContainerSBOM,
					NameTemplate: fmt.Sprintf("analyzed-container-sbom-{run_id}-%s-%s", transferContainerName(container.Name), suffix),
					Path:         "./sbom-artifacts/",
					Required:     false,
				})
			}
		}
	}

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

	return ArtifactTransferPlan{Version: ArtifactTransferPlanVersion, Items: items}
}

func policyIncludesSBOMLayer(value string, layer config.SBOMLayer) bool {
	layers, err := config.ExpandSBOMs(value)
	if err != nil {
		return false
	}

	for _, got := range layers {
		if got == layer {
			return true
		}
	}

	return false
}

func artifactIncludesSBOMLayer(artifact PlannedArtifact, layer config.SBOMLayer) bool {
	for _, got := range artifact.EffectiveSBOMs {
		if got == layer {
			return true
		}
	}

	return false
}

func transferPlatformSuffixes(platforms string) []string {
	if strings.TrimSpace(platforms) == "" {
		return []string{"amd64"}
	}

	out := make([]string, 0)

	for _, raw := range listval.Tokens(platforms) {
		platform := strings.TrimSpace(raw)
		if platform == "" {
			continue
		}

		platform = strings.TrimPrefix(platform, "linux/")
		platform = strings.ReplaceAll(platform, "/", "-")
		out = append(out, platform)
	}

	if len(out) == 0 {
		return []string{"amd64"}
	}

	return out
}

func transferContainerName(name string) string {
	if name == "" {
		return "container"
	}

	return name
}

func filterArtifactsBySBOMLayer(in []PlannedArtifact, layer config.SBOMLayer) []PlannedArtifact {
	return filterArtifacts(in, func(a PlannedArtifact) bool {
		for _, got := range a.EffectiveSBOMs {
			if got == layer {
				return true
			}
		}

		return false
	})
}
