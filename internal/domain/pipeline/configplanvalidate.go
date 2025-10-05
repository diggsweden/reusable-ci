// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// ValidateConfigPlan checks the redundant projections, producer-derived execution
// fields and gates consumed by release planning. It does not replace artifacts.yml
// validation or validate every field for every downstream executor. Invalid
// projections are refused, never repaired from another copy or a fresh derivation.
func ValidateConfigPlan(plan ConfigPlan) error { //nolint:cyclop // bounded, independent contract invariants with field-specific refusals.
	if plan.Version != ConfigPlanVersion {
		return fmt.Errorf("unsupported config-plan version %d: %w", plan.Version, errs.ErrInvalidConfig)
	}

	artifactNames := make(map[string]bool, len(plan.Artifacts.All))
	sources := make([]config.Artifact, 0, len(plan.Artifacts.All))
	anyAuthorization := false
	sbomUnion := make(map[config.SBOMLayer]bool)

	for index, artifact := range plan.Artifacts.All {
		if artifact.Name == "" || artifactNames[artifact.Name] {
			return fmt.Errorf("config-plan artifacts.all[%d] has an empty or duplicate name: %w", index, errs.ErrInvalidConfig)
		}

		artifactNames[artifact.Name] = true
		anyAuthorization = anyAuthorization || artifact.RequireAuthorization
		maps.Copy(sbomUnion, layerSet(artifact.EffectiveSBOMs))

		if !projecttype.IsIn(artifact.ProjectType, config.ValidProjectTypes) {
			return fmt.Errorf("config-plan artifacts.all[%d] has an unsupported project_type: %w", index, errs.ErrInvalidConfig)
		}

		source := config.Artifact{
			Name: artifact.Name, ProjectType: artifact.ProjectType, SBOMs: artifact.SBOMs,
			Go: artifact.Go, Cargo: artifact.Cargo, GradleAndroid: artifact.GradleAndroid, XcodeIOS: artifact.XcodeIOS,
		}
		if artifact.GoBuildMode != config.GoArtifactBuildMode(source) || artifact.CargoBuildMode != config.CargoArtifactBuildMode(source) {
			return fmt.Errorf("config-plan artifacts.all[%d] has inconsistent build modes: %w", index, errs.ErrInvalidConfig)
		}

		if err := validateArtifactExecution(artifact, source); err != nil {
			return fmt.Errorf("config-plan artifacts.all[%d].%w", index, err)
		}

		sources = append(sources, source)
	}

	if err := validateArtifactSets(plan.Artifacts); err != nil {
		return err
	}

	if plan.AnyRequireAuthorization != anyAuthorization {
		return fmt.Errorf("config-plan any_require_authorization disagrees with artifacts.all: %w", errs.ErrInvalidConfig)
	}
	// PipelineSBOMs is documented as the union of All's effective layers.
	// Compare sets so equivalent SBOM spellings do not become schema errors.
	pipelineLayers, expandErr := config.ExpandSBOMs(plan.PipelineSBOMs)
	if expandErr != nil {
		return fmt.Errorf("config-plan pipeline_sboms: %w: %w", expandErr, errs.ErrInvalidConfig)
	}

	if !maps.Equal(layerSet(pipelineLayers), sbomUnion) {
		return fmt.Errorf("config-plan pipeline_sboms disagrees with artifacts.all: %w", errs.ErrInvalidConfig)
	}

	var fallback projecttype.Type
	if len(plan.Artifacts.All) > 0 {
		fallback = plan.Artifacts.All[0].ProjectType
	}

	if plan.FallbackProjectType != fallback {
		return fmt.Errorf("config-plan fallback_project_type disagrees with artifacts.all: %w", errs.ErrInvalidConfig)
	}

	containerNames := make(map[string]bool, len(plan.Containers.All))
	for index, container := range plan.Containers.All {
		if container.Name == "" || containerNames[container.Name] {
			return fmt.Errorf("config-plan containers.all[%d] has an empty or duplicate name: %w", index, errs.ErrInvalidConfig)
		}

		containerNames[container.Name] = true
	}

	if plan.Containers.HasContainers != (len(plan.Containers.All) > 0) {
		return fmt.Errorf("config-plan containers.has_containers disagrees with containers.all: %w", errs.ErrInvalidConfig)
	}

	if err := validateContainerExecution(plan.Containers.All, sources); err != nil {
		return err
	}

	sign := config.SignConfig{Method: plan.Sign.Method, Key: plan.Sign.Key, OIDCIssuer: plan.Sign.OIDCIssuer, Transparency: plan.Sign.Transparency}
	if err := sign.Validate(); err != nil {
		return err
	}

	if plan.Sign != planSign(sign) {
		return fmt.Errorf("config-plan sign has inconsistent resolved fields: %w", errs.ErrInvalidConfig)
	}

	gitSigning := config.GitSigningConfig{Method: plan.GitSigning.Method}
	if err := gitSigning.Validate(); err != nil {
		return err
	}

	if plan.GitSigning != planGitSigning(gitSigning) {
		return fmt.Errorf("config-plan git_signing has inconsistent resolved fields: %w", errs.ErrInvalidConfig)
	}

	return nil
}

func validateArtifactExecution(artifact PlannedArtifact, source config.Artifact) error {
	layers, err := config.ExpandSBOMs(artifact.SBOMs)
	if err != nil {
		return fmt.Errorf("sboms: %w: %w", err, errs.ErrInvalidConfig)
	}

	if !maps.Equal(layerSet(layers), layerSet(artifact.EffectiveSBOMs)) {
		return fmt.Errorf("effective_sboms disagrees with sboms: %w", errs.ErrInvalidConfig)
	}

	if artifact.BuildArtifactName != buildArtifactName(source) {
		return fmt.Errorf("build_artifact_name disagrees with producer: %w", errs.ErrInvalidConfig)
	}

	if artifact.BuildSBOMArtifactName != buildSBOMArtifactName(source) {
		return fmt.Errorf("build_sbom_artifact_name disagrees with producer: %w", errs.ErrInvalidConfig)
	}

	return nil
}

