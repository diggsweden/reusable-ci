// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package pipeline contains typed, provider-neutral pipeline contracts.
package pipeline

import (
	"cmp"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	domainrelease "github.com/diggsweden/reusable-ci/internal/domain/release"
)

// ConfigPlanVersion is the current config-plan contract version.
const ConfigPlanVersion = 1

// ConfigPlan is the typed contract emitted by `config parse-artifacts` for
// later stage planning. It intentionally uses snake_case JSON and does not
// reuse config.Artifact/config.Container, whose JSON tags preserve legacy
// workflow array shapes.
type ConfigPlan struct {
	Version                 int              `json:"version"`
	Artifacts               ArtifactSets     `json:"artifacts"`
	Containers              ContainerSets    `json:"containers"`
	AnyRequireAuthorization bool             `json:"any_require_authorization"`
	PipelineSBOMs           string           `json:"pipeline_sboms"`
	FallbackProjectType     projecttype.Type `json:"fallback_project_type,omitempty"`
	Sign                    PlannedSign      `json:"sign"`
}

// PlannedSign is the resolved signing-backend choice. Always present in
// the plan (Method defaults to gpg when artifacts.yml has no sign:
// block, so downstream workflow code can read sign.method
// unconditionally without nil checks). RequiresIDToken is precomputed
// for the orchestrator workflow's `permissions:` decision — keyless
// Sigstore is the only method that needs id-token: write.
type PlannedSign struct {
	Method           domainrelease.SignMethod `json:"method"`
	Key              string                   `json:"key,omitempty"`
	OIDCIssuer       string                   `json:"oidc_issuer,omitempty"`
	RequiresIDToken  bool                     `json:"requires_id_token"`
}

// ArtifactSets groups artifacts by project type, Go build mode, and supported
// publish target.
type ArtifactSets struct {
	All              []PlannedArtifact `json:"all"`
	Maven            []PlannedArtifact `json:"maven"`
	NPM              []PlannedArtifact `json:"npm"`
	Gradle           []PlannedArtifact `json:"gradle"`
	GradleAndroid    []PlannedArtifact `json:"gradle_android"`
	XcodeIOS         []PlannedArtifact `json:"xcode_ios"`
	Python           []PlannedArtifact `json:"python"`
	Go               []PlannedArtifact `json:"go"`
	Cargo               []PlannedArtifact `json:"cargo"`
	Meta                []PlannedArtifact `json:"meta"`
	GoArtifactFirst     []PlannedArtifact `json:"go_artifact_first"`
	GoContainerFirst    []PlannedArtifact `json:"go_container_first"`
	CargoArtifactFirst  []PlannedArtifact `json:"cargo_artifact_first"`
	CargoContainerFirst []PlannedArtifact `json:"cargo_container_first"`
	GitHubPackages   []PlannedArtifact `json:"github_packages"`
	MavenCentral     []PlannedArtifact `json:"maven_central"`
	GooglePlay       []PlannedArtifact `json:"google_play"`
	NPMJS            []PlannedArtifact `json:"npmjs"`
}

// PlannedArtifact is the plan-facing artifact shape.
//
// Per-ecosystem config is exposed via the typed sub-struct fields
// below. Exactly one is non-nil per artifact, matching ProjectType.
// Workflow consumers read them as `matrix.artifact.go.binary_name`,
// `matrix.artifact.gradle_android.include_aab`, etc. — the JSON tags
// (snake_case) align with GitHub Actions expression syntax.
type PlannedArtifact struct {
	Name                  string                 `json:"name"`
	ProjectType           projecttype.Type       `json:"project_type"`
	WorkingDirectory      string                 `json:"working_directory"`
	BuildType             config.BuildType       `json:"build_type,omitempty"`
	PublishTo             []config.PublishTarget `json:"publish_to,omitempty"`
	SBOMs                 string                 `json:"sboms,omitempty"`
	EffectiveSBOMs        []config.SBOMLayer     `json:"effective_sboms,omitempty"`
	RequireAuthorization  bool                   `json:"require_authorization,omitempty"`
	GoBuildMode           config.GoBuildMode     `json:"go_build_mode,omitempty"`
	CargoBuildMode        config.CargoBuildMode  `json:"cargo_build_mode,omitempty"`
	BuildArtifactName     string                 `json:"build_artifact_name,omitempty"`
	BuildSBOMArtifactName string                 `json:"build_sbom_artifact_name,omitempty"`

	Maven         *config.MavenConfig         `json:"maven,omitempty"`
	NPM           *config.NPMConfig           `json:"npm,omitempty"`
	Gradle        *config.GradleConfig        `json:"gradle,omitempty"`
	GradleAndroid *config.GradleAndroidConfig `json:"gradle_android,omitempty"`
	XcodeIOS      *config.XcodeIOSConfig      `json:"xcode_ios,omitempty"`
	Go            *config.GoConfig            `json:"go,omitempty"`
	Cargo         *config.CargoConfig         `json:"cargo,omitempty"`
	Python        *config.PythonConfig        `json:"python,omitempty"`
}

