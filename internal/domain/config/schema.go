// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package config holds the typed schema for artifacts.yml and the pure
// validation / sbom-expansion / output-derivation logic that backs
// `reusable-ci config parse|validate`.
package config

import "github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"

// ValidProjectTypes lists every project type accepted by the
// artifacts.yml schema. Order is significant: per-ecosystem output arrays
// are emitted in this order. Excludes Auto and Unknown (detection-only values).
//
// Python is intentionally absent: the projecttype.Python constant
// exists in the codebase as a placeholder for future support, but no
// build/publish workflow is wired today. Accepting it here would
// silently produce nothing at release time, which is worse than
// failing loudly at config-parse. When the implementation lands,
// add `projecttype.Python,` back to this list — that's the only
// switch needed to flip it on.
//
//nolint:gochecknoglobals // schema enumeration — read-only and ordered.
var ValidProjectTypes = []projecttype.Type{
	projecttype.Maven,
	projecttype.NPM,
	projecttype.Gradle,
	projecttype.GradleAndroid,
	projecttype.XcodeIOS,
	projecttype.Go,
	projecttype.Cargo,
	projecttype.Meta,
}

// SBOMSupportedTypes is the set of project-types whose default `sboms`
// value is "all". Other types default to "none". Python is omitted
// for the same reason it's omitted from ValidProjectTypes above.
//
//nolint:gochecknoglobals // schema lookup table — read-only.
var SBOMSupportedTypes = map[projecttype.Type]bool{
	projecttype.Maven:         true,
	projecttype.NPM:           true,
	projecttype.Gradle:        true,
	projecttype.GradleAndroid: true,
	projecttype.Go:            true,
	projecttype.Cargo:         true,
}

// BuildType is the artefact build kind for ecosystems that distinguish
// between deployable applications and reusable libraries (currently only
// Maven). Empty / unrecognised values are treated as library by the
// publish-target filter rule: "Maven applications should not publish to
// forge-packages.".
type BuildType string

// Recognised BuildType values.
const (
	BuildTypeApplication BuildType = "application"
	BuildTypeLibrary     BuildType = "library"
)

// ValidBuildTypes lists every BuildType the schema recognises. The
// generated artifacts.yml JSON Schema renders its buildType enum from
// this slice.
//
//nolint:gochecknoglobals // schema enumeration — read-only and ordered.
var ValidBuildTypes = []BuildType{
	BuildTypeApplication,
	BuildTypeLibrary,
}

// GoBuildMode declares how reusable-ci should treat a Go artifact.
// Container-first compiles inside the project Containerfile; artifact-first
// compiles release binaries in build-go.yml before any container consumes them.
type GoBuildMode string

// Recognised GoBuildMode values.
const (
	GoBuildModeArtifactFirst  GoBuildMode = "artifact-first"
	GoBuildModeContainerFirst GoBuildMode = "container-first"
)

// CargoBuildMode declares how reusable-ci should treat a Cargo artifact.
// Container-first compiles inside the project Containerfile (today's
// behaviour, SBOM-only at build stage); artifact-first cross-compiles
// release binaries in build-cargo.yml before any container consumes them
// or before they ship as standalone CLI assets.
type CargoBuildMode string

// Recognised CargoBuildMode values.
const (
	CargoBuildModeArtifactFirst  CargoBuildMode = "artifact-first"
	CargoBuildModeContainerFirst CargoBuildMode = "container-first"
)

// PublishTarget is one of the recognised package-registry destinations.
type PublishTarget string

// Recognised PublishTarget values.
const (
	PublishMavenCentral  PublishTarget = "maven-central"
	PublishForgePackages PublishTarget = "forge-packages"
	PublishGooglePlay    PublishTarget = "google-play"
	PublishNPMJS         PublishTarget = "npmjs"
)

// ValidPublishTargets lists every PublishTarget the schema recognises.
//
//nolint:gochecknoglobals // schema enumeration — read-only and ordered.
var ValidPublishTargets = []PublishTarget{
	PublishMavenCentral,
	PublishForgePackages,
	PublishGooglePlay,
	PublishNPMJS,
}

// SBOMLayer is one of the three CISA SBOM layers.
type SBOMLayer string

// Recognised SBOMLayer values (CISA layer names).
const (
	SBOMLayerBuild             SBOMLayer = "build"
	SBOMLayerAnalyzedArtifact  SBOMLayer = "analyzed-artifact"
	SBOMLayerAnalyzedContainer SBOMLayer = "analyzed-container"
)

// ValidSBOMLayers lists the layer tokens in canonical pipeline order.
// Used by ExpandSBOMs to keep output deterministic.
//
//nolint:gochecknoglobals // schema enumeration — read-only and ordered.
var ValidSBOMLayers = []SBOMLayer{
	SBOMLayerBuild,
	SBOMLayerAnalyzedArtifact,
	SBOMLayerAnalyzedContainer,
}

// Config is the full artifacts.yml content after parsing.
type Config struct {
	Artifacts  []Artifact  `yaml:"artifacts"`
	Containers []Container `yaml:"containers,omitempty"`

	// Sign selects the signing backend for `release sign` and
	// `release sbom-zip --sign`. Empty defaults to gpg (preserves
	// the pre-cosign contract for existing repos).
	Sign SignConfig `yaml:"sign,omitempty"`

	// GitSigning selects how the release commit and tag are signed
	// (git objects), independent of Sign. Empty defaults to gpg.
	GitSigning GitSigningConfig `yaml:"git-signing,omitempty"`
}

