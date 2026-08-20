// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline

// Target keys carried inside stage-plan and stage-result JSON envelopes.
//
// Each constant equals a JSON tag on one of the stage-target structs
// in this package (ReleaseBuildTargets, ReleasePublishTargets,
// DevBuildTargets, DevPublishTargets, ReleasePrepareTargets,
// PRQualityStagePlan). The summary consumers in
// internal/app/summary use these constants to look up per-target
// results — referencing the constant rather than hand-typing the
// string keeps the producer (struct tag) and consumer (lookup) wired
// to the same source of truth.
//
// Drift between a constant and the matching JSON tag is caught by
// TestTargetKeys_MatchStructTags in targetkeys_test.go.
//
// Naming follows Target<Field> — the field name on the matching
// stage-target struct.
const (
	// Build-stage targets (release + dev share these names).
	// Go and Cargo are the artefact-first cross-compile jobs; their
	// container-first counterparts live in the publish-stage block below.
	TargetMaven         = "maven"
	TargetNPM           = "npm"
	TargetGradle        = "gradle"
	TargetGradleAndroid = "gradle_android"
	TargetXcodeIOS      = "xcode_ios"
	TargetGo            = "go"
	TargetCargo         = "cargo"

	// Prepare-stage targets (release only).
	TargetVersionBump = "version_bump"

	// Publish-stage targets (release + dev). The *ContainerFirst pair
	// holds the artefacts whose Build SBOM ships from sbom-{lang}.yml at
	// publish stage. The *ArtifactFirst pair (dev only today) carries
	// the artefact-first set so dev-publish can disambiguate without
	// re-deriving build-mode from the planned-artifact list.
	TargetGitHubPackages       = "github_packages"
	TargetMavenCentral         = "maven_central"
	TargetGitHubPackagesGradle = "github_packages_gradle"
	TargetMavenCentralGradle   = "maven_central_gradle"
	TargetGooglePlay           = "google_play"
	TargetContainers           = "containers"
	TargetCargoContainerFirst  = "cargo_container_first"
	TargetGoContainerFirst     = "go_container_first"
	TargetCargoArtifactFirst   = "cargo_artifact_first" // dev publish only
	TargetGoArtifactFirst      = "go_artifact_first"    // dev publish only
	TargetSBOM                 = "sbom"                 // dev publish only

	// Quality-stage targets (PR).
	TargetNanolinter = "nanolinter"
	TargetSwift      = "swift"
)
