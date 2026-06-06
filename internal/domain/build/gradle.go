// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"fmt"
	"strings"
	"time"
)

// RenderGradleInitScript returns the Kotlin init script body that
// applies the cyclonedx plugin to all projects.
//
// version is interpolated into the plugin coordinate; callers pin it
// from CYCLONEDX_GRADLE_VERSION.
//
// Two non-obvious choices in the script body, both required for the
// init-script path to work against v1.x AND v2.x of
// cyclonedx-gradle-plugin (verified with v1.10.0 and v2.0.0):
//
//  1. We pull the REAL plugin artifact
//     (`org.cyclonedx:cyclonedx-gradle-plugin`), not the plugin-portal
//     marker (`org.cyclonedx.bom:org.cyclonedx.bom.gradle.plugin`).
//     Markers only carry metadata for `plugins {}` resolution; the
//     init-script classpath has no plugin-marker resolution step, so
//     the marker-only path fails with "Plugin with id ... not found".
//
//  2. We `apply<CycloneDxPlugin>()` by class (note the upstream
//     CamelCase: capital D in "Dx"). Init-script classpath does NOT
//     auto-register plugin IDs against the project plugin registry,
//     so `apply(plugin = "org.cyclonedx.bom")` fails. The class name
//     is stable across the v1.x → v2.x transition.
func RenderGradleInitScript(version string) string {
	return fmt.Sprintf(`import org.cyclonedx.gradle.CycloneDxPlugin

initscript {
    repositories {
        gradlePluginPortal()
    }
    dependencies {
        classpath("org.cyclonedx:cyclonedx-gradle-plugin:%s")
    }
}

allprojects {
    apply<CycloneDxPlugin>()
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

// RenderGradleSummary returns the markdown block written by.
func RenderGradleSummary(in GradleSummaryInput, now time.Time) string {
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "## Gradle Build Summary 🔨\n")
	_, _ = fmt.Fprintf(&b, "\n")
	_, _ = fmt.Fprintf(&b, "- **Java:** %s\n", in.JavaVersion)
	_, _ = fmt.Fprintf(&b, "- **Tasks:** %s\n", in.GradleTasks)

	if in.SkipTests {
		_, _ = fmt.Fprintf(&b, "- **Tests:** ⊘ Skipped\n")
	} else {
		_, _ = fmt.Fprintf(&b, "- **Tests:** ✓ Executed\n")
	}

	if in.Version != "" {
		_, _ = fmt.Fprintf(&b, "- **Version:** %s\n", in.Version)
	}

	_, _ = fmt.Fprintf(&b, "\n")
	_, _ = fmt.Fprintf(&b, "*Build completed at %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))

	return b.String()
}
