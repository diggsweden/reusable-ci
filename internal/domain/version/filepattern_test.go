// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/version"
)

func TestFilePattern_KnownTypes(t *testing.T) {
	t.Parallel()

	cases := map[projecttype.Type]string{
		projecttype.Maven:         "CHANGELOG.md :(glob)**/pom.xml",
		projecttype.NPM:           "CHANGELOG.md package.json package-lock.json",
		projecttype.Gradle:        "CHANGELOG.md gradle.properties build.gradle.kts settings.gradle.kts build.gradle settings.gradle",
		projecttype.GradleAndroid: "CHANGELOG.md gradle.properties build.gradle.kts settings.gradle.kts build.gradle settings.gradle",
		projecttype.XcodeIOS:      "CHANGELOG.md versions.xcconfig :(glob)**/*.xcconfig",
		projecttype.Python:        "CHANGELOG.md pyproject.toml",
		projecttype.Go:            "CHANGELOG.md", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		projecttype.Cargo:         "CHANGELOG.md Cargo.toml Cargo.lock",
		projecttype.Meta:          "CHANGELOG.md",
	}
	for typ, want := range cases {
		got := version.FilePattern(typ)
		if got != want {
			t.Errorf("FilePattern(%q) = %q, want %q", typ, got, want)
		}
	}
}

func TestFilePattern_UnknownDefaultsToChangelog(t *testing.T) {
	t.Parallel()

	for _, typ := range []projecttype.Type{"", "rust", projecttype.Unknown, "foo", projecttype.Meta} {
		got := version.FilePattern(typ)
		if got != "CHANGELOG.md" {
			t.Errorf("FilePattern(%q) = %q, want only CHANGELOG.md", typ, got)
		}
	}
}

func TestFilePattern_AlwaysContainsChangelog(t *testing.T) {
	t.Parallel()

	for _, typ := range []projecttype.Type{projecttype.Maven, projecttype.NPM, projecttype.Gradle, projecttype.Go, projecttype.Cargo, ""} {
		if !strings.Contains(version.FilePattern(typ), "CHANGELOG.md") {
			t.Errorf("FilePattern(%q) lacks CHANGELOG.md", typ)
		}
	}
}