// Artifact is one entry under the top-level `artifacts:` list. It is the
// internal representation of an artifacts.yml entry; the public JSON
// contract is pipeline.PlannedArtifact, not this type.
//
// The freeform `config:` YAML block is dispatched at parse time into
// exactly one of the typed sub-struct fields below (Maven, NPM, Gradle,
// GradleAndroid, XcodeIOS, Go, Cargo, Python) matching ProjectType, via
// strict YAML decoding — a typo in artifacts.yml (`bogus-aab: true`)
// fails at config-parse time rather than being silently ignored. See
// Artifact.UnmarshalYAML in parse.go.
type Artifact struct {
	Name                 string           `yaml:"name"`
	ProjectType          projecttype.Type `yaml:"project-type"`
	WorkingDirectory     string           `yaml:"working-directory,omitempty"`
	BuildType            BuildType        `yaml:"build-type,omitempty"`
	PublishTo            []PublishTarget  `yaml:"publish-to,omitempty"`
	SBOMs                string           `yaml:"sboms,omitempty"`
	RequireAuthorization bool             `yaml:"require-authorization,omitempty"`

	// Typed per-ecosystem config. Exactly one is non-nil after parse,
	// matching ProjectType. Always nil when ProjectType is Meta (no
	// `config:` block on meta artifacts).
	Maven         *MavenConfig         `yaml:"-"`
	NPM           *NPMConfig           `yaml:"-"`
	Gradle        *GradleConfig        `yaml:"-"`
	GradleAndroid *GradleAndroidConfig `yaml:"-"`
	XcodeIOS      *XcodeIOSConfig      `yaml:"-"`
	Go            *GoConfig            `yaml:"-"`
	Cargo         *CargoConfig         `yaml:"-"`
	Python        *PythonConfig        `yaml:"-"`

	// EffectiveSBOMs is computed from SBOMs (see ExpandSBOMs). Not present
	// in the YAML; populated during parse for downstream consumers.
	EffectiveSBOMs []SBOMLayer `yaml:"-"`
}

// Container is one entry under the top-level `containers:` list. Internal
// representation only — the public JSON contract is pipeline.PlannedContainer.
type Container struct {
	Name          string   `yaml:"name"`
	From          []string `yaml:"from,omitempty"`
	ContainerFile string   `yaml:"container-file,omitempty"`
	Context       string   `yaml:"context,omitempty"`
	Target        string   `yaml:"target,omitempty"`
	Platforms     string   `yaml:"platforms,omitempty"`
	// EnableSLSA / EnableScan use pointer-bool so the planner can
	// distinguish "unset" (nil) from "explicitly false" — both default to
	// true (per the publish-container.yml input defaults), but an explicit
	// `false` must reach the workflow as a per-container override.
	EnableSLSA *bool `yaml:"enable-slsa,omitempty"`
	EnableScan *bool `yaml:"enable-scan,omitempty"`
	// ScanSeverity narrows the trivy --severity filter (and the fail-on
	// threshold) for this container. Empty → use the workflow default
	// (CRITICAL,HIGH). Set per-container to relax / tighten the gate.
	ScanSeverity string            `yaml:"scan-severity,omitempty"`
	BuildArgs    map[string]string `yaml:"build-args,omitempty"`
	// BuildSecrets lists the GHA secret names that should be forwarded
	// to BuildKit as `--mount=type=secret,id=<lowercased-name>` mounts.
	// Unlike BuildArgs (which BuildKit records verbatim in `mode=max`
	// provenance attestations and therefore makes globally readable),
	// BuildKit secret mounts are tmpfs-bound to the RUN step and are
	// never recorded in provenance. Use this field for any secret the
	// Containerfile needs at build time (private package registry tokens,
	// API keys consumed during `RUN`, etc.); use BuildArgs only for
	// public build parameters (toolchain versions, feature flags).
	//
	// Each entry must be a valid env-var identifier ([A-Z_][A-Z0-9_]*).
	// The caller workflow packs all listed secrets into the
	// REUSABLE_CI_BUILD_SECRETS_JSON envelope; publish-container.yml
	// unpacks at build time. See docs/artifacts-reference.md for the
	// full caller-side recipe.
	BuildSecrets []string          `yaml:"build-secrets,omitempty"`
	Extract      *ContainerExtract `yaml:"extract,omitempty"`

	// Computed during parse. Not in YAML.
	ArtifactTypes               []projecttype.Type `yaml:"-"`
	EnableAnalyzedContainerSBOM bool               `yaml:"-"`
	BuildArgsString             string             `yaml:"-"`
	GoArtifactName              string             `yaml:"-"`
	CargoArtifactName           string             `yaml:"-"`
}

// ContainerExtract holds optional extract-time byproducts from a
// container build.
type ContainerExtract struct {
	Binary *ContainerExtractBinary `yaml:"binary,omitempty"`
}

// ContainerExtractBinary describes the stage and names for compiled
// binaries extracted from a container-first build.
type ContainerExtractBinary struct {
	Target string   `yaml:"target,omitempty"`
	Names  []string `yaml:"names,omitempty"`
}
