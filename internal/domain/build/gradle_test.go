// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build_test

import (
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/build"
)

func TestRenderGradleInitScript_EmbedsVersion(t *testing.T) {
	got := build.RenderGradleInitScript("3.2.1")
	for _, want := range []string{
		"gradlePluginPortal()",
		// Real plugin artifact, not the plugin-portal marker. Markers
		// are only resolved by `plugins {}`, not by init-script classpath.
		`classpath("org.cyclonedx:cyclonedx-gradle-plugin:3.2.1")`,
		"allprojects {",
		// Apply by class with the upstream CamelCase casing ("Dx").
		// The class name is stable across v1.x and v2.x.
		"import org.cyclonedx.gradle.CycloneDxPlugin",
		"apply<CycloneDxPlugin>()",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("init script missing %q\n--- got ---\n%s", want, got)
		}
	}
}

func TestRenderGradleInitScript_UsesRealArtifactNotMarker(t *testing.T) {
	got := build.RenderGradleInitScript("3.2.1")
	// The plugin-portal marker coordinate fails in init-script context.
	// Guard so a future "simplification" cannot regress it back.
	if strings.Contains(got, "org.cyclonedx.bom.gradle.plugin") {
		t.Errorf("init script must depend on the real plugin artifact, not the portal marker")
	}
}

func TestRenderGradleSummary_OmitsVersionWhenEmpty(t *testing.T) {
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