// ContainerSets groups planned containers and carries the common has-containers
// gate currently computed in workflow shell.
type ContainerSets struct {
	All           []PlannedContainer `json:"all"`
	HasContainers bool               `json:"has_containers"`
}

// PlannedContainer is the plan-facing container shape.
type PlannedContainer struct {
	Name                        string             `json:"name"`
	From                        []string           `json:"from,omitempty"`
	ArtifactTypes               []projecttype.Type `json:"artifact_types,omitempty"`
	ContainerFile               string             `json:"container_file"`
	Context                     string             `json:"context"`
	Target                      string             `json:"target,omitempty"`
	Platforms                   string             `json:"platforms"`
	EnableAnalyzedContainerSBOM bool               `json:"enable_analyzed_container_sbom"`
	EnableSLSA                  bool               `json:"enable_slsa"`
	EnableScan                  bool               `json:"enable_scan"`
	ScanSeverity                string             `json:"scan_severity,omitempty"`
	BuildSecrets                []string           `json:"build_secrets,omitempty"`
	BuildArgs                   map[string]string  `json:"build_args,omitempty"`
	BuildArgsString             string             `json:"build_args_string,omitempty"`
	GoArtifactName              string             `json:"go_artifact_name,omitempty"`
	CargoArtifactName           string             `json:"cargo_artifact_name,omitempty"`
	MavenArtifactName           string             `json:"maven_artifact_name,omitempty"`
	NPMArtifactName             string             `json:"npm_artifact_name,omitempty"`
	GradleArtifactName          string             `json:"gradle_artifact_name,omitempty"`
	ExtractBinaryTarget         string             `json:"extract_binary_target,omitempty"`
	ExtractBinaryNames          []string           `json:"extract_binary_names,omitempty"`
}

// NewConfigPlan builds a plan from an already validated and derived config.
func NewConfigPlan(cfg *config.Config) ConfigPlan {
	if cfg == nil {
		return ConfigPlan{Version: ConfigPlanVersion}
	}

	artifacts := planArtifacts(cfg.Artifacts)
	containers := planContainers(cfg.Artifacts, cfg.Containers)

	plan := ConfigPlan{
		Version: ConfigPlanVersion,
		Artifacts: ArtifactSets{
			All:           artifacts,
			Maven:         filterArtifacts(artifacts, func(a PlannedArtifact) bool { return a.ProjectType == projecttype.Maven }),
			NPM:           filterArtifacts(artifacts, func(a PlannedArtifact) bool { return a.ProjectType == projecttype.NPM }),
			Gradle:        filterArtifacts(artifacts, func(a PlannedArtifact) bool { return a.ProjectType == projecttype.Gradle }),
			GradleAndroid: filterArtifacts(artifacts, func(a PlannedArtifact) bool { return a.ProjectType == projecttype.GradleAndroid }),
			XcodeIOS:      filterArtifacts(artifacts, func(a PlannedArtifact) bool { return a.ProjectType == projecttype.XcodeIOS }),
			Python:        filterArtifacts(artifacts, func(a PlannedArtifact) bool { return a.ProjectType == projecttype.Python }),
			Go:            filterArtifacts(artifacts, func(a PlannedArtifact) bool { return a.ProjectType == projecttype.Go }),
			Cargo:         filterArtifacts(artifacts, func(a PlannedArtifact) bool { return a.ProjectType == projecttype.Cargo }),
			Meta:          filterArtifacts(artifacts, func(a PlannedArtifact) bool { return a.ProjectType == projecttype.Meta }),
			GoArtifactFirst: filterArtifacts(artifacts, func(a PlannedArtifact) bool {
				return a.ProjectType == projecttype.Go && a.GoBuildMode == config.GoBuildModeArtifactFirst
			}),
			GoContainerFirst: filterArtifacts(artifacts, func(a PlannedArtifact) bool {
				return a.ProjectType == projecttype.Go && a.GoBuildMode == config.GoBuildModeContainerFirst
			}),
			CargoArtifactFirst: filterArtifacts(artifacts, func(a PlannedArtifact) bool {
				return a.ProjectType == projecttype.Cargo && a.CargoBuildMode == config.CargoBuildModeArtifactFirst
			}),
			CargoContainerFirst: filterArtifacts(artifacts, func(a PlannedArtifact) bool {
				return a.ProjectType == projecttype.Cargo && a.CargoBuildMode == config.CargoBuildModeContainerFirst
			}),
			GitHubPackages: filterArtifactsByPublishTarget(artifacts, config.PublishGitHubPackages),
			MavenCentral:   filterArtifactsByPublishTarget(artifacts, config.PublishMavenCentral),
			GooglePlay:     filterArtifactsByPublishTarget(artifacts, config.PublishGooglePlay),
			NPMJS:          filterArtifactsByPublishTarget(artifacts, config.PublishNPMJS),
		},
		Containers: ContainerSets{
			All:           containers,
			HasContainers: len(containers) > 0,
		},
		AnyRequireAuthorization: config.AnyRequireAuthorization(cfg.Artifacts),
		PipelineSBOMs:           config.PipelineSBOMs(cfg.Artifacts),
		Sign:                    planSign(cfg.Sign),
	}
	if len(artifacts) > 0 {
		plan.FallbackProjectType = artifacts[0].ProjectType
	}

	return plan
}

