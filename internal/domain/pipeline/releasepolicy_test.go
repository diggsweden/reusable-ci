// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline

import "testing"

// White-box tests for the unexported resolveReleasePolicy. The function
// is package-private because resolution is an implementation detail of
// NewReleasePlan, but the cases below have enough surface to deserve
// their own focused test file.

func baseInputs() releasePolicyInputs {
	return releasePolicyInputs{
		ReleaseSBOMs:  "all",
		PipelineSBOMs: "none",   //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		RefName:       "v1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}
}

func TestResolveReleasePolicy_StableTagInfersMakeLatest(t *testing.T) {
	t.Parallel()

	in := baseInputs()
	in.RefName = "v1.0.0"
	in.ReleaseType = ""

	got, err := resolveReleasePolicy(in)
	if err != nil {
		t.Fatal(err)
	}

	if !got.MakeLatest {
		t.Errorf("v1.0.0 with no release_type should be stable")
	}
}

func TestResolveReleasePolicy_PrereleaseTagNotStable(t *testing.T) {
	t.Parallel()

	in := baseInputs()
	in.RefName = "v1.0.0-rc.1"

	got, _ := resolveReleasePolicy(in)
	if got.MakeLatest {
		t.Errorf("prerelease tag should not be stable")
	}

	if got.CreateDraftRelease {
		t.Errorf("semver prerelease tag should not become draft")
	}
}

func TestResolveReleasePolicy_ExplicitStableOverridesTagShape(t *testing.T) {
	t.Parallel()

	in := baseInputs()
	in.ReleaseType = "stable"
	in.RefName = "v1.0.0-rc.1"

	got, _ := resolveReleasePolicy(in)
	if !got.MakeLatest {
		t.Errorf("release_type=stable wins over tag shape")
	}
}

func TestResolveReleasePolicy_DraftReleaseDecisions(t *testing.T) {
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

			in := baseInputs()
			in.RefName = testCase.refName
			in.ReleaseDraft = testCase.releaseDraft

			got, _ := resolveReleasePolicy(in)
			if !got.CreateDraftRelease {
				t.Errorf("ref=%q release_draft=%v should create draft", testCase.refName, testCase.releaseDraft)
			}
		})
	}
}

func TestResolveReleasePolicy_CreateReleaseGatesOnPublisher(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"github-cli": true,
		"":           false,
		"forgejo":    false,
	}
	for publisher, want := range cases {
		in := baseInputs()
		in.ReleasePublisher = publisher

		got, _ := resolveReleasePolicy(in)
		if got.CreateRelease != want {
			t.Errorf("publisher=%q → CreateRelease=%v, want %v", publisher, got.CreateRelease, want)
		}
	}
}

func TestResolveReleasePolicy_AuthorizationDisjunction(t *testing.T) {
	t.Parallel()

	cases := []struct {
		any, release, want bool
	}{
		{false, false, false},
		{true, false, true},
		{false, true, true},
		{true, true, true},
	}
	for _, c := range cases { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		in := baseInputs()
		in.AnyRequireAuthorization = c.any
		in.ReleaseRequireAllowlistedSigner = c.release

		got, _ := resolveReleasePolicy(in)
		if got.RequireAllowlistedSigner != c.want {
			t.Errorf("any=%v release=%v → %v, want %v", c.any, c.release, got.RequireAllowlistedSigner, c.want)
		}
	}
}

func TestResolveReleasePolicy_VersionBumpRequiresGitCliffAndNotSkipped(t *testing.T) {
	t.Parallel()

	cases := []struct {
		creator string
		skip    bool
		want    bool
	}{
		{"git-cliff", false, true}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"git-cliff", true, false},
		{"", false, false},
		{"changie", false, false},
	}
	for _, c := range cases { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		in := baseInputs()
		in.ChangelogCreator = c.creator
		in.ChangelogSkipVersionBump = c.skip

		got, _ := resolveReleasePolicy(in)
		if got.RunVersionBump != c.want {
			t.Errorf("creator=%q skip=%v → %v, want %v", c.creator, c.skip, got.RunVersionBump, c.want)
		}
	}
}

func TestResolveReleasePolicy_EffectiveSBOMsIntersection(t *testing.T) {
	t.Parallel()

	in := baseInputs()
	in.ReleaseSBOMs = "all"
	in.PipelineSBOMs = "build,analyzed-artifact"

	got, _ := resolveReleasePolicy(in)
	if got.SBOMs != "build,analyzed-artifact" {
		t.Errorf("SBOMs = %q", got.SBOMs)
	}

	if got.SBOMConflict != nil {
		t.Errorf("no conflict expected: %+v", got.SBOMConflict)
	}
}

func TestResolveReleasePolicy_EffectiveSBOMsIntersectionCanonicalOrder(t *testing.T) {
	t.Parallel()

	in := baseInputs()
	in.ReleaseSBOMs = "analyzed-container,build"
	in.PipelineSBOMs = "analyzed-artifact,analyzed-container,build"

	got, _ := resolveReleasePolicy(in)
	if got.SBOMs != "build,analyzed-container" {
		t.Errorf("SBOMs = %q", got.SBOMs)
	}
}

func TestResolveReleasePolicy_EffectiveSBOMsDisjointConflict(t *testing.T) {
	t.Parallel()

	in := baseInputs()
	in.ReleaseSBOMs = "build" //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	in.PipelineSBOMs = "analyzed-container"

	got, _ := resolveReleasePolicy(in)
	if got.SBOMs != "none" {
		t.Errorf("disjoint should yield 'none', got %q", got.SBOMs)
	}

	if got.SBOMConflict == nil {
		t.Error("expected SBOMConflict to be set on disjoint non-none configs")
	}
}

func TestResolveReleasePolicy_EffectiveSBOMsNoneNoConflict(t *testing.T) {
	t.Parallel()

	in := baseInputs()
	in.ReleaseSBOMs = "none"
	in.PipelineSBOMs = "build"

	got, _ := resolveReleasePolicy(in)
	if got.SBOMs != "none" {
		t.Errorf("SBOMs = %q", got.SBOMs)
	}

	if got.SBOMConflict != nil {
		t.Error("explicit 'none' should not flag a conflict")
	}
}

func TestResolveReleasePolicy_HasContainersPassthrough(t *testing.T) {
	t.Parallel()

	in := baseInputs()
	in.HasContainers = true

	got, _ := resolveReleasePolicy(in)
	if !got.HasContainers {
		t.Error("HasContainers should pass through")
	}
}

func TestResolveReleasePolicy_BadSBOMValueErrors(t *testing.T) {
	t.Parallel()

	in := baseInputs()

	in.ReleaseSBOMs = "banana"
	if _, err := resolveReleasePolicy(in); err == nil {
		t.Fatal("expected error on bad release_sboms")
	}
}
