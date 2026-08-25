// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"fmt"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// AndroidArtifactNamesInput drives ResolveAndroidArtifactNames. Mirrors
//
// When Override is non-empty, the override-mode names are emitted (the
// canonical artifact name from artifacts.yml drives them). Otherwise
// the date-stamped composed names are emitted from the remaining
// fields.
type AndroidArtifactNamesInput struct {
	IncludeDate bool
	Prefix      string
	RepoName    string
	Flavor      string
	Override    string

	// Today is interpolated as YYYY-MM-DD into the date stamp. Tests
	// pass a fixed time; production passes time.Now().
	Today time.Time
}

// AndroidArtifactNames is the four output names the workflow consumes.
type AndroidArtifactNames struct {
	DebugName   string
	ReleaseName string
	AABName     string
	SBOMName    string
	// AARName is the library counterpart of AABName: the name of the
	// module's primary published artifact. It deliberately shares
	// AABName's *unsuffixed* form under an override, because the
	// orchestrator forwards artifacts.yml `name:` as the override and
	// downstream steps resolve the primary artifact by that exact
	// string (PlannedArtifact.BuildArtifactName). Using ReleaseName
	// here would upload the AAR as "<name>-release" and the download
	// would miss it.
	AARName string
}

// ResolveAndroidArtifactNames computes every artifact name. It is
// pure: same input → same output, no I/O.
func ResolveAndroidArtifactNames(in AndroidArtifactNamesInput) (AndroidArtifactNames, error) {
	if in.RepoName == "" && in.Override == "" {
		return AndroidArtifactNames{}, fmt.Errorf("repo-name is required (or set --override): %w", errs.ErrUsage)
	}

	if in.Override != "" {
		return AndroidArtifactNames{
			DebugName:   in.Override + "-debug",
			ReleaseName: in.Override + "-release",
			AABName:     in.Override,
			SBOMName:    in.Override + "-sbom",
			AARName:     in.Override,
		}, nil
	}

	dateStamp := ""
	if in.IncludeDate {
		dateStamp = in.Today.UTC().Format("2006-01-02") + " - "
	}

	prefix := ""
	if in.Prefix != "" {
		prefix = in.Prefix + " - "
	}

	flavorSuffix := ""
	if in.Flavor != "" {
		flavorSuffix = " - " + in.Flavor
	}

	base := dateStamp + prefix + in.RepoName + flavorSuffix

	return AndroidArtifactNames{
		DebugName:   base + " - APK debug",
		ReleaseName: base + " - APK release",
		AABName:     base + " - AAB release",
		SBOMName:    base + " - build SBOM",
		AARName:     base + " - AAR release",
	}, nil
}

// ResolveAndroidBuildTasksInput drives ResolveAndroidBuildTasks.
type ResolveAndroidBuildTasksInput struct {
	Flavor      string
	BuildTypes  string // "debug", "release", or "debug,release" — substring-matched
	IncludeAAB  bool
	BuildModule string // empty → "app"
	// Library selects AAR-producing library mode. An Android library is
	// `project-type: gradle-android` + `build-type: library`; it has no
	// Play listing, so the AAB and debug-APK derivation below do not
	// apply to it.
	Library bool
}

// ResolveAndroidBuildTasks computes the gradle task list. Pure mirror.
func ResolveAndroidBuildTasks(in ResolveAndroidBuildTasksInput) string {
	module := in.BuildModule
	if module == "" {
		module = "app"
	}

	flavorCap := capitalizeFirst(in.Flavor)

	// Library mode is deliberately release-only and AAB-free. An AAR is
	// what gets published to Maven, and `bundle…` has no meaning for a
	// library — asking for it is a task-not-found failure, not a variant.
	// The flavor is still honoured, because library modules can declare
	// product flavors.
	if in.Library {
		return module + ":assemble" + flavorCap + "Release"
	}

	var parts []string
	if strings.Contains(in.BuildTypes, "debug") {
		parts = append(parts, "assemble"+flavorCap+"Debug")
	}

	if strings.Contains(in.BuildTypes, "release") {
		parts = append(parts, "assemble"+flavorCap+"Release")
	}

	if in.IncludeAAB && strings.Contains(in.BuildTypes, "release") {
		parts = append(parts, module+":bundle"+flavorCap+"Release")
	}

	return strings.Join(parts, " ")
}

// capitalizeFirst returns the input with the first ASCII letter
// upper-cased and the rest lower-cased — same shape as the bash
// awk substr trick.
func capitalizeFirst(s string) string {
	if s == "" {
		return ""
	}

	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

// ParseGradleVersionFromProperties extracts versionName and versionCode
// from a gradle.properties text body. Missing lines yield "unknown",
// matching the bash fallback.
func ParseGradleVersionFromProperties(body string) (string, string) {
	versionName := "unknown" //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	versionCode := "unknown"

	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, "versionName="):
			versionName = strings.TrimPrefix(line, "versionName=")
		case strings.HasPrefix(line, "versionCode="):
			versionCode = strings.TrimPrefix(line, "versionCode=")
		}
	}

	return versionName, versionCode
}

