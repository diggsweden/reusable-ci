// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

func TestDerive_DefaultsSBOMsAndContainerFields(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Artifacts: []config.Artifact{
			{Name: "lib", ProjectType: projecttype.Maven},
			{Name: "meta", ProjectType: projecttype.Meta},
			{Name: "web", ProjectType: projecttype.NPM, SBOMs: "build,analyzed-container"},
		},
		Containers: []config.Container{{
			Name:      "image",
			From:      []string{"web", "meta", "missing"},
			BuildArgs: map[string]string{"ZETA": "last", "ALPHA": "1"},
		}},
	}

	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}

	if got := cfg.Artifacts[0].SBOMs; got != "all" {
		t.Errorf("maven default sboms = %q, want all", got)
	}
	if got := cfg.Artifacts[0].EffectiveSBOMs; !reflect.DeepEqual(got, []config.SBOMLayer{
		config.SBOMLayerBuild, config.SBOMLayerAnalyzedArtifact, config.SBOMLayerAnalyzedContainer,
	}) {
		t.Errorf("maven effective sboms = %v", got)
	}
	if got := cfg.Artifacts[1].SBOMs; got != "none" {
		t.Errorf("meta default sboms = %q, want none", got)
	}
	if got := cfg.Artifacts[2].SBOMs; got != "build,analyzed-container" {
		t.Errorf("explicit sboms changed to %q", got)
	}

	c := cfg.Containers[0]
	if got, want := c.ArtifactTypes, []projecttype.Type{projecttype.NPM, projecttype.Meta}; !reflect.DeepEqual(got, want) {
		t.Errorf("artifact types = %v, want %v", got, want)
	}
	if !c.EnableAnalyzedContainerSBOM {
		t.Error("expected analyzed-container SBOM to be enabled from referenced artifact")
	}
	if got, want := c.BuildArgsString, "ALPHA=1\nZETA=last"; got != want {
		t.Errorf("build args string = %q, want %q", got, want)
	}
}

func TestDerive_InvalidSBOMNamesArtifact(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Artifacts: []config.Artifact{{Name: "bad", ProjectType: projecttype.NPM, SBOMs: "build,bogus"}}}
	err := config.Derive(cfg)
	if err == nil || !strings.Contains(err.Error(), `artefact "bad"`) || !strings.Contains(err.Error(), "unknown token") {
		t.Fatalf("err = %v", err)
	}
}

func TestArtifactFilters(t *testing.T) {
	t.Parallel()

	artifacts := []config.Artifact{
		{Name: "app", ProjectType: projecttype.Maven, BuildType: config.BuildTypeApplication, PublishTo: []config.PublishTarget{config.PublishGitHubPackages}, RequireAuthorization: true},
		{Name: "lib", ProjectType: projecttype.Maven, BuildType: config.BuildTypeLibrary, PublishTo: []config.PublishTarget{config.PublishGitHubPackages}},
		{Name: "pkg", ProjectType: projecttype.NPM, PublishTo: []config.PublishTarget{config.PublishNPMJS, config.PublishGitHubPackages}},
	}

	if !config.AnyRequireAuthorization(artifacts) {
		t.Fatal("expected authorization requirement")
	}
	if config.AnyRequireAuthorization(artifacts[1:]) {
		t.Fatal("did not expect authorization requirement")
	}
	if got := names(config.ArtifactsByProjectType(artifacts, projecttype.Maven)); !reflect.DeepEqual(got, []string{"app", "lib"}) {
		t.Errorf("maven artifacts = %v", got)
	}
	if got := names(config.ArtifactsByPublishTarget(artifacts, config.PublishGitHubPackages)); !reflect.DeepEqual(got, []string{"lib", "pkg"}) {
		t.Errorf("github-packages artifacts = %v, want maven app excluded", got)
	}
	if got := names(config.ArtifactsByPublishTarget(artifacts, config.PublishNPMJS)); !reflect.DeepEqual(got, []string{"pkg"}) {
		t.Errorf("npmjs artifacts = %v", got)
	}
}

func names(artifacts []config.Artifact) []string {
	out := make([]string, len(artifacts))
	for i, a := range artifacts {
		out[i] = a.Name
	}
	return out
}
