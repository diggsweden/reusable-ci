// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/internal/domain/release"
)

func TestResolveArtifactNames_PackageOverridesUseBuildSuffix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		projType projecttype.Type
		prefix   string
	}{
		{projecttype.Maven, "maven-lib"},
		{projecttype.NPM, "web"},
	} {
		got := release.ResolveArtifactNames(tc.projType, tc.prefix)

		want := release.ArtifactNamePair{
			BuildArtifact: tc.prefix + "-build-artifacts",
			SBOMArtifact:  tc.prefix + "-build-sbom",
		}
		if got != want {
			t.Errorf("%s override -> %+v, want %+v", tc.projType, got, want)
		}
	}
}

func TestResolveArtifactNames_GoWithOverride(t *testing.T) {
	t.Parallel()

	got := release.ResolveArtifactNames(projecttype.Go, "go-cli")

	want := release.ArtifactNamePair{
		BuildArtifact: "go-cli-go-build-artifacts",
		SBOMArtifact:  "go-cli-go-build-sbom",
	}
	if got != want {
		t.Errorf("go override -> %+v, want %+v", got, want)
	}
}

func TestResolveArtifactNames_GradleWithOverride(t *testing.T) {
	t.Parallel()

	got := release.ResolveArtifactNames(projecttype.Gradle, "my-component")

	want := release.ArtifactNamePair{
		BuildArtifact: "my-component-build-artifacts",
		SBOMArtifact:  "my-component-build-sbom",
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

func TestResolveArtifactNames_CargoArtefactFirstShape(t *testing.T) {
	t.Parallel()
	// Cargo mirrors Go: artefact-first cargo uploads a real
	// `<name>-cargo-build-artifacts` plus a separate `<name>-cargo-build-sbom`.
	// Container-first cargo still uses the SBOM name (via sbom-cargo.yml);
	// its BuildArtifact slot is gated to "" by buildArtifactName in the
	// planner, so the value here is only consulted when artefact-first.
	got := release.ResolveArtifactNames(projecttype.Cargo, "")
	if got.BuildArtifact != "cargo-build-artifacts" {
		t.Errorf("cargo default build-artifact = %q, want %q", got.BuildArtifact, "cargo-build-artifacts")
	}

	if got.SBOMArtifact != "cargo-build-sbom" {
		t.Errorf("cargo default sbom-artifact = %q, want %q", got.SBOMArtifact, "cargo-build-sbom")
	}

	got = release.ResolveArtifactNames(projecttype.Cargo, "core")
	if got.BuildArtifact != "core-cargo-build-artifacts" {
		t.Errorf("cargo override build-artifact = %q, want %q", got.BuildArtifact, "core-cargo-build-artifacts")
	}

	if got.SBOMArtifact != "core-cargo-build-sbom" {
		t.Errorf("cargo override sbom-artifact = %q, want %q", got.SBOMArtifact, "core-cargo-build-sbom")
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
		"owner/repo":        "repo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
