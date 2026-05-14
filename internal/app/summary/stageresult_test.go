// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary_test

import (
	"context"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakemanifestsink"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
)

// --- BuildStage --------------------------------------------------------

func TestBuildStage_RanWithMultipleResults(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.BuildStageResult(context.Background(), out, mf, appsummary.BuildStageInput{
		MavenResult:    "success",
		NPMResult:      "failure",
		MavenArtifacts: "[{\"name\":\"x\"}]",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Single("stage-ran") != "true" {
		t.Errorf("stage-ran = %q", out.Single("stage-ran"))
	}
	if out.Single("stage-result") != "failure" {
		t.Errorf("stage-result = %q", out.Single("stage-result"))
	}
	body := mf.Body("build")
	for _, want := range []string{
		`"stage":"build"`,
		`"result":"failure"`,
		`"ran":true`,
		`"maven":"success"`,
		`"npm":"failure"`,
		`"gradle":"skipped"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("manifest missing %q in:\n%s", want, body)
		}
	}
}

func TestBuildStage_NotRanIsSkipped(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.BuildStageResult(context.Background(), out, mf, appsummary.BuildStageInput{
		MavenResult: "failure", // ignored — stage didn't run
		// No *_ARTIFACTS set → ran=false.
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Single("stage-ran") != "false" {
		t.Errorf("stage-ran = %q", out.Single("stage-ran"))
	}
	if out.Single("stage-result") != "skipped" {
		t.Errorf("stage-result = %q (failure should not surface when ran=false)", out.Single("stage-result"))
	}
}

func TestBuildStage_ProjectTypeExtra(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.BuildStageResult(context.Background(), out, mf, appsummary.BuildStageInput{
		ProjectType:  "npm",
		NPMArtifacts: "[{}]",
		NPMResult:    "success",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mf.Body("build"), `"project_type":"npm"`) {
		t.Errorf("manifest missing project_type:\n%s", mf.Body("build"))
	}
}

func TestBuildStage_DevStageNameOverride(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.BuildStageResult(context.Background(), out, mf, appsummary.BuildStageInput{
		StageName: "dev-build",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mf.Body("dev-build"), `"stage":"dev-build"`) {
		t.Errorf("stage name not applied: %s", mf.Body("dev-build"))
	}
	if mf.Body("build") != "" {
		t.Error("default 'build' name should not be written when override given")
	}
}

func TestBuildStage_FailurePriority(t *testing.T) {
	t.Parallel()
	// failure > cancelled > success
	cases := []struct {
		maven, npm, gradle, want string
	}{
		{"failure", "cancelled", "success", "failure"},
		{"cancelled", "success", "success", "cancelled"},
		{"success", "success", "success", "success"},
	}
	for _, c := range cases {
		out := fakeoutputsink.New(t)
		mf := fakemanifestsink.New(t)
		if _, err := appsummary.BuildStageResult(context.Background(), out, mf, appsummary.BuildStageInput{
			MavenResult:    c.maven,
			NPMResult:      c.npm,
			GradleResult:   c.gradle,
			MavenArtifacts: "[{}]",
		}); err != nil {
			t.Fatal(err)
		}
		if got := out.Single("stage-result"); got != c.want {
			t.Errorf("aggregate %v → %q, want %q", c, got, c.want)
		}
	}
}

func TestBuildStage_CargoDoesNotAffectBuildStage(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.BuildStageResult(context.Background(), out, mf, appsummary.BuildStageInput{})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Single("stage-ran"); got != "false" {
		t.Errorf("stage-ran = %q", got)
	}
	if strings.Contains(mf.Body("build"), `"cargo"`) {
		t.Errorf("cargo should not appear in build manifest: %s", mf.Body("build"))
	}
}

// --- PublishStage ------------------------------------------------------

func TestPublishStage_Targets(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.PublishStageResult(context.Background(), out, mf, appsummary.PublishStageInput{
		ContainersResult: "success",
		Containers:       "[{}]",
	})
	if err != nil {
		t.Fatal(err)
	}
	body := mf.Body("publish")
	for _, want := range []string{
		`"stage":"publish"`,
		`"containers":"success"`,
		`"cargo":"skipped"`,
		`"githubpackages":"skipped"`,
		`"ran":true`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestPublishStage_CargoTargetAndFailurePriority(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.PublishStageResult(context.Background(), out, mf, appsummary.PublishStageInput{
		GHPackagesResult:    "failure",
		CargoSBOMResult:     "cancelled",
		GHPackagesArtifacts: `[{"name":"app.jar"}]`,
		CargoArtifacts:      `[{"name":"app"}]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Single("stage-result"); got != "failure" {
		t.Errorf("stage-result = %q", got)
	}
	body := mf.Body("publish")
	for _, want := range []string{`"githubpackages":"failure"`, `"cargo":"cancelled"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}

func TestPublishStage_ResultsWithoutArtifactsStaySkipped(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.PublishStageResult(context.Background(), out, mf, appsummary.PublishStageInput{
		GHPackagesResult: "success",
		ContainersResult: "success",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Single("stage-ran"); got != "false" {
		t.Errorf("stage-ran = %q", got)
	}
	if got := out.Single("stage-result"); got != "skipped" {
		t.Errorf("stage-result = %q", got)
	}
	if !strings.Contains(mf.Body("publish"), `"ran":false`) {
		t.Errorf("manifest should show ran=false: %s", mf.Body("publish"))
	}
}

// --- PrepareStage ------------------------------------------------------

func TestPrepareStage_RunsOnlyWhenBumpAndArtifacts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		bump      bool
		artifacts string
		ran       string
		result    string
	}{
		{true, "[{}]", "true", "success"},
		{true, "[]", "false", "skipped"},
		{false, "[{}]", "false", "skipped"},
		{false, "", "false", "skipped"},
	}
	for _, c := range cases {
		out := fakeoutputsink.New(t)
		mf := fakemanifestsink.New(t)
		_, err := appsummary.PrepareStageResult(context.Background(), out, mf, appsummary.PrepareStageInput{
			PrepareReleaseResult: "success",
			ShouldRunVersionBump: c.bump,
			Artifacts:            c.artifacts,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := out.Single("stage-ran"); got != c.ran {
			t.Errorf("bump=%v artifacts=%q → ran=%q, want %q", c.bump, c.artifacts, got, c.ran)
		}
		if got := out.Single("stage-result"); got != c.result {
			t.Errorf("bump=%v artifacts=%q → result=%q, want %q", c.bump, c.artifacts, got, c.result)
		}
	}
}

func TestPrepareStage_VersionBumpTargetAlwaysIncluded(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.PrepareStageResult(context.Background(), out, mf, appsummary.PrepareStageInput{
		PrepareReleaseResult: "success",
		ShouldRunVersionBump: true,
		Artifacts:            "[{}]",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mf.Body("prepare"), `"version-bump":"success"`) {
		t.Errorf("manifest missing version-bump target: %s", mf.Body("prepare"))
	}
}

func TestPrepareStage_UnknownResultNormalizesToSkipped(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.PrepareStageResult(context.Background(), out, mf, appsummary.PrepareStageInput{
		PrepareReleaseResult: "in_progress",
		ShouldRunVersionBump: true,
		Artifacts:            `[{"name":"app.jar"}]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Single("stage-result"); got != "skipped" {
		t.Errorf("stage-result = %q", got)
	}
}

// --- PRQualityStage ----------------------------------------------------

func TestPRQualityStage_DefaultsAndAlwaysRuns(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.PRQualityStageResult(context.Background(), out, mf, appsummary.PRQualityStageInput{})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Single("stage-ran"); got != "true" {
		t.Errorf("stage-ran = %q (pr-quality is always true by design)", got)
	}
	if got := out.Single("stage-result"); got != "success" {
		t.Errorf("stage-result = %q", got)
	}
	body := mf.Body("pr-quality")
	for _, want := range []string{`"dependencyreview":"skipped"`, `"swift":"skipped"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in: %s", want, body)
		}
	}
}

func TestPRQualityStage_DisabledTargetsSkipped(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)
	_, err := appsummary.PRQualityStageResult(context.Background(), out, mf, appsummary.PRQualityStageInput{
		// real result is success, but enabled=false → display skipped
		DependencyReviewResult:  "success",
		DependencyReviewEnabled: "false",
		// real result is failure AND enabled
		SASTOpengrepResult:  "failure",
		SASTOpengrepEnabled: "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	body := mf.Body("pr-quality")
	if !strings.Contains(body, `"dependencyreview":"skipped"`) {
		t.Errorf("disabled target should render skipped: %s", body)
	}
	if !strings.Contains(body, `"sastopengrep":"failure"`) {
		t.Errorf("enabled target should render real result: %s", body)
	}
	// Stage result aggregates raw values so the failure surfaces.
	if got := out.Single("stage-result"); got != "failure" {
		t.Errorf("stage-result = %q (should aggregate raw, not gated)", got)
	}
}
