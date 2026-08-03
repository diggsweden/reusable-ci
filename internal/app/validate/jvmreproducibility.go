// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// JVMReproducibilityInput drives `validate jvm-reproducibility`.
type JVMReproducibilityInput struct {
	ConfigPlanJSON string
}

// JVMReproducibility checks every planned Maven/Gradle (JVM + Android)
// artifact for the manifest setting required to make its archive
// byte-identical across rebuilds. Cargo has a symmetric check on
// Cargo.lock; this is the JVM-side counterpart.
//
// A missing setting is a hard error — reproducibility is foundational
// to the deterministic-pipeline contract. The error message includes
// the exact snippet to paste into pom.xml / build.gradle so the fix is
// one copy-paste away. Adopters who can't (or won't) fix the upstream
// build script can drop the JVM artifact from `release-orchestrator`'s
// matrix; there is no per-artifact opt-out for the reproducibility
// invariant itself.
//
// Heuristic detection:
//
//   - Maven: real XML parse of pom.xml. Looks for
//     <project.build.outputTimestamp> in <properties>. A non-empty
//     value is taken as "configured" (we don't validate the literal —
//     it can be a fixed ISO-8601 or a property reference like
//     ${git.commit.author.time}).
//   - Gradle: substring check on build.gradle / build.gradle.kts /
//     settings.gradle. Looks for "preserveFileTimestamps" set to
//     false AND "reproducibleFileOrder" set to true. The Gradle DSL
//     is a script, so a real parse would require a JVM; the
//     substring approach matches every form we've seen in the wild
//     (Groovy + Kotlin DSL, with or without "= ", inside an
//     AbstractArchiveTask block or applied directly).
func JVMReproducibility(_ context.Context, out io.Writer, annot output.Annotator, in JVMReproducibilityInput) error {
	plan, err := parseConfigPlan(in.ConfigPlanJSON)
	if err != nil {
		return err
	}

	maven, gradle, android := jvmArtifactsFromPlan(plan)
	if len(maven)+len(gradle)+len(android) == 0 {
		annot.Noticef("No Maven/Gradle artifacts to check for reproducibility")

		return nil
	}

	seen := map[string]bool{}

	mavenOK, err := runMavenReproChecks(maven, seen, out, annot)
	if err != nil {
		return err
	}

	gradleOK, err := runGradleReproChecks(append(gradle, android...), seen, out, annot)
	if err != nil {
		return err
	}

	if !mavenOK || !gradleOK {
		return fmt.Errorf("jvm reproducibility settings missing: %w", errs.ErrValidation)
	}

	return nil
}

// runMavenReproChecks walks every Maven artifact, deduplicates by
// working directory, and runs checkMavenReproducibility for each.
// Returns (true, nil) when every check passed.
func runMavenReproChecks(artifacts []pipeline.PlannedArtifact, seen map[string]bool, out io.Writer, annot output.Annotator) (bool, error) {
	allOK := true

	for _, artifact := range artifacts {
		dir, dirErr := safeWorkingDir(artifact.WorkingDirectory)
		if dirErr != nil {
			return false, dirErr
		}

		if seen[mavenKey(dir)] {
			continue
		}

		seen[mavenKey(dir)] = true
		if !checkMavenReproducibility(dir, out, annot) {
			allOK = false
		}
	}

	return allOK, nil
}

// runGradleReproChecks mirrors runMavenReproChecks for the combined
// Gradle + Gradle-Android artifact list.
func runGradleReproChecks(artifacts []pipeline.PlannedArtifact, seen map[string]bool, out io.Writer, annot output.Annotator) (bool, error) {
	allOK := true

	for _, artifact := range artifacts {
		dir, dirErr := safeWorkingDir(artifact.WorkingDirectory)
		if dirErr != nil {
			return false, dirErr
		}

		if seen[gradleKey(dir)] {
			continue
		}

		seen[gradleKey(dir)] = true
		if !checkGradleReproducibility(dir, out, annot) {
			allOK = false
		}
	}

	return allOK, nil
}

// parseConfigPlan unwraps the static config-plan JSON; same shape and
// version-pinning logic as plannedCargoArtifacts but reads the
// top-level config plan rather than the publish-stage plan, because
// Maven/Gradle artifacts live in release-build-stage, not publish.
func parseConfigPlan(value string) (pipeline.ConfigPlan, error) {
	if strings.TrimSpace(value) == "" {
		return pipeline.ConfigPlan{}, fmt.Errorf("config-plan-json is required: %w", errs.ErrUsage)
	}

	var plan pipeline.ConfigPlan
	if err := json.Unmarshal([]byte(value), &plan); err != nil {
		return pipeline.ConfigPlan{}, fmt.Errorf("parse config-plan-json: %w: %w", err, errs.ErrInvalidConfig)
	}

	if plan.Version != pipeline.ConfigPlanVersion {
		return pipeline.ConfigPlan{}, fmt.Errorf("config-plan-json has unsupported version %d: %w", plan.Version, errs.ErrInvalidConfig)
	}

	return plan, nil
}

