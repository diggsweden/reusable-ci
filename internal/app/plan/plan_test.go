// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package plan_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	appplan "github.com/diggsweden/reusable-ci/internal/app/plan"
	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
)

// fakeSummary records every Append for assertions.
type fakeSummary struct{ buf bytes.Buffer }

func (f *fakeSummary) Append(_ context.Context, s string) error {
	f.buf.WriteString(s)

	return nil
}

//nolint:cyclop // verifies many typed plan outputs on one Release plan.
func TestPlanRelease_EmitsTypedPlanOutputs(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	cfg := &config.Config{
		Artifacts: []config.Artifact{
			{
				Name:                 "lib",
				ProjectType:          projecttype.Maven,
				BuildType:            config.BuildTypeLibrary,
				PublishTo:            []config.PublishTarget{config.PublishMavenCentral},
				RequireAuthorization: true,
			},
			{Name: "go-cli", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeArtifactFirst}},
		},
		Containers: []config.Container{{Name: "image"}},
	}
	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}

	configPlanJSON := mustConfigPlanJSON(t, pipeline.NewConfigPlan(cfg))

	got, err := appplan.Release(context.Background(), sink, nil, appplan.ReleaseInput{
		ConfigPlanJSON:       configPlanJSON,
		Branch:               "main",
		RefName:              "v1.2.3",
		ReleasePublisher:     "github-cli",
		ReleaseSBOMs:         "all",
		ReleaseSignArtifacts: true,
		ChangelogCreator:     "git-cliff",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !got.Policy.CreateRelease || !got.Policy.RunVersionBump || !got.Policy.RequireAllowlistedSigner {
		t.Errorf("policy = %+v", got.Policy)
	}

	var planJSON pipeline.ReleasePlan
	if err := json.Unmarshal([]byte(sink.Single("release-plan-json")), &planJSON); err != nil {
		t.Fatal(err)
	}

	if planJSON.Stages.Build.Stage != "build" || !planJSON.Stages.Build.Targets.Go.Runs {
		t.Errorf("build-stage plan = %+v", planJSON.Stages.Build)
	}

	if !planJSON.Stages.Publish.Targets.MavenCentral.Runs || !planJSON.Stages.Publish.Targets.Containers.Runs {
		t.Errorf("publish-stage plan = %+v", planJSON.Stages.Publish)
	}

	var transferPlan pipeline.ArtifactTransferPlan
	if err := json.Unmarshal([]byte(sink.Single("artifact-transfer-plan-json")), &transferPlan); err != nil {
		t.Fatal(err)
	}

	if transferPlan.Version != pipeline.ArtifactTransferPlanVersion || len(transferPlan.Items) == 0 {
		t.Errorf("artifact-transfer-plan-json = %+v", transferPlan)
	}

	if got := sink.Single("release-policy-json"); got != "" {
		t.Errorf("release-policy-json compatibility output should not be emitted, got %s", got)
	}
}

func TestPlanRelease_SBOMConflictWarnsToSummary(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	summary := &fakeSummary{}

	cfg := &config.Config{
		Artifacts: []config.Artifact{{Name: "web", ProjectType: projecttype.NPM, SBOMs: "analyzed-container"}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}
	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}

	configPlanJSON := mustConfigPlanJSON(t, pipeline.NewConfigPlan(cfg))

	got, err := appplan.Release(context.Background(), sink, summary, appplan.ReleaseInput{
		ConfigPlanJSON: configPlanJSON,
		RefName:        "v1.2.3",
		ReleaseSBOMs:   "build",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.Policy.SBOMs != "none" {
		t.Errorf("policy = %+v", got.Policy)
	}

	if !strings.Contains(summary.buf.String(), "SBOM misconfiguration") {
		t.Errorf("summary = %q", summary.buf.String())
	}
}

func TestPlanRelease_MissingConfigPlanErrors(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	_, err := appplan.Release(context.Background(), sink, nil, appplan.ReleaseInput{})
	if err == nil || !strings.Contains(err.Error(), "config-plan-json is required") {
		t.Errorf("err = %v", err)
	}
}

func TestPlanRelease_RejectsUnsupportedConfigPlanVersion(t *testing.T) {
	t.Parallel()

	_, err := appplan.Release(context.Background(), fakeoutputsink.New(t), nil, appplan.ReleaseInput{
		ConfigPlanJSON: `{"version":2,"artifacts":{"all":[]},"containers":{"all":[],"has_containers":false}}`,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported config-plan version 2") {
		t.Errorf("err = %v", err)
	}
}

func TestPlanPR_EmitsTypedPlanOutputs(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	got, err := appplan.PR(context.Background(), sink, appplan.PRInput{
		ProjectType: "go",
		Nanolinter:  true,
		SwiftLint:   true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.Context.ProjectType != projecttype.Go || !got.Policy.Swift {
		t.Errorf("pr plan = %+v", got)
	}

	if out := sink.Single("pr-plan-json"); !strings.Contains(out, `"project_type":"go"`) || !strings.Contains(out, `"swift":true`) {
		t.Errorf("pr-plan-json = %s", out)
	}

	if out := sink.Single("quality-stage-plan-json"); !strings.Contains(out, `"stage":"pr-quality"`) || !strings.Contains(out, `"nanolinter":{"runs":true`) {
		t.Errorf("quality-stage-plan-json = %s", out)
	}
}

func TestPlanPR_RejectsUnknownProjectType(t *testing.T) {
	t.Parallel()

	_, err := appplan.PR(context.Background(), fakeoutputsink.New(t), appplan.PRInput{ProjectType: "rust"})
	if err == nil || !strings.Contains(err.Error(), "unknown project-type") {
		t.Fatalf("err = %v", err)
	}
}

//nolint:cyclop // verifies many typed plan outputs on one SnapshotRelease plan.
func TestPlanSnapshotRelease_EmitsTypedPlanOutputs(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	configPlanJSON := mustConfigPlanJSON(t, pipeline.NewConfigPlan(&config.Config{
		Artifacts: []config.Artifact{
			{Name: "web", ProjectType: projecttype.NPM},
			{Name: "worker", ProjectType: projecttype.Go, Go: &config.GoConfig{BuildMode: config.GoBuildModeArtifactFirst}},
		},
		Containers: []config.Container{{Name: "image"}},
	}))

	got, err := appplan.SnapshotRelease(context.Background(), sink, appplan.SnapshotReleaseInput{
		ConfigPlanJSON:      configPlanJSON,
		Branch:              "feature/dev-plan",
		ReleaseSHA:          "abc123",
		ReleaseActor:        "octocat",
		ReleaseRepository:   "org/repo",
		Registry:            "ghcr.io",
		ReusableCIBinaryRef: "v3.0.0",
		NPMRegistry:         "https://npm.pkg.github.com",
		PackageScope:        "@org",
		SBOMs:               "analyzed-artifact",
		PublishNPM:          true,
		UseCIToken:          true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.Context.ProjectType != projecttype.NPM {
		t.Errorf("project type = %q", got.Context.ProjectType)
	}

	var planJSON pipeline.SnapshotReleasePlan
	if err := json.Unmarshal([]byte(sink.Single("snapshot-release-plan-json")), &planJSON); err != nil {
		t.Fatal(err)
	}

	if planJSON.Stages.Build.Stage != "dev-build" || !planJSON.Stages.Build.Targets.Go.Runs {
		t.Errorf("build stage plan = %+v", planJSON.Stages.Build)
	}

	if planJSON.Context.Branch != "feature/dev-plan" || !planJSON.Policy.PublishNPM || !planJSON.Policy.UseCIToken {
		t.Errorf("dev release plan context/policy = %+v %+v", planJSON.Context, planJSON.Policy)
	}

	var transferPlan pipeline.ArtifactTransferPlan
	if err := json.Unmarshal([]byte(sink.Single("artifact-transfer-plan-json")), &transferPlan); err != nil {
		t.Fatal(err)
	}

	if transferPlan.Version != pipeline.ArtifactTransferPlanVersion || len(transferPlan.Items) == 0 {
		t.Errorf("artifact-transfer-plan-json = %+v", transferPlan)
	}

	if got := sink.Single("dev-context-json"); got != "" {
		t.Errorf("dev-context-json compatibility output should not be emitted, got %s", got)
	}

	if got := sink.Single("dev-policy-json"); got != "" {
		t.Errorf("dev-policy-json compatibility output should not be emitted, got %s", got)
	}
}

func TestPlanSnapshotRelease_ProjectTypeOverrideWins(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	configPlanJSON := mustConfigPlanJSON(t, pipeline.NewConfigPlan(&config.Config{
		Artifacts: []config.Artifact{{Name: "web", ProjectType: projecttype.NPM}},
	}))

	got, err := appplan.SnapshotRelease(context.Background(), sink, appplan.SnapshotReleaseInput{
		ConfigPlanJSON: configPlanJSON,
		ProjectType:    "maven", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.Context.ProjectType != projecttype.Maven {
		t.Errorf("project type = %q", got.Context.ProjectType)
	}
}

func TestPlanSnapshotRelease_MissingConfigPlanErrors(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	_, err := appplan.SnapshotRelease(context.Background(), sink, appplan.SnapshotReleaseInput{})
	if err == nil || !strings.Contains(err.Error(), "config-plan-json is required") {
		t.Errorf("err = %v", err)
	}
}

func TestPlanSnapshotRelease_RejectsUnsupportedConfigPlanVersion(t *testing.T) {
	t.Parallel()

	_, err := appplan.SnapshotRelease(context.Background(), fakeoutputsink.New(t), appplan.SnapshotReleaseInput{
		ConfigPlanJSON: `{"version":2,"artifacts":{"all":[]},"containers":{"all":[],"has_containers":false}}`,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported config-plan version 2") {
		t.Errorf("err = %v", err)
	}
}

func mustConfigPlanJSON(t *testing.T, plan pipeline.ConfigPlan) string {
	t.Helper()

	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func TestGetFilePattern_KnownProjectType(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	var buf bytes.Buffer

	got, err := appplan.GetFilePattern(context.Background(), sink, &buf, appplan.GetFilePatternInput{
		ProjectType:   "npm", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		WriteToOutput: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "CHANGELOG.md package.json package-lock.json"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if !strings.Contains(buf.String(), want) {
		t.Errorf("out = %q", buf.String())
	}

	if v := sink.Single("pattern"); v != want {
		t.Errorf("sink pattern = %q", v)
	}
}

func TestGetFilePattern_CustomOverride(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	got, err := appplan.GetFilePattern(context.Background(), sink, &bytes.Buffer{}, appplan.GetFilePatternInput{
		ProjectType:   "npm",
		CustomPattern: "MY/CUSTOM",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got != "MY/CUSTOM" {
		t.Errorf("custom override should win, got %q", got)
	}
}

func TestGetFilePattern_CustomOverrideDoesNotRequireProjectType(t *testing.T) {
	t.Parallel()

	got, err := appplan.GetFilePattern(context.Background(), fakeoutputsink.New(t), &bytes.Buffer{}, appplan.GetFilePatternInput{
		CustomPattern: "custom/path",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got != "custom/path" {
		t.Errorf("custom override = %q", got)
	}
}

func TestGetFilePattern_JSONOutput(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	got, err := appplan.GetFilePattern(context.Background(), fakeoutputsink.New(t), &out, appplan.GetFilePatternInput{
		ProjectType: "npm",
		Format:      output.FormatJSON,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := "CHANGELOG.md package.json package-lock.json"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if strings.TrimSpace(out.String()) != `{"pattern":"CHANGELOG.md package.json package-lock.json"}` {
		t.Errorf("out = %q", out.String())
	}
}

func TestGetFilePattern_GitHubOutputWritesSinkOnly(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	_, err := appplan.GetFilePattern(context.Background(), sink, &out, appplan.GetFilePatternInput{
		ProjectType: "maven",
		Format:      output.FormatGitHub,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("pattern"); got != "CHANGELOG.md :(glob)**/pom.xml" {
		t.Errorf("sink pattern = %q", got)
	}

	if out.String() != "" {
		t.Errorf("out = %q", out.String())
	}
}

func TestGetFilePattern_CustomOverrideWrittenToOutput(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	got, err := appplan.GetFilePattern(context.Background(), sink, &out, appplan.GetFilePatternInput{
		ProjectType:   "maven",
		CustomPattern: "pom.xml package.json", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		WriteToOutput: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got != "pom.xml package.json" {
		t.Errorf("got %q", got)
	}

	if sink.Single("pattern") != "pom.xml package.json" {
		t.Errorf("sink pattern = %q", sink.Single("pattern"))
	}

	if strings.TrimSpace(out.String()) != "pom.xml package.json" {
		t.Errorf("out = %q", out.String())
	}
}

func TestGetFilePattern_EmptyCustomPatternFallsBackToDefault(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	got, err := appplan.GetFilePattern(context.Background(), sink, &bytes.Buffer{}, appplan.GetFilePatternInput{
		ProjectType:   "maven",
		CustomPattern: "",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got != "CHANGELOG.md :(glob)**/pom.xml" {
		t.Errorf("got %q", got)
	}
}

func TestGetFilePattern_MetaDefaultsToChangelog(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	got, err := appplan.GetFilePattern(context.Background(), sink, &bytes.Buffer{}, appplan.GetFilePatternInput{
		ProjectType: "meta",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got != "CHANGELOG.md" {
		t.Errorf("got %q", got)
	}
}

func TestGetFilePattern_EmptyProjectTypeErrors(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	_, err := appplan.GetFilePattern(context.Background(), sink, &bytes.Buffer{}, appplan.GetFilePatternInput{})
	if err == nil || !strings.Contains(err.Error(), "project type is required") {
		t.Errorf("err = %v", err)
	}
}

func TestGetFilePattern_UnknownProjectTypeErrors(t *testing.T) {
	t.Parallel()

	_, err := appplan.GetFilePattern(context.Background(), fakeoutputsink.New(t), &bytes.Buffer{}, appplan.GetFilePatternInput{ProjectType: "unknown"})
	if err == nil || !strings.Contains(err.Error(), "unknown project-type") {
		t.Errorf("err = %v", err)
	}
}
