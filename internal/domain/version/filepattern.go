// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package version

import "github.com/diggsweden/reusable-ci/internal/domain/projecttype"

// FilePattern returns the git-add pathspec for a project type's
// version-bump commit. The CHANGELOG.md is included for every type;
// the project file(s) follow the toolchain's idiomatic location(s).
//
// Mirrors scripts/config/get-file-pattern.sh.
func FilePattern(pt projecttype.Type) string {
	switch pt {
	case projecttype.Maven:
		return "CHANGELOG.md :(glob)**/pom.xml"
	case projecttype.NPM:
		return "CHANGELOG.md package.json package-lock.json"
	case projecttype.Gradle, projecttype.GradleAndroid:
		return "CHANGELOG.md gradle.properties build.gradle.kts settings.gradle.kts build.gradle settings.gradle"
	case projecttype.XcodeIOS:
		return "CHANGELOG.md versions.xcconfig :(glob)**/*.xcconfig"
	case projecttype.Python:
		return "CHANGELOG.md pyproject.toml"
	case projecttype.Go:
		return "CHANGELOG.md go.mod"
	case projecttype.Cargo:
		return "CHANGELOG.md Cargo.toml Cargo.lock"
	}
	return "CHANGELOG.md"
}
