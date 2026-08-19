// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func newMavenDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <groupId>se.digg.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.2.3</version>
</project>`)

	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), pom, 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	return dir
}

// mavenSteps strips the forwarded CLI-opts prefix from each recorded
// invocation, leaving the step the orchestrator chose. The opts
// themselves are asserted separately, and the exact argv of each step is
// pinned by the MavenLibrary/MavenApplication tests in maven_test.go.
func mavenSteps(t *testing.T, runs [][]string, opts []string) [][]string {
	t.Helper()

	out := make([][]string, 0, len(runs))

	for _, r := range runs {
		if len(r) < len(opts) || !reflect.DeepEqual(r[:len(opts)], opts) {
			t.Errorf("CLI opts not forwarded to mvn call: %v", r)

			continue
		}

		out = append(out, r[len(opts):])
	}

	return out
}

func TestMavenReleaseBuild_RunsStepsInOrder(t *testing.T) {
	t.Parallel()

	opts := []string{"-B", "-ntp"}

	for _, tc := range []struct {
		name      string
		buildType string
		profile   string
		skipTests bool
		sbom      bool
		want      [][]string
	}{
		{
			name:      "app",
			buildType: "app",
			sbom:      true,
			want: [][]string{
				{"install", "-DskipTests"},
				{"clean", "package"},
				{"org.cyclonedx:cyclonedx-maven-plugin:2.9.1:makeAggregateBom"},
			},
		},
		{
			name:      "app skipping tests",
			buildType: "app",
			skipTests: true,
			want: [][]string{
				{"install", "-DskipTests"},
				{"clean", "package", "-DskipTests"},
			},
		},
		{
			// A library compiles, tests, then packages separately, so the
			// sources and javadoc jars are built from a tested tree.
			name:      "lib with a profile",
			buildType: "lib",
			profile:   "central-release",
			want: [][]string{
				{"install", "-DskipTests"},
				{"clean", "compile", "-Pcentral-release"},
				{"test", "-Pcentral-release"},
				{"package", "-DskipTests=false", "-Pcentral-release", "-Dgpg.skip=true"},
			},
		},
		{
			// Skipping tests drops the separate test run, and the package
			// step is told so explicitly rather than left to a default.
			name:      "lib skipping tests",
			buildType: "lib",
			skipTests: true,
			want: [][]string{
				{"install", "-DskipTests"},
				{"clean", "compile"},
				{"package", "-DskipTests=true", "-Dgpg.skip=true"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ops := &fakeMaven{}

			var out bytes.Buffer

			err := appbuild.MavenReleaseBuild(context.Background(), &recordingSummarySink{}, ops, &out, &out, appbuild.MavenReleaseBuildInput{
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newMavenDir(t), SkipTests: tc.skipTests, EnableBuildSBOM: tc.sbom},
				BuildType:           tc.buildType,
				Profile:             tc.profile,
				CLIOpts:             opts,
				SBOMToolVersion:     "2.9.1",
			})
			if err != nil {
				t.Fatal(err)
			}

			// Ordered and complete. The previous assertions searched a
			// joined string for three fragments, so they saw neither order
			// nor the steps they did not name -- and the "SBOM did not run"
			// check passed equally on a run where nothing ran.
			if got := mavenSteps(t, ops.runs, opts); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("mvn steps =\n%v\nwant\n%v", got, tc.want)
			}
		})
	}
}

func TestMavenReleaseBuild_RejectsUnknownBuildType(t *testing.T) {
	t.Parallel()

	ops := &fakeMaven{}
	summary := &recordingSummarySink{}

	err := appbuild.MavenReleaseBuild(context.Background(), summary, ops, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.MavenReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newMavenDir(t)},
		BuildType:           "fatjar",
	})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	// Validated first, so an unknown build type does not half-build.
	if len(ops.runs) != 0 {
		t.Errorf("ran mvn for an unknown build type: %v", ops.runs)
	}

	if summary.buf.Len() != 0 {
		t.Errorf("wrote a summary for a build that never started: %q", summary.buf.String())
	}
}
