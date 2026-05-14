// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

import (
	"fmt"
	"strings"
	"time"
)

// RenderGradleInitScript returns the Kotlin init script body that
// applies the cyclonedx plugin to all projects. Mirrors the heredoc in
// scripts/sbom/generate-gradle-sbom.sh.
//
// version is interpolated into the plugin coordinate; callers pin it
// from CYCLONEDX_GRADLE_VERSION.
func RenderGradleInitScript(version string) string {
	return fmt.Sprintf(`import org.cyclonedx.gradle.CyclonedxPlugin

initscript {
    repositories {
        gradlePluginPortal()
    }
    dependencies {
        classpath("org.cyclonedx.bom:org.cyclonedx.bom.gradle.plugin:%s")
    }
}

allprojects {
    apply<CyclonedxPlugin>()
}
`, version)
}

// GradleSummaryInput drives RenderGradleSummary.
type GradleSummaryInput struct {
	JavaVersion string
	GradleTasks string
	SkipTests   bool
	Version     string // optional; bash omits the line when empty
}

// RenderGradleSummary returns the markdown block written by
// scripts/summary/write-gradle-build-summary.sh, byte-for-byte.
func RenderGradleSummary(in GradleSummaryInput, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Gradle Build Summary 🔨\n")
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "- **Java:** %s\n", in.JavaVersion)
	fmt.Fprintf(&b, "- **Tasks:** %s\n", in.GradleTasks)
	if in.SkipTests {
		fmt.Fprintf(&b, "- **Tests:** ⊘ Skipped\n")
	} else {
		fmt.Fprintf(&b, "- **Tests:** ✓ Executed\n")
	}
	if in.Version != "" {
		fmt.Fprintf(&b, "- **Version:** %s\n", in.Version)
	}
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "*Build completed at %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))
	return b.String()
}
