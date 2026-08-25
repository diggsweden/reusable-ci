// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import "github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"

// FilePattern returns the git-add pathspec for a project type's
// version-bump commit. The CHANGELOG.md is included for every type;
// the project file(s) follow the toolchain's idiomatic location(s).
func FilePattern(pt projecttype.Type) string {
	switch pt {
	case projecttype.Maven:
		return "CHANGELOG.md :(glob)**/pom.xml"
	case projecttype.NPM:
		return "CHANGELOG.md package.json package-lock.json"
	case projecttype.Gradle, projecttype.GradleAndroid:
		// One glob per file family rather than an entry per DSL spelling:
		// build.gradle* covers both Groovy and Kotlin DSL (and any future
		// suffix) in a single pathspec. Which of these exist varies by
		// project — see git.AddPathspecs for why that is safe.
		return "CHANGELOG.md :(glob)gradle.properties :(glob)build.gradle* :(glob)settings.gradle*"
	case projecttype.XcodeIOS:
		// The bare versions.xcconfig literal is redundant: the glob below
		// already matches it at the repo root.
		return "CHANGELOG.md :(glob)**/*.xcconfig"
	case projecttype.Python:
		return "CHANGELOG.md pyproject.toml"
	case projecttype.Go:
		return "CHANGELOG.md"
	case projecttype.Cargo:
		return "CHANGELOG.md Cargo.toml Cargo.lock"
	default:
		// Auto / Meta / Unknown: only the changelog moves on version
		// bump — meta artifacts have no project file of their own.
		return "CHANGELOG.md"
	}
}
