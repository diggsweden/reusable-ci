// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	apppublish "github.com/diggsweden/reusable-ci/v3/internal/app/publish"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// writeJAR creates an empty file at <root>/<rel>. Parents are created.
func writeJAR(t *testing.T, fsys *testfs.Real, rel string) {
	t.Helper()
	fsys.WriteFile(rel, []byte("jar"))
}

func TestMavenValidateArtifacts_HappyPath(t *testing.T) {
	fsys := testfs.NewReal(t)
	root := fsys.Root
	writeJAR(t, fsys, "target/demo-1.0.0.jar")
	writeJAR(t, fsys, "target/demo-1.0.0-sources.jar")
	writeJAR(t, fsys, "target/demo-1.0.0-javadoc.jar")

	var out, stderr bytes.Buffer

	res, err := apppublish.MavenValidateArtifacts(context.Background(), &out, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), apppublish.MavenValidateArtifactsInput{Root: root})
	if err != nil {
		t.Fatalf("MavenValidateArtifacts: %v\nstderr: %s", err, stderr.String())
	}

	if res.SourcesCount != 1 || res.JavadocCount != 1 {
		t.Errorf("counts = %+v", res)
	}

	if !strings.Contains(out.String(), "Sources JARs: 1") {
		t.Errorf("out missing sources count:\n%s", out.String())
	}
}

func TestMavenValidateArtifacts_MultiModuleAggregatesAcrossModules(t *testing.T) {
	fsys := testfs.NewReal(t)
	root := fsys.Root
	writeJAR(t, fsys, "core/target/core-1.0.0.jar")
	writeJAR(t, fsys, "core/target/core-1.0.0-sources.jar")
	writeJAR(t, fsys, "core/target/core-1.0.0-javadoc.jar")
	writeJAR(t, fsys, "api/target/api-1.0.0.jar")
	writeJAR(t, fsys, "api/target/api-1.0.0-sources.jar")
	writeJAR(t, fsys, "api/target/api-1.0.0-javadoc.jar")

	res, err := apppublish.MavenValidateArtifacts(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, apppublish.MavenValidateArtifactsInput{Root: root})
	if err != nil {
		t.Fatalf("MavenValidateArtifacts: %v", err)
	}

	if res.SourcesCount != 2 || res.JavadocCount != 2 {
		t.Errorf("counts = %+v, want 2/2", res)
	}
}

func TestMavenValidateArtifacts_ErrorsWhenSourcesMissing(t *testing.T) {
	fsys := testfs.NewReal(t)
	root := fsys.Root
	writeJAR(t, fsys, "target/demo-1.0.0.jar")
	writeJAR(t, fsys, "target/demo-1.0.0-javadoc.jar")

	var stderr bytes.Buffer

	_, err := apppublish.MavenValidateArtifacts(context.Background(), &bytes.Buffer{}, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), apppublish.MavenValidateArtifactsInput{Root: root})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "sources") {
		t.Errorf("err = %v, want ErrValidation naming the missing sources jar", err)
	}

	if !strings.Contains(stderr.String(), "Maven Central requires sources") {
		t.Errorf("missing error message:\n%s", stderr.String())
	}

	if !strings.Contains(stderr.String(), "POM/profile") {
		t.Errorf("missing project-owned attachment guidance:\n%s", stderr.String())
	}
}

func TestMavenValidateArtifacts_ErrorsWhenJavadocMissing(t *testing.T) {
	fsys := testfs.NewReal(t)
	root := fsys.Root
	writeJAR(t, fsys, "target/demo-1.0.0.jar")
	writeJAR(t, fsys, "target/demo-1.0.0-sources.jar")

	_, err := apppublish.MavenValidateArtifacts(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, apppublish.MavenValidateArtifactsInput{Root: root})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "javadoc") {
		t.Errorf("err = %v, want ErrValidation naming the missing javadoc jar", err)
	}
}

