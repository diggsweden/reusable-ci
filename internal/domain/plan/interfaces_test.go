// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package plan_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/plan"
)

func TestMarshalReleasePolicy_AllFalseExactShape(t *testing.T) {
	t.Parallel()
	b, err := plan.MarshalReleasePolicy(plan.ReleasePolicyEnvelope{
		SBOMs: "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"sign_artifacts":false,"check_authorization":false,"run_version_bump":false,"create_release":false,"create_draft_release":false,"sboms":"none","make_latest":false,"has_containers":false}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}

func TestMarshalReleasePolicy_AllTrueShape(t *testing.T) {
	t.Parallel()
	b, _ := plan.MarshalReleasePolicy(plan.ReleasePolicyEnvelope{
		SignArtifacts:      true,
		CheckAuthorization: true,
		RunVersionBump:     true,
		CreateRelease:      true,
		CreateDraftRelease: true,
		SBOMs:              "all",
		MakeLatest:         true,
		HasContainers:      true,
	})
	want := `{"sign_artifacts":true,"check_authorization":true,"run_version_bump":true,"create_release":true,"create_draft_release":true,"sboms":"all","make_latest":true,"has_containers":true}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}

func TestMarshalDevContext_FieldOrder(t *testing.T) {
	t.Parallel()
	b, _ := plan.MarshalDevContext(plan.DevContext{
		ProjectType:       "npm",
		Branch:            "main",
		ReleaseSHA:        "abcdef0",
		ReleaseActor:      "bot",
		ReleaseRepository: "owner/repo",
		WorkingDirectory:  ".",
		JavaVersion:       "21",
		NodeVersion:       "20",
		RustToolchain:     "stable",
		Registry:          "ghcr.io",
		ScriptsRef:        "v1",
		NPMRegistry:       "https://registry.npmjs.org",
		PackageScope:      "@diggsweden",
	})
	want := `{"project_type":"npm","branch":"main","release_sha":"abcdef0","release_actor":"bot","release_repository":"owner/repo","working_directory":".","java_version":"21","node_version":"20","rust_toolchain":"stable","registry":"ghcr.io","scripts_ref":"v1","npm_registry":"https://registry.npmjs.org","package_scope":"@diggsweden"}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}

func TestMarshalDevPolicy_FieldOrder(t *testing.T) {
	t.Parallel()

	b, _ := plan.MarshalDevPolicy(plan.DevPolicy{PublishNPM: true, UseCIToken: true})
	want := `{"publish_npm":true,"use_ci_token":true}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}

func TestMarshalPRContext_FieldOrder(t *testing.T) {
	t.Parallel()

	b, _ := plan.MarshalPRContext(plan.PRContext{
		ProjectType:                "maven",
		BaseBranch:                 "main",
		ScriptsRef:                 "v1",
		SASTOpengrepRules:          "p/default",
		SASTOpengrepFailOnSeverity: "high",
	})
	want := `{"project_type":"maven","base_branch":"main","scripts_ref":"v1","sast_opengrep_rules":"p/default","sast_opengrep_fail_on_severity":"high"}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}

func TestBuildPRPolicy_SwiftDisjunction(t *testing.T) {
	t.Parallel()
	cases := []struct {
		fmt, lint, swift bool
	}{
		{false, false, false},
		{true, false, true},
		{false, true, true},
		{true, true, true},
	}
	for _, c := range cases {
		got := plan.BuildPRPolicy(plan.PRPolicy{SwiftFormat: c.fmt, SwiftLint: c.lint})
		if got.Swift != c.swift {
			t.Errorf("fmt=%v lint=%v → swift=%v, want %v", c.fmt, c.lint, got.Swift, c.swift)
		}
	}
}

func TestMarshalPRPolicy_FieldOrder(t *testing.T) {
	t.Parallel()
	b, _ := plan.MarshalPRPolicy(plan.BuildPRPolicy(plan.PRPolicy{
		DependencyReview: true,
		SASTOpengrep:     true,
		PublicCodeLint:   true,
		DevbaseCheck:     true,
		SwiftFormat:      true,
		SwiftLint:        false,
	}))
	want := `{"dependencyreview":true,"sastopengrep":true,"publiccodelint":true,"devbasecheck":true,"swiftformat":true,"swiftlint":false,"swift":true}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}