// planSign resolves the signing block. The Method always carries a
// concrete value in the output (gpg when artifacts.yml omits the
// block), so the orchestrator workflow can read it without branching
// on emptiness. RequiresIDToken precomputes the "do we need
// `id-token: write` permission?" decision for the caller workflow —
// only keyless Sigstore needs it; gpg signs from a secret and kms
// authenticates to the KMS provider out-of-band.
func planSign(sign config.SignConfig) PlannedSign {
	method := sign.EffectiveMethod()

	return PlannedSign{
		Method:          method,
		Key:             sign.Key,
		OIDCIssuer:      sign.OIDCIssuer,
		RequiresIDToken: method == domainrelease.SignMethodSigstore,
	}
}

func planArtifacts(in []config.Artifact) []PlannedArtifact {
	out := make([]PlannedArtifact, 0, len(in))
	for _, a := range in { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		out = append(out, PlannedArtifact{
			Name:                  a.Name,
			ProjectType:           a.ProjectType,
			WorkingDirectory:      cmp.Or(a.WorkingDirectory, "."),
			BuildType:             a.BuildType,
			PublishTo:             append([]config.PublishTarget(nil), a.PublishTo...),
			SBOMs:                 a.SBOMs,
			EffectiveSBOMs:        append([]config.SBOMLayer(nil), a.EffectiveSBOMs...),
			RequireAuthorization:  a.RequireAuthorization,
			GoBuildMode:           config.GoArtifactBuildMode(a),
			CargoBuildMode:        config.CargoArtifactBuildMode(a),
			BuildArtifactName:     buildArtifactName(a),
			BuildSBOMArtifactName: buildSBOMArtifactName(a),

			Maven:         a.Maven,
			NPM:           a.NPM,
			Gradle:        a.Gradle,
			GradleAndroid: a.GradleAndroid,
			XcodeIOS:      a.XcodeIOS,
			Go:            a.Go,
			Cargo:         a.Cargo,
			Python:        a.Python,
		})
	}

	return out
}

//nolint:cyclop // plans container builds with one branch per (project type, multi-arch, platform) combination.
func planContainers(artifacts []config.Artifact, containers []config.Container) []PlannedContainer {
	byName := make(map[string]config.Artifact, len(artifacts))
	for _, a := range artifacts {
		byName[a.Name] = a
	}

	out := make([]PlannedContainer, 0, len(containers))
	for _, c := range containers { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		pc := PlannedContainer{
			Name:                        c.Name,
			From:                        append([]string(nil), c.From...),
			ArtifactTypes:               append([]projecttype.Type(nil), c.ArtifactTypes...),
			ContainerFile:               cmp.Or(c.ContainerFile, "Containerfile"),
			Context:                     cmp.Or(c.Context, "."),
			Target:                      c.Target,
			Platforms:                   cmp.Or(c.Platforms, "linux/amd64"),
			EnableAnalyzedContainerSBOM: c.EnableAnalyzedContainerSBOM,
			EnableSLSA:                  c.EnableSLSAEffective(),
			EnableScan:                  c.EnableScanEffective(),
			ScanSeverity:                c.ScanSeverity,
			BuildSecrets:                append([]string(nil), c.BuildSecrets...),
			BuildArgs:                   cloneStringMap(c.BuildArgs),
			BuildArgsString:             c.BuildArgsString,
			GoArtifactName:              c.GoArtifactName,
			CargoArtifactName:           c.CargoArtifactName,
		}
		for _, dep := range c.From {
			a, ok := byName[dep] //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			if !ok {
				continue
			}

			switch a.ProjectType {
			case projecttype.Maven:
				if pc.MavenArtifactName == "" {
					pc.MavenArtifactName = buildArtifactName(a)
				}
			case projecttype.NPM:
				if pc.NPMArtifactName == "" {
					pc.NPMArtifactName = buildArtifactName(a)
				}
			case projecttype.Gradle:
				if pc.GradleArtifactName == "" {
					pc.GradleArtifactName = buildArtifactName(a)
				}
			default:
				// Other types (Go, Cargo, Python, GradleAndroid, XcodeIOS,
				// Meta, Auto, Unknown) don't contribute to the container's
				// build-artifact-name slots — they're handled elsewhere
				// in the plan (Go via GoArtifactName, etc.).
			}
		}

		if c.Extract != nil && c.Extract.Binary != nil {
			pc.ExtractBinaryTarget = c.Extract.Binary.Target
			pc.ExtractBinaryNames = append([]string(nil), c.Extract.Binary.Names...)
		}

		out = append(out, pc)
	}

	return out
}

