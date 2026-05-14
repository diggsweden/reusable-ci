// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/internal/domain/release"
)

func TestResolveArtifactNames_FixedNamesIgnoreOverride(t *testing.T) {
	t.Parallel()
	for _, projType := range []projecttype.Type{projecttype.Maven, projecttype.NPM, projecttype.Python, projecttype.Go} {
		// Override should be IGNORED for these types (they have fixed
		// upload names baked into their build workflows).
		got := release.ResolveArtifactNames(projType, "ignored-by-design")
		if got.BuildArtifact == "ignored-by-design" {
			t.Errorf("%s should not honour override, got %+v", projType, got)
		}
	}
}

func TestResolveArtifactNames_GradleWithOverride(t *testing.T) {
	t.Parallel()
	got := release.ResolveArtifactNames(projecttype.Gradle, "my-component")
	want := release.ArtifactNamePair{
		BuildArtifact: "my-component",
		SBOMArtifact:  "my-component-sbom",
	}
	if got != want {
		t.Errorf("gradle override → %+v, want %+v", got, want)
	}
}

func TestResolveArtifactNames_GradleDefault(t *testing.T) {
	t.Parallel()
	got := release.ResolveArtifactNames(projecttype.Gradle, "")
	want := release.ArtifactNamePair{
		BuildArtifact: "gradle-build-artifacts",
		SBOMArtifact:  "gradle-build-sbom",
	}
	if got != want {
		t.Errorf("gradle default → %+v, want %+v", got, want)
	}
}

func TestResolveArtifactNames_CargoSBOMOnly(t *testing.T) {
	t.Parallel()
	// cargo emits SBOM only — both names equal.
	got := release.ResolveArtifactNames(projecttype.Cargo, "")
	if got.BuildArtifact != got.SBOMArtifact {
		t.Errorf("cargo: build/sbom should match, got %+v", got)
	}
	if got.BuildArtifact != "cargo-build-sbom" {
		t.Errorf("cargo default = %+v", got)
	}
	got = release.ResolveArtifactNames(projecttype.Cargo, "core")
	if got.BuildArtifact != "core-cargo-build-sbom" {
		t.Errorf("cargo override = %+v", got)
	}
}

func TestResolveArtifactNames_UnknownProjectFallback(t *testing.T) {
	t.Parallel()
	got := release.ResolveArtifactNames(projecttype.Unknown, "")
	want := release.ArtifactNamePair{"build-artifacts", "build-sbom"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestProjectNameFromRepo_OverrideWins(t *testing.T) {
	t.Parallel()
	if got := release.ProjectNameFromRepo("custom-name", "owner/different"); got != "custom-name" {
		t.Errorf("got %q", got)
	}
}

func TestProjectNameFromRepo_DerivesBasename(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"owner/repo":        "repo",
		"group/sub/project": "project",
		"repo":              "repo",
		"":                  ".",
	}
	for in, want := range cases {
		got := release.ProjectNameFromRepo("", in)
		if got != want {
			t.Errorf("ProjectNameFromRepo(\"\", %q) = %q, want %q", in, got, want)
		}
	}
}