// AndroidSummaryInput drives RenderAndroidSummary.
type AndroidSummaryInput struct {
	JavaVersion string
	JDKDist     string
	BuildModule string
	Flavor      string
	BuildTypes  string
	IncludeAAB  bool
	Signing     bool
	SkipTests   bool
	Version     string
	VersionCode string
	DebugName   string
	ReleaseName string
	AABName     string

	// Library switches the summary to the AAR shape; BuildTypes and
	// IncludeAAB describe the application path only. Mirrors the Library
	// flag on ResolveAndroidBuildTasksInput.
	Library bool
	AARName string
}

// RenderAndroidSummary is the pure markdown body of the Android build
// summary written to the job summary.
func RenderAndroidSummary(in AndroidSummaryInput, now time.Time) string {
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "## Android Variants Build Summary 📱\n\n")

	_, _ = fmt.Fprintf(&b, "### Configuration\n")
	_, _ = fmt.Fprintf(&b, "| Setting | Value |\n")
	_, _ = fmt.Fprintf(&b, "|---------|-------|\n")
	_, _ = fmt.Fprintf(&b, "| **Java** | %s (%s) |\n", in.JavaVersion, in.JDKDist)
	_, _ = fmt.Fprintf(&b, "| **Module** | %s |\n", in.BuildModule)

	flavor := in.Flavor
	if flavor == "" {
		flavor = "default"
	}

	_, _ = fmt.Fprintf(&b, "| **Flavor** | %s |\n", flavor)

	// Library mode is release-only and AAB-free by construction, so the
	// incoming BuildTypes / IncludeAAB values describe the application path
	// and would misreport here.
	if in.Library {
		_, _ = fmt.Fprintf(&b, "| **Build Types** | release (library) |\n")
	} else {
		_, _ = fmt.Fprintf(&b, "| **Build Types** | %s |\n", in.BuildTypes)
		_, _ = fmt.Fprintf(&b, "| **Include AAB** | %s |\n", checkmark(in.IncludeAAB))
	}

	// A library is never keystore-signed — its Maven Central signature is
	// the GPG release key, applied in publish-gradle.yml. That rule lives
	// here rather than in the caller's YAML, alongside every other
	// library/application presentation difference in this function.
	_, _ = fmt.Fprintf(&b, "| **Signing** | %s |\n", boolStatus(in.Signing && !in.Library))

	if in.SkipTests {
		_, _ = fmt.Fprintf(&b, "| **Tests** | ⊘ Skipped |\n")
	} else {
		_, _ = fmt.Fprintf(&b, "| **Tests** | ✓ Executed |\n")
	}

	if in.Version != "" && in.Version != "unknown" {
		_, _ = fmt.Fprintf(&b, "| **Version** | %s (%s) |\n", in.Version, in.VersionCode)
	}

	_, _ = fmt.Fprintf(&b, "\n### Artifacts Generated\n")
	_, _ = b.WriteString(renderAndroidArtifactLines(in))

	_, _ = fmt.Fprintf(&b, "\n*Build completed at %s*\n", now.UTC().Format("2006-01-02 15:04:05 UTC"))

	return b.String()
}

// renderAndroidArtifactLines lists what the build actually produced. A
// library produces exactly one artifact — the AAR — and none of the
// APK/AAB variants; an application's variants come from BuildTypes and
// IncludeAAB, which are the same gates build-gradle-android.yml applies
// to its upload steps.
func renderAndroidArtifactLines(in AndroidSummaryInput) string {
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	if in.Library {
		_, _ = fmt.Fprintf(&b, "✓ Release AAR: `%s`\n", in.AARName)

		return b.String()
	}

	if strings.Contains(in.BuildTypes, "debug") {
		_, _ = fmt.Fprintf(&b, "✓ Debug APK: `%s`\n", in.DebugName)
	}

	if strings.Contains(in.BuildTypes, "release") {
		_, _ = fmt.Fprintf(&b, "✓ Release APK: `%s`\n", in.ReleaseName)
	}

	if in.IncludeAAB && strings.Contains(in.BuildTypes, "release") {
		_, _ = fmt.Fprintf(&b, "✓ Release AAB: `%s`\n", in.AABName)
	}

	return b.String()
}

func checkmark(b bool) string {
	if b {
		return "✓"
	}

	return "✗"
}

func boolStatus(b bool) string {
	if b {
		return "✓ Enabled"
	}

	return "⊘ Disabled"
}
