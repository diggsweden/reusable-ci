// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"path"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// ArtifactNamePair is the (build, sbom) pair of upload-artifact names
// that the release-build stage hands to publish/release stages.
type ArtifactNamePair struct {
	BuildArtifact string
	SBOMArtifact  string
}

// ResolveArtifactNames returns the canonical upload-artifact name pair
// for a project type. The optional artifactName is the logical artifact name
// from artifacts.yml; matrix-safe upload names append the ecosystem suffix.
//
//nolint:cyclop // name resolution: one branch per project type's filename convention.
func ResolveArtifactNames(pt projecttype.Type, artifactName string) ArtifactNamePair {
	switch pt {
	case projecttype.Maven:
		if artifactName != "" {
			return ArtifactNamePair{artifactName + "-build-artifacts", artifactName + "-build-sbom"}
		}

		return ArtifactNamePair{"maven-build-artifacts", "maven-build-sbom"}
	case projecttype.NPM:
		if artifactName != "" {
			return ArtifactNamePair{artifactName + "-build-artifacts", artifactName + "-build-sbom"}
		}

		return ArtifactNamePair{"npm-build-artifacts", "npm-build-sbom"}
	case projecttype.Gradle:
		if artifactName != "" {
			return ArtifactNamePair{
				BuildArtifact: artifactName + "-build-artifacts",
				SBOMArtifact:  artifactName + "-build-sbom",
			}
		}

		return ArtifactNamePair{"gradle-build-artifacts", "gradle-build-sbom"}
	case projecttype.Python:
		return ArtifactNamePair{"python-build-artifacts", "python-build-sbom"}
	case projecttype.Go:
		if artifactName != "" {
			return ArtifactNamePair{
				BuildArtifact: artifactName + "-go-build-artifacts",
				SBOMArtifact:  artifactName + "-go-build-sbom",
			}
		}

		return ArtifactNamePair{"go-build-artifacts", "go-build-sbom"}
	case projecttype.Cargo:
		// Symmetric with Go: artifact-first cargo produces real binaries
		// uploaded as `<name>-cargo-build-artifacts`; the inline Build SBOM
		// uploads as `<name>-cargo-build-sbom`. Container-first cargo also
		// uses the SBOM name (sbom-cargo.yml emits it at publish stage), but
		// has no BuildArtifact — the planner gates that via buildArtifactName.
		if artifactName != "" {
			return ArtifactNamePair{
				BuildArtifact: artifactName + "-cargo-build-artifacts",
				SBOMArtifact:  artifactName + "-cargo-build-sbom",
			}
		}

		return ArtifactNamePair{"cargo-build-artifacts", "cargo-build-sbom"}
	default:
		// GradleAndroid / XcodeIOS / Meta / Auto / Unknown: fall through
		// to the generic default name pair. Android and Xcode flows have
		// their own naming (AAB/IPA) computed elsewhere in the planner.
		return ArtifactNamePair{"build-artifacts", "build-sbom"}
	}
}

// ProjectNameFromRepo returns ARTIFACT_NAME when set, otherwise the
// last path segment of the repository (basename). Used by release-
// metadata resolution to derive the canonical project name.
func ProjectNameFromRepo(artifactName, repository string) string {
	if artifactName != "" {
		return artifactName
	}

	return path.Base(repository)
}
