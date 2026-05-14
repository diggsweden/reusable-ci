// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release

import (
	"path"

	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

// ArtifactNamePair is the (build, sbom) pair of upload-artifact names
// that the release-build stage hands to publish/release stages.
type ArtifactNamePair struct {
	BuildArtifact string
	SBOMArtifact  string
}

// ResolveArtifactNames returns the canonical upload-artifact name pair
// for a project type. The optional artifactName is honoured for gradle
// and cargo (matrix dispatch when artifacts.yml supplies a name) and
// ignored everywhere else where the upload name is fixed.
//
// Mirrors scripts/release/resolve-artifact-name.sh.
func ResolveArtifactNames(pt projecttype.Type, artifactName string) ArtifactNamePair {
	switch pt {
	case projecttype.Maven:
		return ArtifactNamePair{"maven-build-artifacts", "maven-build-sbom"}
	case projecttype.NPM:
		return ArtifactNamePair{"npm-build-artifacts", "npm-build-sbom"}
	case projecttype.Gradle:
		if artifactName != "" {
			return ArtifactNamePair{
				BuildArtifact: artifactName,
				SBOMArtifact:  artifactName + "-sbom",
			}
		}
		return ArtifactNamePair{"gradle-build-artifacts", "gradle-build-sbom"}
	case projecttype.Python:
		return ArtifactNamePair{"python-build-artifacts", "python-build-sbom"}
	case projecttype.Go:
		return ArtifactNamePair{"go-build-artifacts", "go-build-sbom"}
	case projecttype.Cargo:
		// cargo emits SBOM only — name and sbom-name match. The build is
		// produced inside the Containerfile during publish-stage.
		if artifactName != "" {
			n := artifactName + "-cargo-build-sbom"
			return ArtifactNamePair{n, n}
		}
		return ArtifactNamePair{"cargo-build-sbom", "cargo-build-sbom"}
	}
	return ArtifactNamePair{"build-artifacts", "build-sbom"}
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
