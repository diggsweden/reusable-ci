// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

const (
	reproduciblePOM    = "<project><properties><project.build.outputTimestamp>2026-01-01T00:00:00Z</project.build.outputTimestamp></properties></project>"
	unreproduciblePOM  = "<project><properties><other>x</other></properties></project>"
	reproducibleGradle = "tasks.withType(AbstractArchiveTask).configureEach {\n    preserveFileTimestamps = false\n    reproducibleFileOrder = true\n}\n"
	halfGradle         = "tasks.withType(AbstractArchiveTask).configureEach {\n    preserveFileTimestamps = false\n}\n"
)

// TestJVMReproducibility_EveryArtifactOfEveryKindIsChecked puts a failing
// Maven, Gradle and Android artifact after passing ones of the same kind and
// before further artifacts, and a Maven and a Gradle artifact in one directory. Every directory is reported
// once per build system, Maven directories in plan order and then Gradle and
// Android directories in plan order, and the run fails.
func TestJVMReproducibility_EveryArtifactOfEveryKindIsChecked(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.WriteFile("maven/ok/pom.xml", []byte(reproduciblePOM))
	fsys.WriteFile("maven/bad/pom.xml", []byte(unreproduciblePOM))
	fsys.WriteFile("gradle/ok/build.gradle", []byte(reproducibleGradle))
	fsys.WriteFile("gradle/bad/build.gradle.kts", []byte(halfGradle))
	fsys.WriteFile("android/ok/build.gradle", []byte(reproducibleGradle))
	fsys.WriteFile("android/bad/build.gradle", []byte("plugins { id 'com.android.application' }\n"))
	fsys.WriteFile("shared/pom.xml", []byte(reproduciblePOM))
	fsys.WriteFile("shared/build.gradle", []byte(reproducibleGradle))
	fsys.Chdir()

	plan := validationPlanJSON(t, validationConfigPlan(t,
		config.Artifact{Name: "gradle-ok", ProjectType: projecttype.Gradle, WorkingDirectory: "gradle/ok"},
		config.Artifact{Name: "maven-ok", ProjectType: projecttype.Maven, WorkingDirectory: "maven/ok"},
		config.Artifact{Name: "android-ok", ProjectType: projecttype.GradleAndroid, WorkingDirectory: "android/ok"},
		config.Artifact{Name: "gradle-bad", ProjectType: projecttype.Gradle, WorkingDirectory: "gradle/bad"},
		config.Artifact{Name: "maven-bad", ProjectType: projecttype.Maven, WorkingDirectory: "maven/bad"},
		config.Artifact{Name: "shared-maven", ProjectType: projecttype.Maven, WorkingDirectory: "shared"},
		config.Artifact{Name: "shared-gradle", ProjectType: projecttype.Gradle, WorkingDirectory: "shared"},
		config.Artifact{Name: "android-bad", ProjectType: projecttype.GradleAndroid, WorkingDirectory: "android/bad"},
	))

	_, annotations, err := runJVMRepro(t, plan)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation\n%s", err, annotations)
	}

	want := []string{
		"::notice::Maven reproducibility configured in maven/ok (outputTimestamp=2026-01-01T00:00:00Z)",
		"::error::<project.build.outputTimestamp> property is not set in maven/bad",
		"::notice::Maven reproducibility configured in shared (outputTimestamp=2026-01-01T00:00:00Z)",
		"::notice::Gradle reproducibility configured in gradle/ok",
		"::notice::Gradle reproducibility configured in android/ok",
		"::error::Gradle reproducibility not configured in gradle/bad",
		"::notice::Gradle reproducibility configured in shared",
		"::error::Gradle reproducibility not configured in android/bad",
	}

	lines := strings.Split(strings.TrimSpace(annotations), "\n")
	if len(lines) != len(want) {
		t.Fatalf("annotations:\n%s\nwant %d lines", annotations, len(want))
	}

	for i := range want {
		if !strings.HasPrefix(lines[i], want[i]) {
			t.Errorf("annotation %d = %q, want prefix %q", i, lines[i], want[i])
		}
	}

	for _, reason := range []string{"(reproducibleFileOrder=true not found)", "(neither preserveFileTimestamps=false nor reproducibleFileOrder=true found)"} {
		if !strings.Contains(annotations, reason) {
			t.Errorf("missing reason %q:\n%s", reason, annotations)
		}
	}
}

// TestJVMReproducibility_LaterDirectoryIsRefusedBeforeAnyReport names a
// directory that does not exist after passing Maven and Gradle artifacts. The
// run is refused as invalid configuration with nothing reported; it used to
// report the Maven directories first.
func TestJVMReproducibility_LaterDirectoryIsRefusedBeforeAnyReport(t *testing.T) {
	for _, kind := range []projecttype.Type{projecttype.Maven, projecttype.Gradle, projecttype.GradleAndroid} {
		t.Run(string(kind), func(t *testing.T) {
			fsys := testfs.NewReal(t)
			fsys.WriteFile("maven/pom.xml", []byte(reproduciblePOM))
			fsys.WriteFile("gradle/build.gradle", []byte(reproducibleGradle))
			fsys.Chdir()

			plan := validationPlanJSON(t, validationConfigPlan(t,
				config.Artifact{Name: "maven", ProjectType: projecttype.Maven, WorkingDirectory: "maven"},
				config.Artifact{Name: "gradle", ProjectType: projecttype.Gradle, WorkingDirectory: "gradle"},
				config.Artifact{Name: "later", ProjectType: kind, WorkingDirectory: "gone"},
			))

			out, annotations, err := runJVMRepro(t, plan)
			if !errors.Is(err, errs.ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}

			if out != "" || annotations != "" {
				t.Errorf("a refused plan was reported:\nout:\n%s\nannotations:\n%s", out, annotations)
			}
		})
	}
}
