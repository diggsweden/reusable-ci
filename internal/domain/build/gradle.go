// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
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
//  2. We apply by class. Init-script classpath does NOT
//     auto-register plugin IDs against the project plugin registry,
//     so `apply(plugin = "org.cyclonedx.bom")` fails. The class name
//     is version-dependent: `CycloneDxPlugin` (capital D) through
//     v1.x/v2.x, renamed `CyclonedxPlugin` in v3 (verified against the
//     3.2.1 jar's plugin descriptor). Rendering the wrong one fails
//     script compilation and silently skips the build SBOM.
//
// v3 also moves the aggregate output from build/reports/bom.json to
// build/reports/cyclonedx/; the harvest globs in app/sbom and
// app/summary already cover both locations.
func RenderGradleInitScript(version string) string {
	class := cyclonedxPluginClass(version)

	return fmt.Sprintf(`import org.cyclonedx.gradle.%[1]s

initscript {
    repositories {
        gradlePluginPortal()
    }
    dependencies {
        classpath("org.cyclonedx:cyclonedx-gradle-plugin:%[2]s")
    }
}

allprojects {
    apply<%[1]s>()
}
`, class, version)
}

// cyclonedxPluginClass returns the plugin class for the pinned version:
// upstream renamed CycloneDxPlugin to CyclonedxPlugin in v3. An
// unparsable version keeps the pre-v3 name, matching prior behaviour.
func cyclonedxPluginClass(version string) string {
	major, _, _ := strings.Cut(version, ".")
	if n, err := strconv.Atoi(major); err == nil && n >= 3 {
		return "CyclonedxPlugin"
	}

	return "CycloneDxPlugin"
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
	_, _ = fmt.Fprintf(&b, "- **Java:** %s\n", summary.LiteralText(in.JavaVersion))
	_, _ = fmt.Fprintf(&b, "- **Tasks:** %s\n", summary.LiteralText(in.GradleTasks))

	if in.SkipTests {
		_, _ = fmt.Fprintf(&b, "- **Tests:** ⊘ Skipped\n")
	} else {
		_, _ = fmt.Fprintf(&b, "- **Tests:** ✓ Executed\n")
	}

	if in.Version != "" {
		_, _ = fmt.Fprintf(&b, "- **Version:** %s\n", summary.LiteralText(in.Version))
	}

	_, _ = fmt.Fprintf(&b, "\n")
	_, _ = fmt.Fprintf(&b, "*Build completed at %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))

	return b.String()
}
