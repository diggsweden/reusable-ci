// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package doctor

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/publish"
)

// Gradle publishing has a handful of build-script preconditions that
// otherwise fail deep inside Gradle — or, worse, late at Sonatype — with
// messages that don't point at the cause. A missing `withSourcesJar()`
// surfaces as a Central validation rejection minutes after the publish
// "succeeded"; a repository block named something other than the derived
// task expects surfaces as "Task 'publishAllPublicationsToMavenCentral
// Repository' not found in root project".
//
// These checks read the build script as text. That is deliberate: it is
// offline, fast, and testable, where invoking Gradle to introspect the
// model would be none of those. The cost is that an unusual but valid
// build script can produce a false warning — which is why every check
// here is SeverityWarn, never SeverityFail.

//nolint:gochecknoglobals // immutable compiled patterns.
var (
	// Deliberately not anchored to line start: `plugins { id("maven-publish") }`
	// on one line is as common as the multi-line block form.
	reMavenPublishPlugin = regexp.MustCompile(`(id\s*\(?\s*["']maven-publish["']|apply\s+plugin:\s*["']maven-publish["']|` + "`" + `maven-publish` + "`" + `)`)
	// Two spellings beyond the maven-publish set above, because "signing"
	// — unlike "maven-publish" — is a legal bare Kotlin identifier:
	//
	//   plugins { signing }   the accessor form, needing no backticks
	//   signing { … }         the configuration block, which does not
	//                         compile unless the plugin is applied
	//
	// Missing them warned "does not apply the signing plugin" at a real
	// Android library that applies it correctly.
	reSigningPlugin = regexp.MustCompile(`(?m)(id\s*\(?\s*["']signing["']|apply\s+plugin:\s*["']signing["']|` + "`" + `signing` + "`" + `|^\s*signing\s*$|signing\s*\{)`)
	reSourcesJar    = regexp.MustCompile(`withSourcesJar\s*\(`)
	reJavadocJar    = regexp.MustCompile(`withJavadocJar\s*\(`)
	// Applied only to the `repositories { … }` regions of the script (see
	// repositoriesRegions) — on the whole body it also matches the `name`
	// of a pom, a publication or a project, any of which could spell
	// "MavenCentral" and turn the check into a false OK.
	reRepositoryName = regexp.MustCompile(`name\s*=?\s*["']([A-Za-z0-9_-]+)["']`)

	// Anchors repositoriesRegions on real Gradle syntax — the keyword as a
	// whole word, then its opening brace. Matching the next "{" anywhere
	// ahead instead would let a mention in a comment swallow an unrelated
	// block.
	reRepositoriesBlock = regexp.MustCompile(`(^|[^\w.])repositories\s*\{`)

	// The vanniktech plugin supplies maven-publish, signing, and the
	// sources/javadoc jars on the adopter's behalf, so its presence
	// satisfies most of the checks below.
	reVanniktechPlugin = regexp.MustCompile(`com\.vanniktech\.maven\.publish`)
)

// gradlePublishArtifact pairs an artifact with the build script text
// found for it.
type gradlePublishArtifact struct {
	artifact config.Artifact
	path     string // repo-relative, comma-joined when several were read
	body     string // every script's text, concatenated
}

// checkGradlePublishing returns the gradle-publishing checks for every
// artifact that publishes through the Gradle toolchain. It returns nil
// when no artifact does, so the checks stay silent for every existing
// adopter.
func checkGradlePublishing(root string, artifacts []config.Artifact) []Check {
	var checks []Check

	for _, a := range artifacts {
		targets := gradlePublishTargets(a)
		if len(targets) == 0 {
			continue
		}

		dirs := gradleScriptDirs(a)

		found, ok := readGradleBuildScript(root, dirs, a)
		if !ok {
			checks = append(checks, Check{
				Name:        gradleCheckName(a, "build script found"),
				Severity:    SeverityWarn,
				Message:     fmt.Sprintf("no build.gradle or build.gradle.kts under %s", quoteAll(dirs)),
				Remediation: "set working-directory to the module that applies maven-publish",
			})

			continue
		}

		checks = append(checks, gradleScriptChecks(found, targets)...)
	}

	return checks
}