func buildArtifactName(art config.Artifact) string {
	switch art.ProjectType {
	case projecttype.Maven, projecttype.NPM, projecttype.Gradle:
		return domainrelease.ResolveArtifactNames(art.ProjectType, art.Name).BuildArtifact
	case projecttype.Go:
		return artifactFirstGoName(art)
	case projecttype.Cargo:
		return artifactFirstCargoName(art)
	case projecttype.GradleAndroid:
		return androidReleaseAABName(art)
	case projecttype.XcodeIOS:
		if !art.EnableCodeSigning() {
			return art.Name + "-archive"
		}

		return art.Name
	default:
		// Python, Meta, Auto, Unknown: no build-artifact name (Python
		// produces SBOMs only; Meta is a pure orchestration marker).
		return ""
	}
}

// artifactFirstGoName returns the build-stage artefact name for a Go
// artefact, empty when the artefact uses container-first build mode.
func artifactFirstGoName(art config.Artifact) string {
	if config.GoArtifactBuildMode(art) != config.GoBuildModeArtifactFirst {
		return ""
	}

	return domainrelease.ResolveArtifactNames(art.ProjectType, art.Name).BuildArtifact
}

// artifactFirstCargoName mirrors artifactFirstGoName for Cargo: only
// artefact-first cargo produces a binary upload at build-stage.
// Container-first cargo's binary stays inside the container build and
// (optionally) ships via extract.binary, not as a build-artifact.
func artifactFirstCargoName(art config.Artifact) string {
	if config.CargoArtifactBuildMode(art) != config.CargoBuildModeArtifactFirst {
		return ""
	}

	return domainrelease.ResolveArtifactNames(art.ProjectType, art.Name).BuildArtifact
}

// androidReleaseAABName returns the AAB upload name when both
// IncludeAAB and the release build-type are enabled. Empty otherwise.
// IncludeAAB defaults to true (handled by the typed accessor on
// Artifact); BuildTypes defaults to "debug,release", so an explicit
// "debug"-only value drops the release artifact.
func androidReleaseAABName(art config.Artifact) string {
	if !art.IncludeAAB() {
		return ""
	}

	if !strings.Contains(art.AndroidBuildTypes(), "release") {
		return ""
	}

	return art.Name
}

func buildSBOMArtifactName(a config.Artifact) string {
	switch a.ProjectType {
	case projecttype.Maven, projecttype.NPM, projecttype.Gradle, projecttype.Go, projecttype.Cargo:
		return domainrelease.ResolveArtifactNames(a.ProjectType, a.Name).SBOMArtifact
	case projecttype.GradleAndroid:
		return artifactUploadName(a.Name, "gradle-android-build-sbom", "sbom")
	default:
		// XcodeIOS, Python, Meta, Auto, Unknown: no build-SBOM artifact
		// (Xcode flows produce IPA/archive only; Python and Meta are
		// outside the SBOM pipeline).
		return ""
	}
}

func artifactUploadName(name, fallback, suffix string) string {
	if name == "" {
		return fallback
	}

	return name + "-" + suffix
}

func filterArtifacts(in []PlannedArtifact, keep func(PlannedArtifact) bool) []PlannedArtifact {
	out := make([]PlannedArtifact, 0, len(in))
	for _, a := range in {
		if keep(a) {
			out = append(out, a)
		}
	}

	return out
}

func filterArtifactsByPublishTarget(in []PlannedArtifact, target config.PublishTarget) []PlannedArtifact {
	return filterArtifacts(in, func(a PlannedArtifact) bool { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		for _, t := range a.PublishTo {
			if t == target && config.SupportedPublishTarget(config.Artifact{
				Name:        a.Name,
				ProjectType: a.ProjectType,
				BuildType:   a.BuildType,
			}, target) {
				if target == config.PublishGitHubPackages && a.ProjectType == projecttype.Maven && a.BuildType == config.BuildTypeApplication {
					return false
				}

				return true
			}
		}

		return false
	})
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}

	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}

	return out
}
