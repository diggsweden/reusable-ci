// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package plan_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/plan"
)

func base() plan.ReleasePlanInputs {
	return plan.ReleasePlanInputs{
		ReleaseSBOMs:  "all",
		PipelineSBOMs: "none",
		RefName:       "v1.0.0",
	}
}

func TestResolve_StableTagInfersMakeLatest(t *testing.T) {
	t.Parallel()
	in := base()
	in.RefName = "v1.0.0"
	in.ReleaseType = ""
	got, err := plan.ResolveReleasePlan(in)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ShouldMakeLatest {
		t.Errorf("v1.0.0 with no release_type should be stable")
	}
}

func TestResolve_PrereleaseTagNotStable(t *testing.T) {
	t.Parallel()
	in := base()
	in.RefName = "v1.0.0-rc.1"
	got, _ := plan.ResolveReleasePlan(in)
	if got.ShouldMakeLatest {
		t.Errorf("prerelease tag should not be stable")
	}
	if got.ShouldCreateDraftRelease {
		t.Errorf("semver prerelease tag should not become draft")
	}
}

func TestResolve_ExplicitStableOverridesTagShape(t *testing.T) {
	t.Parallel()
	in := base()
	in.ReleaseType = "stable"
	in.RefName = "v1.0.0-rc.1"
	got, _ := plan.ResolveReleasePlan(in)
	if !got.ShouldMakeLatest {
		t.Errorf("release_type=stable wins over tag shape")
	}
}

func TestResolve_DraftReleaseDecisions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		refName      string
		releaseDraft bool
	}{
		{name: "uppercase_snapshot", refName: "v1.0.0-SNAPSHOT"},
		{name: "lowercase_snapshot", refName: "v1.0.0-snapshot"},
		{name: "non_semver", refName: "weird-tag"},
		{name: "explicit_draft_flag", refName: "v1.0.0", releaseDraft: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			in := base()
			in.RefName = testCase.refName
			in.ReleaseDraft = testCase.releaseDraft
			got, _ := plan.ResolveReleasePlan(in)
			if !got.ShouldCreateDraftRelease {
				t.Errorf("ref=%q release_draft=%v should create draft", testCase.refName, testCase.releaseDraft)
			}
		})
	}
}

func TestResolve_CreateReleaseGatesOnPublisher(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"github-cli": true,
		"":           false,
		"forgejo":    false,
	}
	for publisher, want := range cases {
		in := base()
		in.ReleasePublisher = publisher
		got, _ := plan.ResolveReleasePlan(in)
		if got.ShouldCreateRelease != want {
			t.Errorf("publisher=%q → CreateRelease=%v, want %v", publisher, got.ShouldCreateRelease, want)
		}
	}
}

func TestResolve_AuthorizationDisjunction(t *testing.T) {
	t.Parallel()
	cases := []struct {
		any, release, want bool
	}{
		{false, false, false},
		{true, false, true},
		{false, true, true},
		{true, true, true},
	}
	for _, c := range cases {
		in := base()
		in.AnyRequireAuthorization = c.any
		in.ReleaseCheckAuthorization = c.release
		got, _ := plan.ResolveReleasePlan(in)
		if got.ShouldCheckAuthorization != c.want {
			t.Errorf("any=%v release=%v → %v, want %v", c.any, c.release, got.ShouldCheckAuthorization, c.want)
		}
	}
}

func TestResolve_VersionBumpRequiresGitCliffAndNotSkipped(t *testing.T) {
	t.Parallel()
	cases := []struct {
		creator string
		skip    bool
		want    bool
	}{
		{"git-cliff", false, true},
		{"git-cliff", true, false},
		{"", false, false},
		{"changie", false, false},
	}
	for _, c := range cases {
		in := base()
		in.ChangelogCreator = c.creator
		in.ChangelogSkipVersionBump = c.skip
		got, _ := plan.ResolveReleasePlan(in)
		if got.ShouldRunVersionBump != c.want {
			t.Errorf("creator=%q skip=%v → %v, want %v", c.creator, c.skip, got.ShouldRunVersionBump, c.want)
		}
	}
}

func TestResolve_EffectiveSBOMsIntersection(t *testing.T) {
	t.Parallel()
	in := base()
	in.ReleaseSBOMs = "all"
	in.PipelineSBOMs = "build,analyzed-artifact"
	got, _ := plan.ResolveReleasePlan(in)
	if got.EffectiveSBOMs != "build,analyzed-artifact" {
		t.Errorf("effective = %q", got.EffectiveSBOMs)
	}
	if got.SBOMConflict != nil {
		t.Errorf("no conflict expected: %+v", got.SBOMConflict)
	}
}

func TestResolve_EffectiveSBOMsIntersectionCanonicalOrder(t *testing.T) {
	t.Parallel()
	in := base()
	in.ReleaseSBOMs = "analyzed-container,build"
	in.PipelineSBOMs = "analyzed-artifact,analyzed-container,build"
	got, _ := plan.ResolveReleasePlan(in)
	if got.EffectiveSBOMs != "build,analyzed-container" {
		t.Errorf("effective = %q", got.EffectiveSBOMs)
	}
}

func TestResolve_EffectiveSBOMsDisjointConflict(t *testing.T) {
	t.Parallel()
	in := base()
	in.ReleaseSBOMs = "build"
	in.PipelineSBOMs = "analyzed-container"
	got, _ := plan.ResolveReleasePlan(in)
	if got.EffectiveSBOMs != "none" {
		t.Errorf("disjoint should yield 'none', got %q", got.EffectiveSBOMs)
	}
	if got.SBOMConflict == nil {
		t.Error("expected SBOMConflict to be set on disjoint non-none configs")
	}
}

func TestResolve_EffectiveSBOMsNoneNoConflict(t *testing.T) {
	t.Parallel()
	in := base()
	in.ReleaseSBOMs = "none"
	in.PipelineSBOMs = "build"
	got, _ := plan.ResolveReleasePlan(in)
	if got.EffectiveSBOMs != "none" {
		t.Errorf("effective = %q", got.EffectiveSBOMs)
	}
	if got.SBOMConflict != nil {
		t.Error("explicit 'none' should not flag a conflict")
	}
}

func TestResolve_HasContainersPassthrough(t *testing.T) {
	t.Parallel()
	in := base()
	in.HasContainers = true
	got, _ := plan.ResolveReleasePlan(in)
	if !got.HasContainers {
		t.Error("HasContainers should pass through")
	}
}

func TestResolve_BadSBOMValueErrors(t *testing.T) {
	t.Parallel()
	in := base()
	in.ReleaseSBOMs = "banana"
	if _, err := plan.ResolveReleasePlan(in); err == nil {
		t.Fatal("expected error on bad release_sboms")
	}
}
