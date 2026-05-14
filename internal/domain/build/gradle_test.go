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
		"import org.cyclonedx.gradle.CyclonedxPlugin",
		"gradlePluginPortal()",
		`classpath("org.cyclonedx.bom:org.cyclonedx.bom.gradle.plugin:3.2.1")`,
		"allprojects {",
		"apply<CyclonedxPlugin>()",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("init script missing %q\n--- got ---\n%s", want, got)
		}
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
		Version:     "1.2.3",
	}, now)
	if !strings.Contains(got, "- **Version:** 1.2.3\n") {
		t.Errorf("missing version line in:\n%s", got)
	}
	if !strings.Contains(got, "- **Tests:** ⊘ Skipped\n") {
		t.Errorf("missing skipped marker in:\n%s", got)
	}
}
