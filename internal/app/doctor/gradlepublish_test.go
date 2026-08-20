// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package doctor_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/app/doctor"
)

// gradleChecks runs doctor over a repo and returns only the
// gradle-publishing checks.
func gradleChecks(t *testing.T, artifactsYAML string, files map[string]string) []doctor.Check {
	t.Helper()

	root := writeRepo(t, artifactsYAML, files)

	all, err := doctor.Run(doctor.Input{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	var out []doctor.Check

	for _, c := range all {
		if strings.HasPrefix(c.Name, "gradle publish [") {
			out = append(out, c)
		}
	}

	return out
}

// gradleCheckMatching finds a gradle check by name substring.
// (doctor_test.go's findCheck matches on the exact full name.)
func gradleCheckMatching(t *testing.T, checks []doctor.Check, substr string) doctor.Check {
	t.Helper()

	for _, c := range checks {
		if strings.Contains(c.Name, substr) {
			return c
		}
	}

	t.Fatalf("no check matching %q in %v", substr, names(checks))

	return doctor.Check{}
}

func names(checks []doctor.Check) []string {
	out := make([]string, len(checks))
	for i, c := range checks {
		out[i] = c.Name
	}

	return out
}

const centralArtifacts = `
artifacts:
  - name: jvm-lib
    project-type: gradle
    build-type: library
    publish-to: [maven-central]
`

// androidLibArtifacts mirrors examples/android-library: the artefact's
// working-directory is the repo root, but the publishing config lives in
// the build module. Reading the root script here warned on every check
// for a correctly configured project.
const androidLibArtifacts = `
artifacts:
  - name: android-lib
    project-type: gradle-android
    working-directory: .
    build-type: library
    publish-to: [maven-central]
    config:
      build-module: lib
`

// centralReadyScript satisfies every Central-facing check, so a case that
// exercises one dimension (script discovery, repository naming) is not
// also asserting on the plugin/jars checks by accident.
const centralReadyScript = `
plugins { id("maven-publish"); id("signing") }
java { withSourcesJar(); withJavadocJar() }
publishing { repositories { maven { name = "MavenCentral" } } }
`

// The checks must stay completely silent for adopters that don't publish
// through the Gradle toolchain — that is what makes them additive.
func TestGradlePublishChecks_SilentForNonGradleAdopters(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"maven artifact": `
artifacts:
  - name: lib
    project-type: maven
    build-type: library
    publish-to: [maven-central]
`,
		"gradle artifact that does not publish": `
artifacts:
  - name: app
    project-type: gradle
    build-type: application
`,
		"android app publishing to play": `
artifacts:
  - name: app
    project-type: gradle-android
    publish-to: [google-play]
`,
	}
	for name, yml := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := gradleChecks(t, yml, nil); len(got) != 0 {
				t.Errorf("expected no gradle checks, got %v", names(got))
			}
		})
	}
}

func TestGradlePublishChecks_HappyPath(t *testing.T) {
	t.Parallel()

	script := `
plugins {
    id("maven-publish")
    id("signing")
}
java {
    withSourcesJar()
    withJavadocJar()
}
publishing {
    repositories {
        maven {
            name = "MavenCentral"
            url = uri("https://central.sonatype.com/api/v1/publisher")
        }
    }
}
`
	checks := gradleChecks(t, centralArtifacts, map[string]string{"build.gradle.kts": script})
	if len(checks) == 0 {
		t.Fatal("expected gradle checks to run")
	}

	for _, c := range checks {
		if c.Severity != doctor.SeverityOK {
			t.Errorf("check %q = %s (%s), want ok", c.Name, c.Severity, c.Message)
		}
	}
}

func TestGradlePublishChecks_MissingPluginAndJars(t *testing.T) {
	t.Parallel()

	checks := gradleChecks(t, centralArtifacts, map[string]string{
		"build.gradle.kts": "plugins {\n    kotlin(\"jvm\")\n}\n",
	})

	for _, want := range []string{"maven-publish applied", "signing configured", "sources + javadoc jars"} {
		if got := gradleCheckMatching(t, checks, want).Severity; got != doctor.SeverityWarn {
			t.Errorf("check %q = %s, want warn", want, got)
		}
	}

	jars := gradleCheckMatching(t, checks, "sources + javadoc jars")
	for _, want := range []string{"withSourcesJar()", "withJavadocJar()"} {
		if !strings.Contains(jars.Message, want) {
			t.Errorf("message %q should name %q", jars.Message, want)
		}
	}
}

// An explicit publish-tasks override replaces the derived task, so the
// naming convention no longer applies and must not be reported.
func TestGradlePublishChecks_OverrideSuppressesRepositoryCheck(t *testing.T) {
	t.Parallel()

	yml := `
artifacts:
  - name: jvm-lib
    project-type: gradle
    build-type: library
    publish-to: [maven-central]
    config:
      publish-tasks: publishToMavenCentral
`
	script := `
plugins { id("maven-publish"); id("signing") }
java { withSourcesJar(); withJavadocJar() }
publishing { repositories { maven { name = "sonatype" } } }
`

	for _, c := range gradleChecks(t, yml, map[string]string{"build.gradle.kts": script}) {
		if strings.Contains(c.Name, "publish repository names") {
			t.Errorf("repository-name check ran despite a publish-tasks override: %v", c)
		}
	}
}