func jvmArtifactsFromPlan(plan pipeline.ConfigPlan) ([]pipeline.PlannedArtifact, []pipeline.PlannedArtifact, []pipeline.PlannedArtifact) {
	var maven, gradle, android []pipeline.PlannedArtifact

	for _, art := range plan.Artifacts.All {
		switch art.ProjectType {
		case projecttype.Maven:
			maven = append(maven, art)
		case projecttype.Gradle:
			gradle = append(gradle, art)
		case projecttype.GradleAndroid:
			android = append(android, art)
		default:
			// NPM / Go / Cargo / XcodeIOS / Python / Meta / Auto /
			// Unknown — not JVM artifacts, skipped.
		}
	}

	return maven, gradle, android
}

// Distinct key prefixes so a single working dir holding both a pom and
// a build.gradle (rare but possible) gets both checks once each.
func mavenKey(dir string) string  { return "mvn:" + dir }
func gradleKey(dir string) string { return "gradle:" + dir }

// mavenPOM is the minimal slice of pom.xml needed for the
// reproducibility check. Anything we don't read goes through
// xml.Decoder unmodified; we never round-trip the document.
type mavenPOM struct {
	XMLName    xml.Name `xml:"project"`
	Properties struct {
		// The XML element name itself contains dots ("project.build.outputTimestamp").
		// encoding/xml can't bind that with a struct tag, so we capture all child
		// elements and look up the field by name below.
		Any []struct {
			XMLName xml.Name
			Value   string `xml:",chardata"`
		} `xml:",any"`
	} `xml:"properties"`
}

// mavenOutputTimestamp returns the (possibly empty) value of the
// <project.build.outputTimestamp> property. The bool reports whether
// the element was present at all — missing element and empty value
// are the two failure modes JVMReproducibility surfaces separately.
func mavenOutputTimestamp(pom mavenPOM) (string, bool) {
	for _, p := range pom.Properties.Any {
		if p.XMLName.Local == "project.build.outputTimestamp" {
			return strings.TrimSpace(p.Value), true
		}
	}

	return "", false
}

// checkMavenReproducibility returns true when the Maven artifact at dir
// satisfies the reproducibility invariant. False = a hard violation
// that JVMReproducibility surfaces as ErrValidation.
func checkMavenReproducibility(dir string, out io.Writer, annot output.Annotator) bool {
	pomPath := filepath.Join(dir, "pom.xml")

	body, err := os.ReadFile(pomPath) //nolint:gosec // path derived from validated config plan; filename hardcoded.
	if err != nil {
		annot.Errorf("pom.xml unreadable in %s: %v", displayDir(dir), err)

		return false
	}

	var pom mavenPOM
	if err := xml.Unmarshal(body, &pom); err != nil { //nolint:musttag // the inner anonymous struct uses `xml:",any"` to capture every <properties> child element; explicit per-element tags wouldn't help since the element name we want (`project.build.outputTimestamp`) contains dots and isn't a valid struct-tag identifier.
		annot.Errorf("pom.xml in %s is malformed: %v", displayDir(dir), err)

		return false
	}

	value, present := mavenOutputTimestamp(pom)

	switch {
	case !present:
		failMavenMissing(dir, "<project.build.outputTimestamp> property is not set", out, annot)

		return false
	case value == "":
		failMavenMissing(dir, "<project.build.outputTimestamp> is set but empty", out, annot)

		return false
	default:
		annot.Noticef("Maven reproducibility configured in %s (outputTimestamp=%s)", displayDir(dir), value)
		_, _ = fmt.Fprintf(out, "%s %s: project.build.outputTimestamp=%s\n", clicolor.Check(out), displayDir(dir), value)

		return true
	}
}

func failMavenMissing(dir, reason string, out io.Writer, annot output.Annotator) {
	annot.Errorf("%s in %s — JAR builds will not be byte-identical across rebuilds", reason, displayDir(dir))

	_, _ = fmt.Fprintf(out, "%s %s: %s\n", clicolor.Cross(out), displayDir(dir), reason)
	_, _ = fmt.Fprintf(out, "  Fix: add to pom.xml <properties>:\n")
	_, _ = fmt.Fprintf(out, "    <project.build.outputTimestamp>2026-01-01T00:00:00Z</project.build.outputTimestamp>\n")
	_, _ = fmt.Fprintf(out, "  Reference: https://maven.apache.org/guides/mini/guide-reproducible-builds.html\n")
}

