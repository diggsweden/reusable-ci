// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
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

// mavenRunJoins returns each recorded mvn invocation joined, for assertions.
func mavenRunJoins(runs [][]string) []string {
	out := make([]string, 0, len(runs))
	for _, r := range runs {
		out = append(out, strings.Join(r, " "))
	}

	return out
}

func TestMavenReleaseBuild_AppSequence(t *testing.T) {
	t.Parallel()

	ops := &fakeMaven{}

	var out bytes.Buffer

	err := appbuild.MavenReleaseBuild(context.Background(), &recordingSummarySink{}, ops, &out, &out, appbuild.MavenReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newMavenDir(t), EnableBuildSBOM: true},
		BuildType:           "app",
		CLIOpts:             []string{"-B", "-ntp"},
		SBOMToolVersion:     "2.9.1",
	})
	if err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(mavenRunJoins(ops.runs), "\n")
	for _, want := range []string{"install -DskipTests", "clean package", "cyclonedx-maven-plugin:2.9.1:makeAggregateBom"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing mvn call %q in:\n%s", want, joined)
		}
	}
	// CLI opts are forwarded to every mvn call.
	for _, r := range ops.runs {
		if len(r) < 2 || r[0] != "-B" || r[1] != "-ntp" {
			t.Errorf("CLI opts not forwarded to mvn call: %v", r)
		}
	}
}

func TestMavenReleaseBuild_LibUsesProfile(t *testing.T) {
	t.Parallel()

	ops := &fakeMaven{}

	err := appbuild.MavenReleaseBuild(context.Background(), &recordingSummarySink{}, ops, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.MavenReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newMavenDir(t), EnableBuildSBOM: false},
		BuildType:           "lib",
		Profile:             "central-release",
	})
	if err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(mavenRunJoins(ops.runs), "\n")
	if !strings.Contains(joined, "-Pcentral-release") {
		t.Errorf("library build should activate the profile:\n%s", joined)
	}

	if strings.Contains(joined, "makeAggregateBom") {
		t.Errorf("SBOM ran despite EnableBuildSBOM=false:\n%s", joined)
	}
}

func TestMavenReleaseBuild_RejectsUnknownBuildType(t *testing.T) {
	t.Parallel()

	err := appbuild.MavenReleaseBuild(context.Background(), &recordingSummarySink{}, &fakeMaven{}, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.MavenReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newMavenDir(t)},
		BuildType:           "fatjar",
	})
	if err == nil || !strings.Contains(err.Error(), "build-type must be") {
		t.Fatalf("err = %v, want build-type validation", err)
	}
}