func TestMavenValidateArtifacts_IgnoresOriginalJARs(t *testing.T) {
	fsys := testfs.NewReal(t)
	root := fsys.Root
	writeJAR(t, fsys, "target/demo-1.0.0.jar")
	writeJAR(t, fsys, "target/demo-1.0.0-sources.jar")
	writeJAR(t, fsys, "target/demo-1.0.0-javadoc.jar")
	// Spring-Boot leaves the un-shaded jar named original-*.jar — should
	// be skipped from the listing (matches the bash `! -name "original-*.jar"`).
	writeJAR(t, fsys, "target/original-demo-1.0.0.jar")

	res, err := apppublish.MavenValidateArtifacts(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, apppublish.MavenValidateArtifactsInput{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	for _, j := range res.JARs {
		if strings.Contains(filepath.Base(j), "original-") {
			t.Errorf("did not expect original-*.jar in listing: %s", j)
		}
	}
}

func TestMavenValidateArtifacts_IgnoresJARsOutsideTarget(t *testing.T) {
	fsys := testfs.NewReal(t)
	root := fsys.Root
	writeJAR(t, fsys, "lib/demo-sources.jar") // outside target/
	writeJAR(t, fsys, "target/demo.jar")
	writeJAR(t, fsys, "target/demo-sources.jar")
	writeJAR(t, fsys, "target/demo-javadoc.jar")

	res, err := apppublish.MavenValidateArtifacts(context.Background(), &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, apppublish.MavenValidateArtifactsInput{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	if res.SourcesCount != 1 {
		t.Errorf("sources = %d, want 1 (only counted under target/)", res.SourcesCount)
	}
}

// fakePublishMaven captures invocations.
type fakePublishMaven struct {
	runs   [][]string
	runErr error
}

func (f *fakePublishMaven) RunInherit(_ context.Context, _, _ io.Writer, args ...string) error {
	f.runs = append(f.runs, args)

	return f.runErr
}

func TestMavenCentralDeploy_NoSettings(t *testing.T) {
	ops := &fakePublishMaven{}
	if err := apppublish.MavenCentralDeploy(context.Background(), ops, &bytes.Buffer{}, &bytes.Buffer{}, apppublish.MavenCentralDeployInput{
		CLIOpts: []string{"--batch-mode"},
		Profile: "central-release", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}); err != nil {
		t.Fatal(err)
	}

	want := []string{"--batch-mode", "deploy", "-Pcentral-release", "-DskipTests"}
	if len(ops.runs) != 1 || !slices.Equal(ops.runs[0], want) {
		t.Errorf("args = %v, want %v", ops.runs[0], want)
	}
}

func TestMavenCentralDeploy_WithSettings(t *testing.T) {
	fsys := testfs.NewReal(t)
	settings := fsys.WriteFile("settings.xml", []byte("<settings/>"))

	ops := &fakePublishMaven{}
	if err := apppublish.MavenCentralDeploy(context.Background(), ops, &bytes.Buffer{}, &bytes.Buffer{}, apppublish.MavenCentralDeployInput{
		SettingsPath: settings,
		Profile:      "central-release",
	}); err != nil {
		t.Fatal(err)
	}

	want := []string{"deploy", "--settings", settings, "-Pcentral-release", "-DskipTests"}
	if !slices.Equal(ops.runs[0], want) {
		t.Errorf("args = %v, want %v", ops.runs[0], want)
	}
}

func TestMavenCentralDeploy_MissingSettingsFails(t *testing.T) {
	ops := &fakePublishMaven{}

	err := apppublish.MavenCentralDeploy(context.Background(), ops, &bytes.Buffer{}, &bytes.Buffer{}, apppublish.MavenCentralDeployInput{
		SettingsPath: filepath.Join(t.TempDir(), "missing.xml"),
		Profile:      "central-release",
	})
	if !errors.Is(err, errs.ErrMissingInput) || !strings.Contains(err.Error(), "settings file") {
		t.Fatalf("err = %v, want ErrMissingInput naming the settings file", err)
	}

	if len(ops.runs) != 0 {
		t.Errorf("mvn should not have run on validation failure")
	}
}

func TestMavenCentralDeploy_RequiresProfile(t *testing.T) {
	ops := &fakePublishMaven{}

	err := apppublish.MavenCentralDeploy(context.Background(), ops, &bytes.Buffer{}, &bytes.Buffer{}, apppublish.MavenCentralDeployInput{})
	if !errors.Is(err, errs.ErrUsage) || !strings.Contains(err.Error(), "profile is required") {
		t.Fatalf("err = %v, want ErrUsage naming the missing profile", err)
	}

	if len(ops.runs) != 0 {
		t.Errorf("mvn ran without a profile: %v", ops.runs)
	}
}

// TestMavenCentralDeploy_ReturnsTheMavenFailureUnchanged covers the failed
// deploy, which had no test. The adapter's error must survive, so the CLI
// classifies the exit from what Maven reported, and nothing after the attempt
// may claim the deploy went through.
func TestMavenCentralDeploy_ReturnsTheMavenFailureUnchanged(t *testing.T) {
	t.Parallel()

	errDeployRejected := errors.New("sonatype rejected the staging repository") //nolint:err113 // a unique value to find in the chain.
	ops := &fakePublishMaven{runErr: errDeployRejected}

	var out, stderr bytes.Buffer

	err := apppublish.MavenCentralDeploy(context.Background(), ops, &out, &stderr, apppublish.MavenCentralDeployInput{
		Profile: "central-release",
	})
	if !errors.Is(err, errDeployRejected) {
		t.Errorf("err = %v, want the Maven error", err)
	}

	if len(ops.runs) != 1 {
		t.Errorf("mvn runs = %v, want exactly one deploy attempt", ops.runs)
	}

	if out.String() != "Deploying to Maven Central...\n" || stderr.Len() != 0 {
		t.Errorf("stdout = %q, stderr = %q; want only the announcement before the attempt", out.String(), stderr.String())
	}
}
