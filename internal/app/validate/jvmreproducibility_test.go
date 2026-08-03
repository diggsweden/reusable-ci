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
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
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

	out, errs, err := runJVMRepro(t, configPlanJSON(`"maven":[{"name":"api","project_type":"maven","working_directory":"services/api"}]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "outputTimestamp=2026-01-01T00:00:00Z") {
		t.Errorf("expected pass marker in stdout, got:\n%s", out)
	}

	if strings.Contains(errs, "::warning") {
		t.Errorf("unexpected warning when configured correctly:\n%s", errs)
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

	out, stderr, err := runJVMRepro(t, configPlanJSON(`"maven":[{"name":"a","project_type":"maven","working_directory":"."}]`))
	if err == nil {
		t.Fatal("expected validator to fail on missing <project.build.outputTimestamp>")
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("err = %v, want wrapped ErrValidation", err)
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

	_, stderr, err := runJVMRepro(t, configPlanJSON(`"maven":[{"name":"a","project_type":"maven","working_directory":"."}]`))
	if err == nil {
		t.Fatal("expected validator to fail on empty outputTimestamp")
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("err = %v, want wrapped ErrValidation", err)
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

	out, errs, err := runJVMRepro(t, configPlanJSON(`"gradle":[{"name":"app","project_type":"gradle","working_directory":"."}]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "preserveFileTimestamps=false") {
		t.Errorf("expected pass marker, got:\n%s", out)
	}

	if strings.Contains(errs, "::warning") {
		t.Errorf("unexpected warning, got:\n%s", errs)
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

	out, _, err := runJVMRepro(t, configPlanJSON(`"gradle":[{"name":"app","project_type":"gradle","working_directory":"."}]`))
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

	_, stderr, err := runJVMRepro(t, configPlanJSON(`"gradle":[{"name":"app","project_type":"gradle","working_directory":"."}]`))
	if err == nil {
		t.Fatal("expected validator to fail when reproducibleFileOrder is missing")
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("err = %v, want wrapped ErrValidation", err)
	}

	if !strings.Contains(stderr, "reproducibleFileOrder=true not found") {
		t.Errorf("expected specific 'reproducibleFileOrder missing' error, got:\n%s", stderr)
	}
}

func TestJVMReproducibility_GradleNoSettingsFails(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("build.gradle", []byte(`plugins { id 'java' }`))
	fsys.Chdir()

	_, stderr, err := runJVMRepro(t, configPlanJSON(`"gradle":[{"name":"app","project_type":"gradle","working_directory":"."}]`))
	if err == nil {
		t.Fatal("expected validator to fail when both gradle reproducibility settings are missing")
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("err = %v, want wrapped ErrValidation", err)
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
	fsys.WriteFile("build.gradle", []byte(`def myPreserveFileTimestamps = false
def myReproducibleFileOrder = true
`))
	fsys.Chdir()

	_, stderr, err := runJVMRepro(t, configPlanJSON(`"gradle":[{"name":"x","project_type":"gradle","working_directory":"."}]`))
	if err == nil {
		t.Fatal("expected validator to fail — identifier-prefixed names must not satisfy the check")
	}

	if !strings.Contains(stderr, "neither") {
		t.Errorf("substring should not match identifier-prefixed names; got:\n%s", stderr)
	}
}

func TestJVMReproducibility_NoJVMArtifactsNoop(t *testing.T) {
	_, errs, err := runJVMRepro(t, configPlanJSON(`"cargo":[{"name":"x","project_type":"cargo","working_directory":"."}]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(errs, "No Maven/Gradle artifacts") {
		t.Errorf("expected explicit no-op notice, got:\n%s", errs)
	}
}

func TestJVMReproducibility_EmptyPlanIsUsageError(t *testing.T) {
	_, _, err := runJVMRepro(t, "")
	if err == nil {
		t.Fatalf("expected usage error on empty plan")
	}
}

// configPlanJSON returns a minimal valid config plan with the
// supplied per-type artifact lists slotted in.
func configPlanJSON(inner string) string {
	allLine := ""
	// derive an "all" mirror from inner (the validator reads .Artifacts.All).
	// For test ergonomics each inner snippet is single-typed, so we duplicate
	// it under "all" verbatim.
	if inner != "" {
		// extract just the JSON array from inner "key":[...] — the simplest
		// reliable way for fixtures is to require callers to wrap properly.
		// All test callers pass `"<type>":[…]` so we mirror to all by taking
		// the bracket span.
		left := strings.Index(inner, "[")
		right := strings.LastIndex(inner, "]")

		if left != -1 && right > left {
			allLine = `"all":` + inner[left:right+1] + `,`
		}
	}

	return `{"version":1,"artifacts":{` + allLine + inner + `},"containers":{"all":[],"has_containers":false}}`
}