// gradlePublishTargets maps an artifact's publish-to list onto the
// Gradle-toolchain targets, or nil when it does not use that path.
func gradlePublishTargets(a config.Artifact) []publish.GradleTarget {
	if !config.IsGradleToolchain(a.ProjectType) {
		return nil
	}

	var targets []publish.GradleTarget

	for _, t := range a.PublishTo {
		if !config.SupportedPublishTarget(a, t) {
			continue
		}

		// GradleTarget shares its string values with config.PublishTarget,
		// so this both converts and filters: a non-Gradle destination
		// (google-play, npmjs) simply does not parse.
		target, err := publish.ParseGradleTarget(string(t))
		if err != nil {
			continue
		}

		targets = append(targets, target)
	}

	return targets
}

// readGradleBuildScript collects every build script that can carry the
// artifact's publishing config, preferring the Kotlin DSL within a
// directory. The bodies are concatenated, so a check is satisfied when
// *any* of them satisfies it.
//
// Both the module and the working directory are read because either can
// hold the config: an Android library's maven-publish block usually
// belongs to its module (the shipped examples/android-library sets
// working-directory: "." with build-module: "lib"), while a root script
// can just as well configure it for every subproject through
// `subprojects { }` or a convention plugin. Reading only one of the two
// warns on every check for a correctly configured project of the other
// shape.
func readGradleBuildScript(root string, dirs []string, a config.Artifact) (gradlePublishArtifact, bool) {
	var (
		paths  []string
		bodies []string
	)

	for _, dir := range dirs {
		for _, name := range build.GradleBuildScripts {
			rel := filepath.Join(dir, name)

			body, err := os.ReadFile(filepath.Join(root, rel)) //nolint:gosec // path built from the adopter's own config under the repo root.
			if err != nil {
				continue
			}

			paths = append(paths, rel)
			bodies = append(bodies, string(body))

			break // one script per directory, Kotlin DSL first.
		}
	}

	if len(paths) == 0 {
		return gradlePublishArtifact{}, false
	}

	return gradlePublishArtifact{
		artifact: a,
		path:     strings.Join(paths, ", "),
		body:     strings.Join(bodies, "\n"),
	}, true
}

// gradleScriptDirs returns the directories to search, most specific first.
// Only gradle-android carries a build module; everything else resolves to
// the working directory alone.
func gradleScriptDirs(artifact config.Artifact) []string {
	dir := cmp.Or(strings.TrimSpace(artifact.WorkingDirectory), ".")

	if artifact.GradleAndroid == nil {
		return []string{dir}
	}

	module := strings.TrimSpace(artifact.GradleAndroid.BuildModule)
	if module == "" {
		return []string{dir}
	}

	return []string{filepath.Join(dir, module), dir}
}

// gradleScriptChecks runs the text-level assertions over one build script.
func gradleScriptChecks(found gradlePublishArtifact, targets []publish.GradleTarget) []Check {
	var (
		checks     []Check
		vanniktech = reVanniktechPlugin.MatchString(found.body)
	)

	checks = append(checks, gradlePluginCheck(found, vanniktech))

	if slices.Contains(targets, publish.GradleTargetMavenCentral) {
		checks = append(checks,
			gradleSigningCheck(found, vanniktech),
			gradleJarsCheck(found, vanniktech),
		)
	}

	// The repository-name convention only binds when the derived task is
	// in play; an explicit publish-tasks override replaces it entirely.
	if found.artifact.PublishTasks() == "" {
		checks = append(checks, gradleRepositoryCheck(found, targets, vanniktech))
	}

	return checks
}

// scriptCheck is the shape every text-level assertion in this file
// shares: satisfied → SeverityOK naming the script that satisfied it,
// otherwise SeverityWarn with a specific message and remediation.
func scriptCheck(found gradlePublishArtifact, suffix string, satisfied bool, okMessage, message, remediation string) Check {
	name := gradleCheckName(found.artifact, suffix)
	if satisfied {
		return Check{Name: name, Severity: SeverityOK, Message: cmp.Or(okMessage, found.path)}
	}

	return Check{Name: name, Severity: SeverityWarn, Message: message, Remediation: remediation}
}

func gradlePluginCheck(found gradlePublishArtifact, vanniktech bool) Check {
	return scriptCheck(found, "maven-publish applied",
		vanniktech || reMavenPublishPlugin.MatchString(found.body), "",
		fmt.Sprintf("%s does not apply the maven-publish plugin", found.path),
		`add "id(\"maven-publish\")" to the plugins block, or apply com.vanniktech.maven.publish`)
}