// vanniktech's plugin supplies maven-publish, signing and the jars, so a
// script applying it must not be warned about any of them.
func TestGradlePublishChecks_VanniktechPluginSatisfiesChecks(t *testing.T) {
	t.Parallel()

	script := `
plugins {
    id("com.vanniktech.maven.publish") version "0.30.0"
}
mavenPublishing { publishToMavenCentral() }
`
	for _, c := range gradleChecks(t, centralArtifacts, map[string]string{"build.gradle.kts": script}) {
		if c.Severity != doctor.SeverityOK {
			t.Errorf("check %q = %s (%s), want ok for a vanniktech project", c.Name, c.Severity, c.Message)
		}
	}
}

// github-packages does not need signing or javadoc jars; only Central does.
func TestGradlePublishChecks_GitHubPackagesSkipsCentralOnlyChecks(t *testing.T) {
	t.Parallel()

	yml := `
artifacts:
  - name: jvm-lib
    project-type: gradle
    build-type: library
    publish-to: [github-packages]
`
	script := `
plugins { id("maven-publish") }
publishing { repositories { maven { name = "GitHubPackages" } } }
`
	checks := gradleChecks(t, yml, map[string]string{"build.gradle.kts": script})
	for _, c := range checks {
		if strings.Contains(c.Name, "signing configured") || strings.Contains(c.Name, "sources + javadoc") {
			t.Errorf("central-only check ran for a github-packages artifact: %q", c.Name)
		}

		if c.Severity != doctor.SeverityOK {
			t.Errorf("check %q = %s (%s), want ok", c.Name, c.Severity, c.Message)
		}
	}
}

// Nothing here may be fatal: these are text-level heuristics over a
// Turing-complete build script, so a false positive must not break a
// release.
func TestGradlePublishChecks_NeverFail(t *testing.T) {
	t.Parallel()

	for _, c := range gradleChecks(t, centralArtifacts, map[string]string{"build.gradle.kts": "// empty\n"}) {
		if c.Severity == doctor.SeverityFail {
			t.Errorf("check %q is fatal; gradle build-script heuristics must warn at most", c.Name)
		}
	}
}

// The repository-name check is what turns the derived-task naming
// convention from a bare gradle "task not found" into a local, actionable
// message. Every row below is one property of the repositoriesRegions
// scan; read together they are that scan's contract.
func TestGradlePublishChecks_RepositoryNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		script string
		want   doctor.Severity
		// wantMessage / wantRemediation are asserted only when non-empty;
		// the phrasing matters on the warn path, where it is the whole
		// point of the check.
		wantMessage     string
		wantRemediation string
	}{
		{
			name: "name that matches no target warns",
			script: `
plugins { id("maven-publish"); id("signing") }
java { withSourcesJar(); withJavadocJar() }
publishing { repositories { maven { name = "sonatype" } } }
`,
			want:            doctor.SeverityWarn,
			wantMessage:     "MavenCentral",
			wantRemediation: "publish-tasks",
		},
		{
			// A `name` outside any repositories block must not satisfy the
			// check — pom { name = ... } is a project title, not a repository.
			name: "pom name is not a repository name",
			script: `
plugins { id("maven-publish"); id("signing") }
java { withSourcesJar(); withJavadocJar() }
publishing {
    publications {
        create<MavenPublication>("lib") {
            pom {
                name = "MavenCentral"
            }
        }
    }
    repositories {
        maven { name = "sonatype" }
    }
}
`,
			want: doctor.SeverityWarn,
		},
		{
			// A repositories block nesting maven { } must still resolve — the
			// scan brace-matches rather than stopping at the first closing brace.
			name: "name inside a nested block resolves",
			script: `
plugins { id("maven-publish"); id("signing") }
java { withSourcesJar(); withJavadocJar() }
publishing {
    repositories {
        maven {
            name = "MavenCentral"
            credentials { username = "x" }
        }
    }
}
`,
			want: doctor.SeverityOK,
		},
		{
			// No repositories block at all falls back to scanning the whole
			// body, so scripts this text-level parse does not understand keep
			// their old (permissive) behaviour rather than starting to warn.
			// Here a convention plugin configures the repository, so the word
			// "repositories" never appears.
			name: "no repositories block falls back to the whole body",
			script: `
plugins { id("maven-publish"); id("signing") }
java { withSourcesJar(); withJavadocJar() }
configureMavenPublishing {
    name = "MavenCentral"
}
`,
			want: doctor.SeverityOK,
		},
		{
			// A bare mention of the keyword must not anchor a region on
			// whatever block happens to open next — scanning forward for the
			// next "{" would swallow the publications block below, pom name and
			// all, and report a false OK even though a real repositories block
			// exists and names something else.
			name: "a bare mention does not swallow a later block",
			script: `
plugins { id("maven-publish"); id("signing") }
java { withSourcesJar(); withJavadocJar() }
// repositories are declared below
publishing {
    publications {
        create<MavenPublication>("lib") { pom { name = "MavenCentral" } }
    }
    repositories {
        maven { name = "sonatype" }
    }
}
`,
			want: doctor.SeverityWarn,
		},
		{
			// Multiple repositories blocks (a dependency one and a publishing
			// one) must all be scanned, not just the first.
			name: "every repositories block is scanned",
			script: `
plugins { id("maven-publish"); id("signing") }
java { withSourcesJar(); withJavadocJar() }
repositories {
    mavenCentral()
}
publishing {
    repositories {
        maven { name = "MavenCentral" }
    }
}
`,
			want: doctor.SeverityOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			checks := gradleChecks(t, centralArtifacts, map[string]string{"build.gradle.kts": tc.script})

			check := gradleCheckMatching(t, checks, "publish repository names")
			if check.Severity != tc.want {
				t.Fatalf("severity = %s (%s), want %s", check.Severity, check.Message, tc.want)
			}

			if tc.wantMessage != "" && !strings.Contains(check.Message, tc.wantMessage) {
				t.Errorf("message %q should name %q", check.Message, tc.wantMessage)
			}

			if tc.wantRemediation != "" && !strings.Contains(check.Remediation, tc.wantRemediation) {
				t.Errorf("remediation %q should mention %q", check.Remediation, tc.wantRemediation)
			}
		})
	}
}

