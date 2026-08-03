// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package release holds pure release-flow domain helpers — file
// discovery, SBOM/checksum policy, etc. Adapter calls and file I/O
// live in app/release.
package release

import (
	"path/filepath"
	"strings"
)

// ReleaseArtifactExtensions is the set of file extensions considered
// release artifacts. Mirrors `ci_find_release_artifacts` in.
//
//nolint:gochecknoglobals // canonical extension list — read-only.
var ReleaseArtifactExtensions = []string{
	".jar", ".tgz", ".tar.gz", ".zip", ".war",
}

// IsReleaseArtifact reports whether path looks like a release artifact:
// has one of the recognised extensions and is not a Maven "original-*.jar"
// (those are pre-shaded copies kept for diagnostics).
func IsReleaseArtifact(path string) bool {
	base := filepath.Base(path)

	if strings.HasPrefix(base, "original-") && strings.HasSuffix(base, ".jar") {
		return false
	}

	for _, ext := range ReleaseArtifactExtensions {
		if strings.HasSuffix(base, ext) {
			return true
		}
	}

	return false
}

// SBOMFilePatterns is the list of glob patterns the SBOM zip + checksum
// flows look for in the working directory.
//
//nolint:gochecknoglobals // canonical pattern list — read-only.
var SBOMFilePatterns = []string{
	"*-sbom.spdx.json",
	"*-sbom.cyclonedx.json",
}

// AnalyzedContainerSBOMPattern matches per-arch / per-container SBOMs that
// land in ./sbom-artifacts/ from publish-container.yml.
const AnalyzedContainerSBOMPattern = "*-analyzed-container-sbom.*.json"

// ChecksumsFile is the canonical filename for the SHA256 manifest.
const ChecksumsFile = "checksums.sha256"

// DefaultReleaseArtifactsDir is the cwd-relative directory release-flow
// commands scan for build outputs by default. Mirrors // $RELEASE_ARTIFACTS_DIR fallback. Keep callers using this constant
// rather than the literal string so the convention has one home.
const DefaultReleaseArtifactsDir = "./release-artifacts"

// DefaultSBOMArtifactsDir is the cwd-relative directory containing
// publish-container SBOMs (one per architecture / per container).
// Mirrors $SBOM_DIR.
const DefaultSBOMArtifactsDir = "./sbom-artifacts"

// DefaultReleaseNotesFile is the canonical filename for the markdown
// release notes consumed by `gh release create --notes-file`. CLI flag
// defaults and app-layer empty-string fallbacks both reference this
// constant so the value has one home.
const DefaultReleaseNotesFile = "release-notes.md"

// DefaultReleaseBinariesDir is the cwd-relative directory where
// per-arch extracted binaries land during the release flow (one file
// per platform, named via `container suffix-extracted-binaries`).
// CLI flag defaults and app-layer empty-string fallbacks reference
// this constant.
const DefaultReleaseBinariesDir = "release-artifacts/binaries"

// DefaultReleaseBinariesGlob is the glob auto-attached when
// DefaultReleaseBinariesDir is non-empty. Kept as a separate constant
// (not derived from DefaultReleaseBinariesDir) so the glob syntax is
// explicit and reviewable in one place.
const DefaultReleaseBinariesGlob = "release-artifacts/binaries/**"
