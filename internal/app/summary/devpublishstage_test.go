// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary_test

import (
	"context"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakemanifestsink"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
)

func TestDevPublishStage_RanWhenProjectTypeRecognised(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.DevPublishStageResult(context.Background(), out, mf, appsummary.DevPublishStageInput{
		ProjectType:       projecttype.NPM,
		PublishNPM:        true,
		ContainerResult:   "success",
		NPMResult:         "success",
		NPMPackageName:    "@digg/example",
		NPMPackageVersion: "0.1.0-dev-main-abc1234",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Single("stage-ran"); got != "true" {
		t.Errorf("stage-ran = %q", got)
	}
	if got := out.Single("stage-result"); got != "success" {
		t.Errorf("stage-result = %q", got)
	}
	for _, want := range []string{
		`"npm_package_name":"@digg/example"`,
		`"npm_package_version":"0.1.0-dev-main-abc1234"`,
		`"npm_publish_status":"published"`,
	} {
		if !strings.Contains(out.Single("artifacts-json"), want) {
			t.Errorf("artifacts-json missing %q:\n%s", want, out.Single("artifacts-json"))
		}
	}
	body := mf.Body("dev-publish")
	for _, want := range []string{
		`"stage":"dev-publish"`,
		`"npm":"success"`, // PublishNPM=true + project=npm → preserved
		`"project_type":"npm"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("manifest missing %q in:\n%s", want, body)
		}
	}
}

func TestDevPublishStage_NPMTargetSkippedWhenPublishFlagFalse(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.DevPublishStageResult(context.Background(), out, mf, appsummary.DevPublishStageInput{
		ProjectType: projecttype.NPM,
		PublishNPM:  false,
		NPMResult:   "success",
	})
	if err != nil {
		t.Fatal(err)
	}
	body := mf.Body("dev-publish")
	if !strings.Contains(body, `"npm":"skipped"`) {
		t.Errorf("expected npm to be skipped when publish-npm=false:\n%s", body)
	}
}

func TestDevPublishStage_DoesNotRunForUnknownProjectType(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.DevPublishStageResult(context.Background(), out, mf, appsummary.DevPublishStageInput{
		ProjectType:     projecttype.Type("java"), // not in {maven,npm,gradle,cargo}
		ContainerResult: "success",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Single("stage-ran"); got != "false" {
		t.Errorf("stage-ran = %q, want false", got)
	}
	if got := out.Single("stage-result"); got != "skipped" {
		t.Errorf("stage-result = %q, want skipped", got)
	}
}

func TestDevPublishStage_DefaultsPublishStatusToPublished(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.DevPublishStageResult(context.Background(), out, mf, appsummary.DevPublishStageInput{
		ProjectType:    projecttype.Cargo,
		NPMPackageName: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Single("artifacts-json"), `"npm_publish_status":"published"`) {
		t.Errorf("expected default publish status, got: %s", out.Single("artifacts-json"))
	}
}

func TestDevPublishStage_RunsForRecognizedProjectTypes(t *testing.T) {
	t.Parallel()
	for _, pt := range []projecttype.Type{projecttype.Maven, projecttype.NPM, projecttype.Gradle, projecttype.Cargo} {
		out := fakeoutputsink.New(t)
		mf := fakemanifestsink.New(t)
		_, err := appsummary.DevPublishStageResult(context.Background(), out, mf, appsummary.DevPublishStageInput{
			ProjectType:     pt,
			ContainerResult: "success",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := out.Single("stage-ran"); got != "true" {
			t.Errorf("project type %q stage-ran = %q", pt, got)
		}
	}
}

func TestDevPublishStage_UnknownResultNormalizesAndStillSucceeds(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.DevPublishStageResult(context.Background(), out, mf, appsummary.DevPublishStageInput{
		ProjectType:     projecttype.Gradle,
		ContainerResult: "weird_value",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Single("stage-result"); got != "success" {
		t.Errorf("stage-result = %q", got)
	}
	if !strings.Contains(mf.Body("dev-publish"), `"container":"skipped"`) {
		t.Errorf("manifest = %s", mf.Body("dev-publish"))
	}
}

func TestDevPublishStage_SBOMAndCargoTargetsSurfaceAndFailStage(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.DevPublishStageResult(context.Background(), out, mf, appsummary.DevPublishStageInput{
		ProjectType:     projecttype.Cargo,
		ContainerResult: "success",
		CargoSBOMResult: "failure",
		SBOMResult:      "failure",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Single("stage-result"); got != "failure" {
		t.Errorf("stage-result = %q", got)
	}
	body := mf.Body("dev-publish")
	for _, want := range []string{`"cargo-sbom":"failure"`, `"sbom":"failure"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}

func TestDevPublishStage_ArtifactsJSONAlreadyExistsStatus(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.DevPublishStageResult(context.Background(), out, mf, appsummary.DevPublishStageInput{
		ProjectType:        projecttype.NPM,
		ContainerResult:    "success",
		NPMResult:          "success",
		PublishNPM:         true,
		NPMPackageName:     "@org/app",
		NPMPackageVersion:  "1.0.0-dev.1",
		NPMPublishStatus:   "already-exists",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Single("artifacts-json"), `"npm_publish_status":"already-exists"`) {
		t.Errorf("artifacts-json = %s", out.Single("artifacts-json"))
	}
}