func gradleSigningCheck(found gradlePublishArtifact, vanniktech bool) Check {
	return scriptCheck(found, "signing configured",
		vanniktech || reSigningPlugin.MatchString(found.body), "",
		fmt.Sprintf("%s targets maven-central but does not apply the signing plugin", found.path),
		"add \"id(\\\"signing\\\")\" and useInMemoryPgpKeys(...); "+
			"Central rejects unsigned bundles after the publish appears to succeed")
}

func gradleJarsCheck(found gradlePublishArtifact, vanniktech bool) Check {
	// Scanned once each; the warn path used to re-run both patterns over
	// the whole concatenated body to build the message.
	hasSources := reSourcesJar.MatchString(found.body)
	hasJavadoc := reJavadocJar.MatchString(found.body)

	var missing []string

	if !hasSources {
		missing = append(missing, "withSourcesJar()")
	}

	if !hasJavadoc {
		missing = append(missing, "withJavadocJar()")
	}

	okMessage := ""
	if vanniktech {
		okMessage = "supplied by com.vanniktech.maven.publish"
	}

	return scriptCheck(found, "sources + javadoc jars",
		vanniktech || (hasSources && hasJavadoc), okMessage,
		fmt.Sprintf("%s is missing %s", found.path, strings.Join(missing, " and ")),
		"Maven Central requires sources and javadoc jars; add them to the java { } block")
}

// gradleRepositoryCheck validates the naming convention the derived
// publish tasks depend on: publishAllPublicationsTo<Name>Repository only
// exists when a repository is declared with that exact name.
func gradleRepositoryCheck(found gradlePublishArtifact, targets []publish.GradleTarget, vanniktech bool) Check {
	declared := map[string]bool{}
	for _, m := range reRepositoryName.FindAllStringSubmatch(repositoriesRegions(found.body), -1) {
		declared[m[1]] = true
	}

	var missing []string

	for _, target := range targets {
		want := publish.GradleRepositoryName(target)
		// vanniktech manages the Central repository itself, so its
		// absence from the script is expected there.
		if vanniktech && target == publish.GradleTargetMavenCentral {
			continue
		}

		if want != "" && !declared[want] {
			missing = append(missing, fmt.Sprintf("%q (for %s)", want, target))
		}
	}

	return scriptCheck(found, "publish repository names", len(missing) == 0, "",
		fmt.Sprintf("%s declares no repository named %s", found.path, strings.Join(missing, ", ")),
		"name the repository block to match, or set config.publish-tasks to the task your "+
			"plugin uses (vanniktech: publishToMavenCentral)")
}

func gradleCheckName(a config.Artifact, suffix string) string {
	return fmt.Sprintf("gradle publish [%s]: %s", a.Name, suffix)
}

// quoteAll renders a directory list for a message: `"lib" or "."`.
func quoteAll(dirs []string) string {
	quoted := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		quoted = append(quoted, fmt.Sprintf("%q", dir))
	}

	return strings.Join(quoted, " or ")
}

// repositoriesRegions returns the concatenated bodies of every
// `repositories { … }` block in the script, so the repository-name scan
// cannot be satisfied by a `name` belonging to something else.
//
// The blocks nest (a `maven { … }` inside them), so this brace-matches
// rather than using a regex. When the script declares no repositories
// block at all — a script that configures publishing some other way, or
// one this text-level parse simply does not understand — it returns the
// whole body. That keeps the check's failure mode a false OK rather than
// a false warning, which is the trade every check in this file makes.
func repositoriesRegions(body string) string {
	var regions strings.Builder

	for _, loc := range reRepositoriesBlock.FindAllStringIndex(body, -1) {
		// loc[1] is just past the opening brace the pattern anchored on.
		start := loc[1] - 1

		end, ok := matchBrace(body, start)
		if !ok {
			continue
		}

		regions.WriteString(body[start:end])
		regions.WriteByte('\n')
	}

	if regions.Len() == 0 {
		return body
	}

	return regions.String()
}

// matchBrace returns the index just past the brace closing the one at
// start, or false when the script is unbalanced from there on. Quoting
// and comments are not tracked: a stray brace inside a string would
// mis-scope one region, which costs at most the false OK this whole
// helper narrows.
func matchBrace(body string, start int) (int, bool) {
	depth := 0

	for idx := start; idx < len(body); idx++ {
		switch body[idx] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return idx + 1, true
			}
		}
	}

	return 0, false
}