// gradleBuildScripts is the ordered list of filenames `checkGradle`
// considers a build script. The first existing file wins — Gradle
// itself follows the same precedence (Kotlin DSL preferred over
// Groovy when both exist, but in practice projects pick one).
//
//nolint:gochecknoglobals // ordered enumeration of well-known filenames.
var gradleBuildScripts = []string{
	"build.gradle.kts",
	"build.gradle",
}

// checkGradleReproducibility returns true when the Gradle artifact at
// dir satisfies the reproducibility invariant. False = a hard violation
// that JVMReproducibility surfaces as ErrValidation.
func checkGradleReproducibility(dir string, out io.Writer, annot output.Annotator) bool {
	var (
		body []byte
		path string
		err  error
	)

	for _, name := range gradleBuildScripts {
		candidate := filepath.Join(dir, name)

		body, err = os.ReadFile(candidate) //nolint:gosec // candidate is dir+hardcoded basename; dir already validated.
		if err == nil {
			path = candidate

			break
		}
	}

	if path == "" {
		annot.Errorf("no build.gradle{,.kts} found in %s", displayDir(dir))

		return false
	}

	preserve := gradleHasReproducibilitySetting(body, "preserveFileTimestamps", "false")
	order := gradleHasReproducibilitySetting(body, "reproducibleFileOrder", "true")

	switch {
	case preserve && order:
		annot.Noticef("Gradle reproducibility configured in %s", displayDir(dir))
		_, _ = fmt.Fprintf(out, "%s %s: preserveFileTimestamps=false + reproducibleFileOrder=true\n", clicolor.Check(out), displayDir(dir))

		return true
	case !preserve && !order:
		failGradleMissing(dir, "neither preserveFileTimestamps=false nor reproducibleFileOrder=true found", out, annot)

		return false
	case !preserve:
		failGradleMissing(dir, "preserveFileTimestamps=false not found", out, annot)

		return false
	default:
		failGradleMissing(dir, "reproducibleFileOrder=true not found", out, annot)

		return false
	}
}

// gradleHasReproducibilitySetting reports whether the build script
// assigns key to expectedValue. It tolerates the Groovy and Kotlin
// DSL idioms ("key = value", "key value", "key=value", with or
// without whitespace) and trailing characters within a line.
//
// We don't parse the DSL — that would need a JVM — but the heuristic
// matches every form we've seen in the wild. False positives are
// acceptable (a comment containing the phrase would pass); false
// negatives are not (a real setting in an unusual layout would
// trigger a spurious warning).
func gradleHasReproducibilitySetting(body []byte, key, expectedValue string) bool {
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		idx := strings.Index(trimmed, key)
		if idx == -1 {
			continue
		}

		// Skip mentions where the key is a substring of a longer
		// identifier (e.g. "myPreserveFileTimestamps").
		if idx > 0 {
			prev := trimmed[idx-1]
			if isIdentChar(prev) {
				continue
			}
		}

		rest := strings.TrimSpace(trimmed[idx+len(key):])
		// Strip the "= " / "=" / leading whitespace before the value.
		rest = strings.TrimLeft(rest, "= \t")

		if strings.HasPrefix(rest, expectedValue) {
			// Boundary check on the value too — "trueX" must not pass.
			tail := rest[len(expectedValue):]
			if tail == "" || !isIdentChar(tail[0]) {
				return true
			}
		}
	}

	return false
}

func isIdentChar(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}

func failGradleMissing(dir, reason string, out io.Writer, annot output.Annotator) {
	annot.Errorf("Gradle reproducibility not configured in %s — JAR builds will not be byte-identical across rebuilds (%s)", displayDir(dir), reason)

	_, _ = fmt.Fprintf(out, "%s %s: %s\n", clicolor.Cross(out), displayDir(dir), reason)
	_, _ = fmt.Fprintf(out, "  Fix: add to build.gradle{,.kts}:\n")
	_, _ = fmt.Fprintf(out, "    tasks.withType(AbstractArchiveTask).configureEach {\n")
	_, _ = fmt.Fprintf(out, "        preserveFileTimestamps = false\n")
	_, _ = fmt.Fprintf(out, "        reproducibleFileOrder = true\n")
	_, _ = fmt.Fprintf(out, "    }\n")
	_, _ = fmt.Fprintf(out, "  Reference: https://docs.gradle.org/current/userguide/working_with_files.html#sec:reproducible_archives\n")
}
