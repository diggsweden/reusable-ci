// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// helper to run the validator with no-op annot routing. Returns
// stdout, stderr, and the validator's error.
func runJVMRepro(t *testing.T, plan string) (string, string, error) {
	t.Helper()

	var (
		outBuf bytes.Buffer
		errBuf bytes.Buffer
	)

	err := appvalidate.JVMReproducibility(context.Background(), &outBuf, output.NewAnnotator(&errBuf, output.FormatGitHub), appvalidate.JVMReproducibilityInput{
		ConfigPlanJSON: plan,
	})

	return outBuf.String(), errBuf.String(), err
}

// TestJVMReproducibility_RejectsUnsafeWorkingDirectory covers the shared
// working-directory guard from this side. Both ecosystem validators walk a
// directory the plan names through safeWorkingDir, but only the Cargo caller
// exercised it -- so a change to the guard could have been caught for one
// ecosystem and not the other.
func TestJVMReproducibility_RejectsUnsafeWorkingDirectory(t *testing.T) {
	for name, testCase := range map[string]struct{ dir, want string }{
		"climbs out of the workspace": {dir: "../outside", want: "escapes the workspace"},
		"is absolute":                 {dir: "/etc", want: "must be relative"},
	} {
		t.Run(name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			fsys.Chdir()

			_, _, err := runJVMRepro(t, configPlanJSON(t, projecttype.Maven, "api", testCase.dir))
			if !errors.Is(err, errs.ErrInvalidConfig) || !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("err = %v, want an invalid-config error mentioning %q", err, testCase.want)
			}
		})
	}
}

func TestJVMReproducibility_MavenWithTimestampPasses(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("services/api/pom.xml", []byte(`<?xml version="1.0"?>
<project>
  <modelVersion>4.0.0</modelVersion>
  <groupId>g</groupId><artifactId>a</artifactId><version>1.0.0</version>
  <properties>
    <project.build.outputTimestamp>2026-01-01T00:00:00Z</project.build.outputTimestamp>
  </properties>
</project>`))
	fsys.Chdir()

	out, stderr, err := runJVMRepro(t, configPlanJSON(t, projecttype.Maven, "api", "services/api"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "outputTimestamp=2026-01-01T00:00:00Z") {
		t.Errorf("expected pass marker in stdout, got:\n%s", out)
	}

	if strings.Contains(stderr, "::warning") {
		t.Errorf("unexpected warning when configured correctly:\n%s", stderr)
	}
}

// TestJVMReproducibility_MavenMissingTimestampFails pins the
// deterministic-pipeline guarantee: a pom.xml without
// <project.build.outputTimestamp> can't produce byte-identical jars,
// so the release pipeline rejects it. Adopters get an actionable fix
// snippet in the step summary.
func TestJVMReproducibility_MavenMissingTimestampFails(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("pom.xml", []byte(`<?xml version="1.0"?>
<project><modelVersion>4.0.0</modelVersion>
  <groupId>g</groupId><artifactId>a</artifactId><version>1.0.0</version>
</project>`))
	fsys.Chdir()

	out, stderr, err := runJVMRepro(t, configPlanJSON(t, projecttype.Maven, "a", "."))
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation for a pom without <project.build.outputTimestamp>", err)
	}

	if !strings.Contains(stderr, "::error") {
		t.Errorf("expected ::error annotation, got stderr:\n%s", stderr)
	}

	if !strings.Contains(out, "<project.build.outputTimestamp>") {
		t.Errorf("expected actionable fix snippet in stdout, got:\n%s", out)
	}
}

func TestJVMReproducibility_MavenEmptyTimestampFails(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("pom.xml", []byte(`<?xml version="1.0"?>
<project><modelVersion>4.0.0</modelVersion>
  <groupId>g</groupId><artifactId>a</artifactId><version>1.0.0</version>
  <properties><project.build.outputTimestamp></project.build.outputTimestamp></properties>
</project>`))
	fsys.Chdir()

	_, stderr, err := runJVMRepro(t, configPlanJSON(t, projecttype.Maven, "a", "."))
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation for an empty outputTimestamp", err)
	}

	if !strings.Contains(stderr, "is set but empty") {
		t.Errorf("expected 'empty value' error, got:\n%s", stderr)
	}
}

func TestJVMReproducibility_GradleGroovyWithSettingsPasses(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("build.gradle", []byte(`plugins { id 'java' }
tasks.withType(AbstractArchiveTask).configureEach {
    preserveFileTimestamps = false
    reproducibleFileOrder = true
}
`))
	fsys.Chdir()

	out, stderr, err := runJVMRepro(t, configPlanJSON(t, projecttype.Gradle, "app", "."))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "preserveFileTimestamps=false") {
		t.Errorf("expected pass marker, got:\n%s", out)
	}

	if strings.Contains(stderr, "::warning") {
		t.Errorf("unexpected warning, got:\n%s", stderr)
	}
}

func TestJVMReproducibility_GradleKotlinDSLPasses(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("build.gradle.kts", []byte(`plugins { java }
tasks.withType<AbstractArchiveTask>().configureEach {
    preserveFileTimestamps = false
    reproducibleFileOrder = true
}
`))
	fsys.Chdir()

	out, _, err := runJVMRepro(t, configPlanJSON(t, projecttype.Gradle, "app", "."))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "preserveFileTimestamps=false") {
		t.Errorf("expected pass marker (Kotlin DSL), got:\n%s", out)
	}
}

