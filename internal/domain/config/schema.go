// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package config holds the typed schema for artifacts.yml and the pure
// validation / sbom-expansion / output-derivation logic that backs
// `reusable-ci config parse|validate`.
//
// Mirrors scripts/config/parse-artifacts-config.sh — same input contract,
// same emission shape, same validation rules.
package config

import "github.com/diggsweden/reusable-ci/internal/domain/projecttype"

// ValidProjectTypes lists every project type accepted by the
// artifacts.yml schema. Order is significant: it matches the bash
// VALID_PROJECT_TYPES list so per-ecosystem output arrays come out in
// the same order. Excludes Auto and Unknown (detection-only values).
var ValidProjectTypes = []projecttype.Type{
	projecttype.Maven,
	projecttype.NPM,
	projecttype.Gradle,
	projecttype.GradleAndroid,
	projecttype.XcodeIOS,
	projecttype.Python,
	projecttype.Go,
	projecttype.Cargo,
	projecttype.Meta,
}

// SBOMSupportedTypes is the set of project-types whose default `sboms`
// value is "all". Other types default to "none".
var SBOMSupportedTypes = map[projecttype.Type]bool{
	projecttype.Maven:         true,
	projecttype.NPM:           true,
	projecttype.Gradle:        true,
	projecttype.GradleAndroid: true,
	projecttype.Python:        true,
	projecttype.Go:            true,
	projecttype.Cargo:         true,
}

// BuildType is the artefact build kind for ecosystems that distinguish
// between deployable applications and reusable libraries (currently only
// Maven). Empty / unrecognised values are treated as library by the
// publish-target filter rule: "Maven applications should not publish to
// github-packages."
type BuildType string

const (
	BuildTypeApplication BuildType = "application"
	BuildTypeLibrary     BuildType = "library"
)

// PublishTarget is one of the recognised package-registry destinations.
type PublishTarget string

const (
	PublishMavenCentral   PublishTarget = "maven-central"
	PublishGitHubPackages PublishTarget = "github-packages"
	PublishGooglePlay     PublishTarget = "google-play"
	PublishNPMJS          PublishTarget = "npmjs"
)

// ValidPublishTargets lists every PublishTarget the schema recognises.
var ValidPublishTargets = []PublishTarget{
	PublishMavenCentral,
	PublishGitHubPackages,
	PublishGooglePlay,
	PublishNPMJS,
}

// SBOMLayer is one of the three CISA SBOM layers.
type SBOMLayer string

const (
	SBOMLayerBuild             SBOMLayer = "build"
	SBOMLayerAnalyzedArtifact  SBOMLayer = "analyzed-artifact"
	SBOMLayerAnalyzedContainer SBOMLayer = "analyzed-container"
)

// ValidSBOMLayers lists the layer tokens in canonical pipeline order.
// Used by ExpandSBOMs to keep output deterministic.
var ValidSBOMLayers = []SBOMLayer{
	SBOMLayerBuild,
	SBOMLayerAnalyzedArtifact,
	SBOMLayerAnalyzedContainer,
}

// Config is the full artifacts.yml content after parsing.
type Config struct {
	Artifacts  []Artifact  `yaml:"artifacts"`
	Containers []Container `yaml:"containers,omitempty"`
}

// Artifact is one entry under the top-level `artifacts:` list.
//
// JSON tags mirror the kebab-case keys the bash emits via `yq -o=json`
// so the JSON arrays from `config parse-artifacts` match downstream
// consumers byte-for-byte.
type Artifact struct {
	Name                 string           `yaml:"name"                                 json:"name"`
	ProjectType          projecttype.Type `yaml:"project-type"                         json:"project-type"`
	WorkingDirectory     string           `yaml:"working-directory,omitempty"          json:"working-directory,omitempty"`
	BuildType            BuildType        `yaml:"build-type,omitempty"                 json:"build-type,omitempty"`
	PublishTo            []PublishTarget  `yaml:"publish-to,omitempty"                 json:"publish-to,omitempty"`
	SBOMs                string           `yaml:"sboms,omitempty"                      json:"sboms,omitempty"`
	RequireAuthorization bool             `yaml:"require-authorization,omitempty"      json:"require-authorization,omitempty"`
	Config               map[string]any   `yaml:"config,omitempty"                     json:"config,omitempty"`

	// EffectiveSBOMs is computed from SBOMs (see ExpandSBOMs). Not present
	// in the YAML; populated during parse for downstream consumers.
	EffectiveSBOMs []SBOMLayer `yaml:"-" json:"effective-sboms,omitempty"`
}

// Container is one entry under the top-level `containers:` list.
type Container struct {
	Name          string            `yaml:"name"                       json:"name"`
	From          []string          `yaml:"from,omitempty"             json:"from,omitempty"`
	ContainerFile string            `yaml:"container-file,omitempty"   json:"container-file,omitempty"`
	Context       string            `yaml:"context,omitempty"          json:"context,omitempty"`
	Target        string            `yaml:"target,omitempty"           json:"target,omitempty"`
	Platforms     string            `yaml:"platforms,omitempty"        json:"platforms,omitempty"`
	EnableSLSA    bool              `yaml:"enable-slsa,omitempty"      json:"enable-slsa,omitempty"`
	EnableScan    bool              `yaml:"enable-scan,omitempty"      json:"enable-scan,omitempty"`
	BuildArgs     map[string]string `yaml:"build-args,omitempty"       json:"build-args,omitempty"`
	Extract       *ContainerExtract `yaml:"extract,omitempty"          json:"extract,omitempty"`

	// Computed during parse. Not in YAML.
	ArtifactTypes               []projecttype.Type `yaml:"-" json:"artifact-types,omitempty"`
	EnableAnalyzedContainerSBOM bool               `yaml:"-" json:"enable-analyzed-container-sbom"`
	BuildArgsString             string             `yaml:"-" json:"build-args-string"`
}

// ContainerExtract holds optional extract-time byproducts from a
// container build.
type ContainerExtract struct {
	Binary *ContainerExtractBinary `yaml:"binary,omitempty" json:"binary,omitempty"`
}

// ContainerExtractBinary describes the stage and names for compiled
// binaries extracted from a container-first build.
type ContainerExtractBinary struct {
	Target string   `yaml:"target,omitempty" json:"target,omitempty"`
	Names  []string `yaml:"names,omitempty"  json:"names,omitempty"`
}
