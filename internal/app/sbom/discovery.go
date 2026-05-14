// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package sbom

// Artifact-discovery helpers per project type. Each function mirrors a
// `find ...` expression in the original bash scripts and returns the
// list of paths the layer-orchestration code in layers.go feeds into
// syft. The filesystem walk primitives live in walk.go.

import (
	"strings"

	domainrelease "github.com/diggsweden/reusable-ci/internal/domain/release"
)

func findMavenJARs(ws workspace) []string {
	// Search ./release-artifacts → ./release-artifacts/target → ./target
	jarFilter := func(name string) bool {
		if !strings.HasSuffix(name, ".jar") {
			return false
		}
		if strings.HasSuffix(name, "-sources.jar") ||
			strings.HasSuffix(name, "-javadoc.jar") ||
			strings.HasSuffix(name, "-tests.jar") ||
			strings.HasPrefix(name, "original-") {
			return false
		}
		return true
	}
	for _, root := range []string{domainrelease.DefaultReleaseArtifactsDir, domainrelease.DefaultReleaseArtifactsDir + "/target", "target"} {
		if hits := walkMatching(ws, root, jarFilter); len(hits) > 0 {
			return hits
		}
	}
	return nil
}

func findNPMTarballs(ws workspace) []string {
	for _, root := range []string{domainrelease.DefaultReleaseArtifactsDir, "."} {
		hits := walkMatching(ws, root, func(name string) bool {
			return strings.HasSuffix(name, ".tgz")
		})
		if len(hits) > 0 {
			return hits
		}
	}
	return nil
}

func findGradleJARs(ws workspace, name string) []string {
	prefix := name + "-"
	return walkMatching(ws, "build/libs", func(n string) bool {
		if !strings.HasSuffix(n, ".jar") {
			return false
		}
		if strings.HasSuffix(n, "-sources.jar") || strings.HasSuffix(n, "-javadoc.jar") {
			return false
		}
		return strings.HasPrefix(n, prefix)
	})
}

func findGoExecutables(ws workspace, name string) []string {
	hits := walkExecutable(ws, domainrelease.DefaultReleaseArtifactsDir)
	if len(hits) > 0 {
		return hits
	}
	// Fallback: cwd, by exact name (bash also accepts the binary name only).
	return walkMatching(ws, ".", func(n string) bool { return n == name })
}

func findCargoExecutables(ws workspace) []string {
	hits := walkExecutable(ws, domainrelease.DefaultReleaseArtifactsDir)
	if len(hits) > 0 {
		return hits
	}
	return walkExecutableNoDebugInfo(ws, "target/release")
}

func findPythonWheels(ws workspace) []string {
	for _, root := range []string{domainrelease.DefaultReleaseArtifactsDir, "dist"} {
		hits := walkMatching(ws, root, func(n string) bool {
			return strings.HasSuffix(n, ".whl") || strings.HasSuffix(n, ".tar.gz")
		})
		if len(hits) > 0 {
			return hits
		}
	}
	return nil
}
