// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
)

func TestRenderGradleInitScript_EmbedsVersion(t *testing.T) {
	t.Parallel()

	// The plugin class was renamed upstream in v3 (CycloneDxPlugin ->
	// CyclonedxPlugin); rendering the wrong one fails Kotlin script
	// compilation and the build SBOM silently never appears. An earlier
	// version of this test asserted the v1/v2 class against version 3.2.1
	// — encoding exactly the bug the blackbox tier later caught.
	cases := []struct {
		version string
		class   string
	}{
		{"1.10.0", "CycloneDxPlugin"},
		{"2.0.0", "CycloneDxPlugin"},
		{"3.2.1", "CyclonedxPlugin"},
	}
	for _, tc := range cases {
		got := build.RenderGradleInitScript(tc.version)
		for _, want := range []string{
			"gradlePluginPortal()",
			// Real plugin artifact, not the plugin-portal marker. Markers
			// are only resolved by `plugins {}`, not by init-script classpath.
			`classpath("org.cyclonedx:cyclonedx-gradle-plugin:` + tc.version + `")`,
			"allprojects {",
			"import org.cyclonedx.gradle." + tc.class,
			"apply<" + tc.class + ">()",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("v%s init script missing %q\n--- got ---\n%s", tc.version, want, got)
			}
		}
	}
}

func TestRenderGradleInitScript_UsesRealArtifactNotMarker(t *testing.T) {
	t.Parallel()

	got := build.RenderGradleInitScript("3.2.1")
	// The plugin-portal marker coordinate fails in init-script context.
	// Guard so a future "simplification" cannot regress it back.
	if strings.Contains(got, "org.cyclonedx.bom.gradle.plugin") {
		t.Errorf("init script must depend on the real plugin artifact, not the portal marker")
	}
}

func TestRenderGradleSummary_OmitsVersionWhenEmpty(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	got := build.RenderGradleSummary(build.GradleSummaryInput{
		JavaVersion: "25",
		GradleTasks: "build",
		SkipTests:   false,
	}, now)
	if strings.Contains(got, "**Version:**") {
		t.Errorf("expected no version line, got:\n%s", got)
	}

	for _, want := range []string{
		"## Gradle Build Summary 🔨",
		"- **Java:** 25",
		"- **Tasks:** build",
		"- **Tests:** ✓ Executed",
		"*Build completed at 2026-05-10 12:00:00 UTC*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderGradleSummary_IncludesVersionWhenSet(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	got := build.RenderGradleSummary(build.GradleSummaryInput{
		JavaVersion: "25",
		GradleTasks: "build :app:bundle",
		SkipTests:   true,
		Version:     "1.2.3", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}, now)
	if !strings.Contains(got, "- **Version:** 1.2.3\n") {
		t.Errorf("missing version line in:\n%s", got)
	}

	if !strings.Contains(got, "- **Tests:** ⊘ Skipped\n") {
		t.Errorf("missing skipped marker in:\n%s", got)
	}
}

// TestRenderGradleInitScript_IsTheWholeScript pins the rendered script exactly,
// because every assertion about it is otherwise a substring check.
//
// Substring checks cannot tell an active statement from an inert one. Prefixing
// the apply line with "// " leaves every Contains assertion above satisfied —
// the tokens are all still present — while the generated script compiles
// cleanly and applies no plugin at all. The build SBOM then never appears, and
// nothing fails: the harvest globs simply find no file, which is also what a
// project with no SBOM looks like.
//
// The script is nine lines and fully determined by one input, so there is
// nothing to approximate. Comparing it whole also pins the two choices the
// doc comment argues for — the real artifact coordinate rather than the plugin
// portal marker, and application by class rather than by id — against a future
// edit that keeps the words and changes the meaning.
func TestRenderGradleInitScript_IsTheWholeScript(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		version, class string
	}{
		{"1.10.0", "CycloneDxPlugin"},
		{"2.0.0", "CycloneDxPlugin"},
		{"3.2.1", "CyclonedxPlugin"},
	} {
		want := "import org.cyclonedx.gradle." + tc.class + `

initscript {
    repositories {
        gradlePluginPortal()
    }
    dependencies {
        classpath("org.cyclonedx:cyclonedx-gradle-plugin:` + tc.version + `")
    }
}

allprojects {
    apply<` + tc.class + `>()
}
`

		if got := build.RenderGradleInitScript(tc.version); got != want {
			t.Errorf("v%s script =\n%s\nwant\n%s", tc.version, got, want)
		}
	}
}

// TestRenderGradleInitScript_AppliesThePluginActively states the property the
// exact comparison above enforces, so a later rewrite of that test cannot drop
// it by accident: the apply call is a live statement, not a comment.
func TestRenderGradleInitScript_AppliesThePluginActively(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"1.10.0", "2.0.0", "3.2.1"} {
		var applied bool

		for _, line := range strings.Split(build.RenderGradleInitScript(version), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "apply<") {
				continue
			}

			applied = true

			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
				t.Errorf("v%s: the apply call is commented out: %q", version, trimmed)
			}
		}

		if !applied {
			t.Errorf("v%s: the script applies no plugin", version)
		}
	}
}
