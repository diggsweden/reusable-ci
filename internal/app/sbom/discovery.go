// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom

// Artifact-discovery helpers per project type. Each function returns
// the list of paths the layer-orchestration code in layers.go feeds
// into syft. The filesystem walk primitives live in walk.go.

import (
	"strings"

	domainrelease "github.com/diggsweden/reusable-ci/internal/domain/release"
)

// distRoot is the conventional Cargo / Python / generic-CLI binary
// release output directory ("dist/"). reusable-ci's own builders use
// release-artifacts/ instead, but the discovery code accepts both so
// adopters who hand-roll the artefact upload step can still wire up
// SBOM generation.
const distRoot = "dist"

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

	return walkMatching(ws, "build/libs", func(n string) bool { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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
	// GitHub artifact downloads do not preserve executable bits. Match the
	// Go release/extraction names first, then keep the executable-bit fallback
	// for local/direct runs.
	for _, root := range []string{domainrelease.DefaultReleaseArtifactsDir, distRoot} {
		hits := walkMatching(ws, root, func(n string) bool {
			return isGoBinaryName(n, name)
		})
		if len(hits) > 0 {
			return hits
		}
	}

	hits := walkExecutable(ws, domainrelease.DefaultReleaseArtifactsDir)
	if len(hits) > 0 {
		return hits
	}
	// Fallback: cwd, by exact name (bash also accepts the binary name only).
	return walkMatching(ws, ".", func(n string) bool { return n == name })
}

func isGoBinaryName(n, name string) bool { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if name == "" {
		return false
	}

	if n == name || n == name+".exe" {
		return true
	}

	for _, goos := range []string{"linux", "darwin", "windows", "freebsd", "openbsd", "netbsd"} {
		if strings.HasPrefix(n, name+"-"+goos+"-") {
			return true
		}
	}

	return false
}

func findCargoExecutables(ws workspace, name string) []string {
	// Artifact-first cargo lands binaries in dist/<goos>-<goarch>/<name>-<goos>-<goarch>,
	// matching the Go shape. GitHub artifact downloads do not preserve the
	// executable bit, so name-pattern matching wins over a bit-check fallback.
	for _, root := range []string{domainrelease.DefaultReleaseArtifactsDir, distRoot} {
		hits := walkMatching(ws, root, func(n string) bool {
			return isRustBinaryName(n, name)
		})
		if len(hits) > 0 {
			return hits
		}
	}

	if hits := walkExecutable(ws, domainrelease.DefaultReleaseArtifactsDir); len(hits) > 0 {
		return hits
	}
	// Container-first cargo: cargo's own release tree.
	return walkExecutableNoDebugInfo(ws, "target/release")
}

func isRustBinaryName(n, name string) bool { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if name == "" {
		return false
	}

	if n == name || n == name+".exe" {
		return true
	}

	for _, goos := range []string{"linux", "darwin", "windows", "freebsd", "openbsd", "netbsd"} {
		if strings.HasPrefix(n, name+"-"+goos+"-") {
			return true
		}
	}

	return false
}

func findPythonWheels(ws workspace) []string {
	for _, root := range []string{domainrelease.DefaultReleaseArtifactsDir, distRoot} {
		hits := walkMatching(ws, root, func(n string) bool {
			return strings.HasSuffix(n, ".whl") || strings.HasSuffix(n, ".tar.gz")
		})
		if len(hits) > 0 {
			return hits
		}
	}

	return nil
}