func validateContainerExecution(containers []PlannedContainer, artifacts []config.Artifact) error { //nolint:cyclop // bounded dependency and producer-projection checks, not native configuration validation.
	byName := make(map[string]config.Artifact, len(artifacts))
	for _, artifact := range artifacts {
		byName[artifact.Name] = artifact
	}
	// Only dependency inputs enter the pure producer derivation. Do not run
	// artifacts.yml/native validation or derive into the supplied plan's copies.
	source := config.Config{Artifacts: artifacts, Containers: make([]config.Container, 0, len(containers))}
	for index, container := range containers {
		goCount, cargoCount := 0, 0

		for depIndex, name := range container.From {
			artifact, ok := byName[name]
			if !ok {
				return fmt.Errorf("config-plan containers.all[%d].from[%d] references unknown artifact %q: %w", index, depIndex, name, errs.ErrInvalidConfig)
			}

			if config.GoArtifactBuildMode(artifact) == config.GoBuildModeArtifactFirst {
				goCount++
			}

			if config.CargoArtifactBuildMode(artifact) == config.CargoBuildModeArtifactFirst {
				cargoCount++
			}
		}

		if goCount > 1 {
			return fmt.Errorf("config-plan containers.all[%d].from has multiple artifact-first Go artifacts: %w", index, errs.ErrInvalidConfig)
		}

		if cargoCount > 1 {
			return fmt.Errorf("config-plan containers.all[%d].from has multiple artifact-first Cargo artifacts: %w", index, errs.ErrInvalidConfig)
		}

		source.Containers = append(source.Containers, config.Container{Name: container.Name, From: container.From})
	}

	if err := config.Derive(&source); err != nil {
		return fmt.Errorf("config-plan container dependencies: %w: %w", err, errs.ErrInvalidConfig)
	}

	expected := planContainers(source.Artifacts, source.Containers)
	for index, container := range containers {
		want := expected[index]
		if !slices.Equal(container.ArtifactTypes, want.ArtifactTypes) {
			return fmt.Errorf("config-plan containers.all[%d].artifact_types disagrees with from: %w", index, errs.ErrInvalidConfig)
		}

		if container.EnableAnalyzedContainerSBOM != want.EnableAnalyzedContainerSBOM {
			return fmt.Errorf("config-plan containers.all[%d].enable_analyzed_container_sbom disagrees with from: %w", index, errs.ErrInvalidConfig)
		}

		for _, slot := range []struct{ field, got, want string }{
			{"go_artifact_name", container.GoArtifactName, want.GoArtifactName},
			{"cargo_artifact_name", container.CargoArtifactName, want.CargoArtifactName},
			{"maven_artifact_name", container.MavenArtifactName, want.MavenArtifactName},
			{"npm_artifact_name", container.NPMArtifactName, want.NPMArtifactName},
			{"gradle_artifact_name", container.GradleArtifactName, want.GradleArtifactName},
		} {
			if slot.got != slot.want {
				return fmt.Errorf("config-plan containers.all[%d].%s disagrees with from: %w", index, slot.field, errs.ErrInvalidConfig)
			}
		}
	}

	return nil
}

func validateArtifactSets(sets ArtifactSets) error {
	want := newArtifactSets(sets.All)
	for _, group := range []struct {
		name      string
		got, want []PlannedArtifact
	}{
		{"maven", sets.Maven, want.Maven},
		{"npm", sets.NPM, want.NPM},
		{"gradle", sets.Gradle, want.Gradle},
		{"gradle_android", sets.GradleAndroid, want.GradleAndroid},
		{"xcode_ios", sets.XcodeIOS, want.XcodeIOS},
		{"python", sets.Python, want.Python},
		{"go", sets.Go, want.Go},
		{"cargo", sets.Cargo, want.Cargo},
		{"meta", sets.Meta, want.Meta},
		{"go_artifact_first", sets.GoArtifactFirst, want.GoArtifactFirst},
		{"go_container_first", sets.GoContainerFirst, want.GoContainerFirst},
		{"cargo_artifact_first", sets.CargoArtifactFirst, want.CargoArtifactFirst},
		{"cargo_container_first", sets.CargoContainerFirst, want.CargoContainerFirst},
		{"forge_packages", sets.ForgePackages, want.ForgePackages},
		{"maven_central", sets.MavenCentral, want.MavenCentral},
		{"google_play", sets.GooglePlay, want.GooglePlay},
		{"npmjs", sets.NPMJS, want.NPMJS},
	} {
		// Empty collections and omitted optional empty fields have the same
		// meaning. Compare the wire copies, not Go pointer/slice identity.
		if len(group.got) == 0 && len(group.want) == 0 {
			continue
		}

		gotJSON, err := json.Marshal(group.got)
		if err != nil {
			return fmt.Errorf("config-plan artifacts.%s: %w: %w", group.name, err, errs.ErrInvalidConfig)
		}

		wantJSON, err := json.Marshal(group.want)
		if err != nil {
			return fmt.Errorf("config-plan artifacts.all: %w: %w", err, errs.ErrInvalidConfig)
		}

		if !bytes.Equal(gotJSON, wantJSON) {
			return fmt.Errorf("config-plan artifacts.%s disagrees with artifacts.all: %w", group.name, errs.ErrInvalidConfig)
		}
	}

	return nil
}
