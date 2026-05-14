// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package plan_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	appplan "github.com/diggsweden/reusable-ci/internal/app/plan"
	domainplan "github.com/diggsweden/reusable-ci/internal/domain/plan"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
)

// fakeSummary records every Append for assertions.
type fakeSummary struct{ buf bytes.Buffer }

func (f *fakeSummary) Append(_ context.Context, s string) error {
	f.buf.WriteString(s)
	return nil
}

func TestResolveReleasePlan_EmitsAllScalars(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	in := domainplan.ReleasePlanInputs{
		ReleaseType:      "stable",
		ReleasePublisher: "github-cli",
		ReleaseSBOMs:     "all",
		PipelineSBOMs:    "all",
		RefName:          "v1.0.0",
		ChangelogCreator: "git-cliff",
		ReleaseSignArtifacts:    true,
		AnyRequireAuthorization: true,
		HasContainers:           true,
	}
	got, err := appplan.ResolveReleasePlan(context.Background(), sink, nil, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.EffectiveSBOMs != "build,analyzed-artifact,analyzed-container" {
		t.Errorf("effective = %q", got.EffectiveSBOMs)
	}
	if v := sink.Single("should-make-latest"); v != "true" {
		t.Errorf("should-make-latest = %q", v)
	}
	if v := sink.Single("should-create-release"); v != "true" {
		t.Errorf("should-create-release = %q", v)
	}
	if v := sink.Single("has-containers"); v != "true" {
		t.Errorf("has-containers = %q", v)
	}
	if v := sink.Single("should-sign-artifacts"); v != "true" {
		t.Errorf("should-sign-artifacts = %q", v)
	}
	if v := sink.Single("should-check-authorization"); v != "true" {
		t.Errorf("should-check-authorization = %q", v)
	}
	if v := sink.Single("should-run-version-bump"); v != "true" {
		t.Errorf("should-run-version-bump = %q", v)
	}
	if v := sink.Single("should-create-draft-release"); v != "false" {
		t.Errorf("should-create-draft-release = %q", v)
	}
	if v := sink.Single("effective-sboms"); v != "build,analyzed-artifact,analyzed-container" {
		t.Errorf("effective-sboms = %q", v)
	}
}

func TestResolveReleasePlan_PrereleaseFallbackOutputs(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	in := domainplan.ReleasePlanInputs{
		ReleaseType:               "",
		ReleasePublisher:          "",
		ReleaseCheckAuthorization: false,
		ReleaseDraft:              false,
		ReleaseSBOMs:              "none",
		ReleaseSignArtifacts:      false,
		ChangelogCreator:          "",
		ChangelogSkipVersionBump:  true,
		RefName:                   "v1.2.3-beta.1",
		PipelineSBOMs:             "none",
		AnyRequireAuthorization:   false,
		HasContainers:             false,
	}
	if _, err := appplan.ResolveReleasePlan(context.Background(), sink, nil, in); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"should-make-latest":         "false",
		"has-containers":             "false",
		"effective-sboms":            "none",
		"should-sign-artifacts":      "false",
		"should-create-release":      "false",
		"should-check-authorization": "false",
		"should-run-version-bump":    "false",
		"should-create-draft-release": "false",
	} {
		if got := sink.Single(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestResolveReleasePlan_SBOMConflictWarnsToSummary(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	summary := &fakeSummary{}
	in := domainplan.ReleasePlanInputs{
		ReleaseSBOMs:  "build",
		PipelineSBOMs: "analyzed-container",
		RefName:       "v1.0.0",
	}
	if _, err := appplan.ResolveReleasePlan(context.Background(), sink, summary, in); err != nil {
		t.Fatal(err)
	}
	if v := sink.Single("effective-sboms"); v != "none" {
		t.Errorf("effective-sboms = %q (want none)", v)
	}
	if !strings.Contains(summary.buf.String(), "SBOM misconfiguration") {
		t.Errorf("summary missing SBOM misconfiguration block: %q", summary.buf.String())
	}
}

func TestResolveReleasePlan_NoConflictWhenPipelineNone(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	summary := &fakeSummary{}
	in := domainplan.ReleasePlanInputs{
		ReleaseSBOMs:  "all",
		PipelineSBOMs: "none",
		RefName:       "v1.0.0",
	}
	if _, err := appplan.ResolveReleasePlan(context.Background(), sink, summary, in); err != nil {
		t.Fatal(err)
	}
	if summary.buf.Len() != 0 {
		t.Errorf("explicit 'none' should not produce summary block: %q", summary.buf.String())
	}
}

func TestWriteReleaseInterface_AllFalseExactJSON(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	if err := appplan.WriteReleaseInterface(context.Background(), sink, appplan.WriteReleaseInterfaceInput{
		SBOMs: "none",
	}); err != nil {
		t.Fatal(err)
	}
	got := sink.Single("release-policy-json")
	want := `{"sign_artifacts":false,"check_authorization":false,"run_version_bump":false,"create_release":false,"create_draft_release":false,"sboms":"none","make_latest":false,"has_containers":false}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestWriteReleaseInterface_AllTrueExactJSON(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	if err := appplan.WriteReleaseInterface(context.Background(), sink, appplan.WriteReleaseInterfaceInput{
		SignArtifacts:      true,
		CheckAuthorization: true,
		RunVersionBump:     true,
		CreateRelease:      true,
		CreateDraftRelease: true,
		SBOMs:              "all",
		MakeLatest:         true,
		HasContainers:      true,
	}); err != nil {
		t.Fatal(err)
	}
	got := sink.Single("release-policy-json")
	want := `{"sign_artifacts":true,"check_authorization":true,"run_version_bump":true,"create_release":true,"create_draft_release":true,"sboms":"all","make_latest":true,"has_containers":true}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestWriteReleaseInterface_MixedBooleans(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	if err := appplan.WriteReleaseInterface(context.Background(), sink, appplan.WriteReleaseInterfaceInput{
		SignArtifacts: true,
		SBOMs:         "all",
		HasContainers: true,
	}); err != nil {
		t.Fatal(err)
	}
	body := sink.Single("release-policy-json")
	for _, want := range []string{
		`"sign_artifacts":true`,
		`"check_authorization":false`,
		`"run_version_bump":false`,
		`"create_release":false`,
		`"create_draft_release":false`,
		`"sboms":"all"`,
		`"make_latest":false`,
		`"has_containers":true`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %s: %s", want, body)
		}
	}
}

func TestWriteDevReleaseInterface_RustToolchainDefault(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	if err := appplan.WriteDevReleaseInterface(context.Background(), sink, appplan.WriteDevReleaseInterfaceInput{
		DevContext: domainplan.DevContext{ProjectType: "npm", Branch: "main"},
	}); err != nil {
		t.Fatal(err)
	}
	var ctx domainplan.DevContext
	if err := json.Unmarshal([]byte(sink.Single("dev-context-json")), &ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.RustToolchain != "stable" {
		t.Errorf("rust_toolchain = %q, want stable (default)", ctx.RustToolchain)
	}
}

func TestWriteDevReleaseInterface_AllFieldsAndPolicy(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	if err := appplan.WriteDevReleaseInterface(context.Background(), sink, appplan.WriteDevReleaseInterfaceInput{
		DevContext: domainplan.DevContext{
			ProjectType:       "npm",
			Branch:            "feature/my-branch",
			ReleaseSHA:        "abc123def456",
			ReleaseActor:      "test-user",
			ReleaseRepository: "org/repo",
			WorkingDirectory:  ".",
			JavaVersion:       "21",
			NodeVersion:       "20",
			Registry:          "ghcr.io",
			ScriptsRef:        "v2.5.0",
			NPMRegistry:       "https://npm.pkg.github.com",
			PackageScope:      "@myorg",
		},
		DevPolicy: domainplan.DevPolicy{PublishNPM: true, UseCIToken: true},
	}); err != nil {
		t.Fatal(err)
	}
	ctxJSON := sink.Single("dev-context-json")
	for _, want := range []string{
		`"project_type":"npm"`,
		`"branch":"feature/my-branch"`,
		`"release_sha":"abc123def456"`,
		`"release_actor":"test-user"`,
		`"release_repository":"org/repo"`,
		`"working_directory":"."`,
		`"java_version":"21"`,
		`"node_version":"20"`,
		`"registry":"ghcr.io"`,
		`"scripts_ref":"v2.5.0"`,
		`"npm_registry":"https://npm.pkg.github.com"`,
		`"package_scope":"@myorg"`,
	} {
		if !strings.Contains(ctxJSON, want) {
			t.Errorf("dev-context-json missing %s: %s", want, ctxJSON)
		}
	}
	if got := sink.Single("dev-policy-json"); got != `{"publish_npm":true,"use_ci_token":true}` {
		t.Errorf("dev-policy-json = %s", got)
	}
}

func TestWriteDevReleaseInterface_PolicyBooleans(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   domainplan.DevPolicy
		want string
	}{
		{name: "publish false use-ci true", in: domainplan.DevPolicy{PublishNPM: false, UseCIToken: true}, want: `{"publish_npm":false,"use_ci_token":true}`},
		{name: "publish true use-ci false", in: domainplan.DevPolicy{PublishNPM: true, UseCIToken: false}, want: `{"publish_npm":true,"use_ci_token":false}`},
		{name: "both false", in: domainplan.DevPolicy{PublishNPM: false, UseCIToken: false}, want: `{"publish_npm":false,"use_ci_token":false}`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			sink := fakeoutputsink.New(t)
			if err := appplan.WriteDevReleaseInterface(context.Background(), sink, appplan.WriteDevReleaseInterfaceInput{
				DevContext: domainplan.DevContext{ProjectType: "npm"},
				DevPolicy:  testCase.in,
			}); err != nil {
				t.Fatal(err)
			}
			if got := sink.Single("dev-policy-json"); got != testCase.want {
				t.Errorf("got  %s\nwant %s", got, testCase.want)
			}
		})
	}
}

func TestWriteDevReleaseInterface_EmptyProjectTypeErrors(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	err := appplan.WriteDevReleaseInterface(context.Background(), sink, appplan.WriteDevReleaseInterfaceInput{})
	if err == nil || !strings.Contains(err.Error(), "project-type is empty") {
		t.Errorf("err = %v", err)
	}
}

func TestWritePRInterface_SwiftPolicyDerived(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	in := appplan.WritePRInterfaceInput{
		PRContext: domainplan.PRContext{ProjectType: "go"},
		PRPolicy:  domainplan.PRPolicy{SwiftFormat: true},
	}
	if err := appplan.WritePRInterface(context.Background(), sink, in); err != nil {
		t.Fatal(err)
	}
	var policy domainplan.PRPolicy
	if err := json.Unmarshal([]byte(sink.Single("pr-policy-json")), &policy); err != nil {
		t.Fatal(err)
	}
	if !policy.Swift {
		t.Errorf("swift should be true when swiftformat=true: %+v", policy)
	}
}

func TestWritePRInterface_ContextAndAllEnabledPolicy(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	in := appplan.WritePRInterfaceInput{
		PRContext: domainplan.PRContext{
			ProjectType:                "npm",
			BaseBranch:                 "develop",
			ScriptsRef:                 "v2.5.0",
			SASTOpengrepRules:          "rules/opengrep.yml",
			SASTOpengrepFailOnSeverity: "medium",
		},
		PRPolicy: domainplan.PRPolicy{
			DependencyReview: true,
			SASTOpengrep:     true,
			PublicCodeLint:   true,
			DevbaseCheck:     true,
			SwiftFormat:      true,
			SwiftLint:        true,
		},
	}
	if err := appplan.WritePRInterface(context.Background(), sink, in); err != nil {
		t.Fatal(err)
	}
	ctxJSON := sink.Single("pr-context-json")
	for _, want := range []string{
		`"project_type":"npm"`,
		`"base_branch":"develop"`,
		`"scripts_ref":"v2.5.0"`,
		`"sast_opengrep_rules":"rules/opengrep.yml"`,
		`"sast_opengrep_fail_on_severity":"medium"`,
	} {
		if !strings.Contains(ctxJSON, want) {
			t.Errorf("pr-context-json missing %s: %s", want, ctxJSON)
		}
	}
	body := sink.Single("pr-policy-json")
	for _, want := range []string{
		`"dependencyreview":true`,
		`"sastopengrep":true`,
		`"publiccodelint":true`,
		`"devbasecheck":true`,
		`"swiftformat":true`,
		`"swiftlint":true`,
		`"swift":true`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("pr-policy-json missing %s: %s", want, body)
		}
	}
}

func TestWritePRInterface_AllDisabledExactJSON(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	in := appplan.WritePRInterfaceInput{
		PRContext: domainplan.PRContext{ProjectType: "maven"},
		PRPolicy:  domainplan.PRPolicy{},
	}
	if err := appplan.WritePRInterface(context.Background(), sink, in); err != nil {
		t.Fatal(err)
	}
	if got := sink.Single("pr-policy-json"); got != `{"dependencyreview":false,"sastopengrep":false,"publiccodelint":false,"devbasecheck":false,"swiftformat":false,"swiftlint":false,"swift":false}` {
		t.Errorf("pr-policy-json = %s", got)
	}
}

func TestGetFilePattern_KnownProjectType(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	var buf bytes.Buffer
	got, err := appplan.GetFilePattern(context.Background(), sink, &buf, appplan.GetFilePatternInput{
		ProjectType:   "npm",
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
		t.Errorf("stdout = %q", buf.String())
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

func TestGetFilePattern_CustomOverrideWrittenToOutput(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	var out bytes.Buffer
	got, err := appplan.GetFilePattern(context.Background(), sink, &out, appplan.GetFilePatternInput{
		ProjectType:   "maven",
		CustomPattern: "pom.xml package.json",
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
		t.Errorf("stdout = %q", out.String())
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
	if err == nil || !strings.Contains(err.Error(), "PROJECT_TYPE is required") {
		t.Errorf("err = %v", err)
	}
}
