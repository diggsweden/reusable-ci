// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build_test

import (
	"context"
	"testing"

	buildcmd "github.com/diggsweden/reusable-ci/internal/cli/commands/build"
	"github.com/diggsweden/reusable-ci/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

func TestGradleAndroidArtifactNamesCmd_WritesOutputsFromEnv(t *testing.T) {
	env := ghaenv.Setup(t)
	env.Setenv("INCLUDE_DATE_STAMP", "false")
	env.Setenv("ARTIFACT_NAME_PREFIX", "ci")
	env.Setenv("REPOSITORY_NAME", "myapp")
	env.Setenv("PRODUCT_FLAVOR", "prod")

	cmd := buildcmd.New()
	if err := cmd.Run(context.Background(), []string{"build", "gradle-android", "artifact-names"}); err != nil { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Fatal(err)
	}

	if got := env.Output("debug-name"); got != "ci - myapp - prod - APK debug" {
		t.Errorf("debug-name = %q", got)
	}

	if got := env.Output("release-name"); got != "ci - myapp - prod - APK release" {
		t.Errorf("release-name = %q", got)
	}

	if got := env.Output("aab-name"); got != "ci - myapp - prod - AAB release" {
		t.Errorf("aab-name = %q", got)
	}

	if got := env.Output("sbom-name"); got != "ci - myapp - prod - build SBOM" {
		t.Errorf("sbom-name = %q", got)
	}
}

func TestGradleAndroidVersionInfoCmd_WritesOutputs(t *testing.T) {
	env := ghaenv.Setup(t)
	fsys := testfs.NewReal(t)
	fsys.Chdir()
	fsys.WriteFile("gradle.properties", []byte("versionName=2.5.0\nversionCode=42\n"))

	cmd := buildcmd.New()
	if err := cmd.Run(context.Background(), []string{"build", "gradle-android", "version-info"}); err != nil {
		t.Fatal(err)
	}

	if got := env.Output("version"); got != "2.5.0" {
		t.Errorf("version = %q", got)
	}

	if got := env.Output("version-code"); got != "42" {
		t.Errorf("version-code = %q", got)
	}
}

func TestGradleAndroidResolveBuildTasksCmd_WritesTasksOutput(t *testing.T) {
	env := ghaenv.Setup(t)
	env.Setenv("PRODUCT_FLAVOR", "demo")
	env.Setenv("BUILD_TYPES", "release")
	env.Setenv("INCLUDE_AAB", "true")
	env.Setenv("BUILD_MODULE", "mymodule")

	cmd := buildcmd.New()
	if err := cmd.Run(context.Background(), []string{"build", "gradle-android", "resolve-build-tasks"}); err != nil {
		t.Fatal(err)
	}

	if got := env.Output("tasks"); got != "assembleDemoRelease mymodule:bundleDemoRelease" {
		t.Errorf("tasks = %q", got)
	}
}