// Which build script the checks read is its own dimension: an Android
// library keeps its publishing config in the build module, a plain JVM
// project in the working directory, and a multi-project build may
// configure every subproject from the root. Reading the wrong one warns
// on every check for a correctly configured project.
func TestGradlePublishChecks_ScriptDiscovery(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		artifacts string
		files     map[string]string
		// wantWarn names the check expected to warn. Empty means every
		// check must be ok — i.e. the right script was found.
		wantWarn string
		// wantPath, when set, must appear in every check's message. It is
		// how a case pins *which* script was read, not merely that one was.
		wantPath string
	}{
		{
			name:      "no script at all warns rather than staying silent",
			artifacts: centralArtifacts,
			files:     nil,
			wantWarn:  "build script found",
		},
		{
			name:      "the build module's script wins over the root decoy",
			artifacts: androidLibArtifacts,
			files: map[string]string{
				"build.gradle.kts":     "plugins { id(\"com.android.library\") apply false }\n",
				"lib/build.gradle.kts": centralReadyScript,
			},
			wantPath: "lib/build.gradle.kts",
		},
		{
			// With no script in the module directory the working directory is
			// still searched, so a single-module project keeps working.
			name:      "falls back to the working directory",
			artifacts: androidLibArtifacts,
			files:     map[string]string{"build.gradle.kts": centralReadyScript},
		},
		{
			// The mirror of the build-module case: the module has its own
			// script, but publishing is configured for every subproject from
			// the root. Reading only the most specific script warns on every
			// check for that equally valid shape, so both are scanned.
			name:      "the root script is read alongside the module's",
			artifacts: androidLibArtifacts,
			files: map[string]string{
				"build.gradle": `
subprojects {
  apply plugin: "maven-publish"
  apply plugin: "signing"
  java { withSourcesJar(); withJavadocJar() }
  publishing { repositories { maven { name = "MavenCentral" } } }
}
`,
				"lib/build.gradle.kts": "plugins { id(\"com.android.library\") }\n",
			},
		},
		{
			// Groovy DSL under a non-root working directory: both the
			// build.gradle filename and the working-directory join have to
			// resolve for this to find anything.
			name: "groovy DSL under a nested working directory",
			artifacts: `
artifacts:
  - name: android-lib
    project-type: gradle-android
    build-type: library
    working-directory: libs/android
    publish-to: [github-packages]
    config:
      build-module: lib
`,
			files: map[string]string{"libs/android/build.gradle": `
apply plugin: 'maven-publish'
publishing { repositories { maven { name = 'GitHubPackages' } } }
`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			checks := gradleChecks(t, tc.artifacts, tc.files)
			if len(checks) == 0 {
				t.Fatal("expected gradle checks to run")
			}

			if tc.wantWarn != "" {
				if got := gradleCheckMatching(t, checks, tc.wantWarn).Severity; got != doctor.SeverityWarn {
					t.Fatalf("check %q = %s, want warn", tc.wantWarn, got)
				}

				return
			}

			for _, c := range checks {
				if c.Severity != doctor.SeverityOK {
					t.Errorf("check %q = %s (%s), want ok", c.Name, c.Severity, c.Message)
				}

				if tc.wantPath != "" && !strings.Contains(c.Message, tc.wantPath) {
					t.Errorf("check %q should report %q, got %q", c.Name, tc.wantPath, c.Message)
				}
			}
		})
	}
}