func TestJVMReproducibility_GradleMissingOneSettingFails(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("build.gradle", []byte(`tasks.withType(AbstractArchiveTask).configureEach {
    preserveFileTimestamps = false
    // reproducibleFileOrder forgotten
}
`))
	fsys.Chdir()

	_, stderr, err := runJVMRepro(t, configPlanJSON(t, projecttype.Gradle, "app", "."))
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation when reproducibleFileOrder is missing", err)
	}

	if !strings.Contains(stderr, "reproducibleFileOrder=true not found") {
		t.Errorf("expected specific 'reproducibleFileOrder missing' error, got:\n%s", stderr)
	}
}

func TestJVMReproducibility_GradleNoSettingsFails(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("build.gradle", []byte(`plugins { id 'java' }`))
	fsys.Chdir()

	_, stderr, err := runJVMRepro(t, configPlanJSON(t, projecttype.Gradle, "app", "."))
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation when both gradle settings are missing", err)
	}

	if !strings.Contains(stderr, "neither preserveFileTimestamps") {
		t.Errorf("expected 'neither …' error, got:\n%s", stderr)
	}
}

// TestJVMReproducibility_GradleSubstringFalsePositiveGuard verifies the
// heuristic rejects identifier extensions: "myPreserveFileTimestamps"
// must NOT be taken as a setting on its own. The build still fails
// (no real setting is present) — but it fails because of the missing
// settings, not because the heuristic was fooled into thinking they
// were configured.
func TestJVMReproducibility_GradleSubstringFalsePositiveGuard(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("build.gradle", []byte(`def mypreserveFileTimestamps = false
def myreproducibleFileOrder = true
`))
	fsys.Chdir()

	_, stderr, err := runJVMRepro(t, configPlanJSON(t, projecttype.Gradle, "x", "."))
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation — identifier-prefixed names must not satisfy the check", err)
	}

	if !strings.Contains(stderr, "neither") {
		t.Errorf("substring should not match identifier-prefixed names; got:\n%s", stderr)
	}
}

func TestJVMReproducibility_NoJVMArtifactsNoop(t *testing.T) {
	_, stderr, err := runJVMRepro(t, configPlanJSON(t, projecttype.Cargo, "x", "."))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(stderr, "No Maven/Gradle artifacts") {
		t.Errorf("expected explicit no-op notice, got:\n%s", stderr)
	}
}

func TestJVMReproducibility_EmptyPlanIsUsageError(t *testing.T) {
	_, _, err := runJVMRepro(t, "")
	// The name is the claim: an absent --config-plan-json is a broken
	// invocation (exit 2), not a reproducibility verdict (exit 1).
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}

func configPlanJSON(t *testing.T, kind projecttype.Type, name, dir string) string {
	t.Helper()

	return validationPlanJSON(t, validationConfigPlan(t, config.Artifact{Name: name, ProjectType: kind, WorkingDirectory: dir}))
}

// TestJVMReproducibility_GradleKotlinDSLIsPrefixedPropertiesPass covers the
// spelling Gradle's own reproducible-archives guide gives for
// build.gradle.kts: the Kotlin DSL reaches the two booleans through their
// is-getters, so a build that followed the guide to the letter was refused.
func TestJVMReproducibility_GradleKotlinDSLIsPrefixedPropertiesPass(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("build.gradle.kts", []byte(`plugins { java }
tasks.withType<AbstractArchiveTask>().configureEach {
    isPreserveFileTimestamps = false
    isReproducibleFileOrder = true
}
`))
	fsys.Chdir()

	out, stderr, err := runJVMRepro(t, configPlanJSON(t, projecttype.Gradle, "app", "."))
	if err != nil {
		t.Fatalf("err = %v, want the is-prefixed Kotlin spelling accepted\nstdout:\n%s\nstderr:\n%s", err, out, stderr)
	}

	if !strings.Contains(out, "preserveFileTimestamps=false") {
		t.Errorf("expected pass marker (Kotlin DSL is-getters), got:\n%s", out)
	}
}

// The is-prefix must not widen the identifier guard: a variable that merely
// embeds the Kotlin spelling is still not the setting.
func TestJVMReproducibility_GradleKotlinIsPrefixSubstringGuard(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("build.gradle.kts", []byte(`val myisPreserveFileTimestamps = false
val myisReproducibleFileOrder = true
`))
	fsys.Chdir()

	_, stderr, err := runJVMRepro(t, configPlanJSON(t, projecttype.Gradle, "x", "."))
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation — identifier-prefixed Kotlin names must not satisfy the check", err)
	}

	if !strings.Contains(stderr, "neither") {
		t.Errorf("substring should not match identifier-prefixed names; got:\n%s", stderr)
	}
}

// TestJVMReproducibility_GradleCheckReadsTextNotTheEvaluatedBuild pins the
// static contract docs/verification.md states: the Gradle check reads the
// script's lines and never evaluates the build, so the settings written only
// in a block comment, or overridden later in the script, still pass. A
// strengthening that changes either verdict must update that section too.
func TestJVMReproducibility_GradleCheckReadsTextNotTheEvaluatedBuild(t *testing.T) {
	for name, script := range map[string]string{
		"settings only inside a block comment": "/*\npreserveFileTimestamps = false\nreproducibleFileOrder = true\n*/\n",
		"settings overridden later": "tasks.withType(AbstractArchiveTask).configureEach {\n    preserveFileTimestamps = false\n    reproducibleFileOrder = true\n}\n" +
			"tasks.withType(AbstractArchiveTask).configureEach {\n    preserveFileTimestamps = true\n    reproducibleFileOrder = false\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			fsys.WriteFile("build.gradle", []byte(script))
			fsys.Chdir()

			if _, _, err := runJVMRepro(t, configPlanJSON(t, projecttype.Gradle, "app", ".")); err != nil {
				t.Fatalf("static text check refused %s: %v", name, err)
			}
		})
	}
}
