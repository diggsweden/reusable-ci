// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

func TestDerive_DefaultsSBOMsAndContainerFields(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Artifacts: []config.Artifact{
			{Name: "lib", ProjectType: projecttype.Maven}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			{Name: "meta", ProjectType: projecttype.Meta},
			{Name: "web", ProjectType: projecttype.NPM, SBOMs: "build,analyzed-container"},
		},
		Containers: []config.Container{{
			Name:      "image",
			From:      []string{"web", "meta", "missing"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			BuildArgs: map[string]string{"ZETA": "last", "ALPHA": "1"},
		}},
	}

	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}

	if got := cfg.Artifacts[0].SBOMs; got != "all" {
		t.Errorf("maven default sboms = %q, want all", got)
	}

	if got := cfg.Artifacts[0].EffectiveSBOMs; !slices.Equal(got, []config.SBOMLayer{
		config.SBOMLayerBuild, config.SBOMLayerAnalyzedArtifact, config.SBOMLayerAnalyzedContainer,
	}) {
		t.Errorf("maven effective sboms = %v", got)
	}

	if got := cfg.Artifacts[1].SBOMs; got != "none" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("meta default sboms = %q, want none", got)
	}

	if got := cfg.Artifacts[2].SBOMs; got != "build,analyzed-container" {
		t.Errorf("explicit sboms changed to %q", got)
	}

	c := cfg.Containers[0] //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if got, want := c.ArtifactTypes, []projecttype.Type{projecttype.NPM, projecttype.Meta}; !slices.Equal(got, want) {
		t.Errorf("artifact types = %v, want %v", got, want)
	}

	if !c.EnableAnalyzedContainerSBOM {
		t.Error("expected analyzed-container SBOM to be enabled from referenced artifact")
	}

	if got, want := c.BuildArgsString, "ALPHA=1\nZETA=last"; got != want {
		t.Errorf("build args string = %q, want %q", got, want)
	}
}

// TestDerive_ProjectsNativeArtifactsOntoContainers covers the Go and Cargo
// fields a container derives from its dependencies. publish-container.yml
// downloads a prebuilt binary only when the matching *-artifact-name is set,
// so a container-first dependency must leave its slot empty (the binary is
// compiled inside the Containerfile), and an artifact-first one must fill it
// regardless of where it sits in From. Each container is compared as a whole
// tuple, since the fields are derived in one walk and a mix-up between them
// would pass field-by-field checks on a single-container fixture.
//
// A second artifact-first dependency of one ecosystem is the validator's to
// refuse, so no row relies on which of two would win here.
func TestDerive_ProjectsNativeArtifactsOntoContainers(t *testing.T) {
	t.Parallel()

	type projection struct {
		types    []projecttype.Type
		goName   string
		cargo    string
		analyzed bool
	}

	cfg := &config.Config{
		Artifacts: []config.Artifact{
			{Name: "cli", ProjectType: projecttype.Go, SBOMs: "build"},
			{Name: "cli-in-image", ProjectType: projecttype.Go, SBOMs: "build", Go: &config.GoConfig{BuildMode: config.GoBuildModeContainerFirst}},
			{Name: "tool", ProjectType: projecttype.Cargo, SBOMs: "build", Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeArtifactFirst}},
			{Name: "tool-in-image", ProjectType: projecttype.Cargo, SBOMs: "analyzed-container", Cargo: &config.CargoConfig{BuildMode: config.CargoBuildModeContainerFirst}},
			{Name: "web", ProjectType: projecttype.NPM, SBOMs: "build"},
		},
		Containers: []config.Container{
			{Name: "artifact-first", From: []string{"tool", "cli"}},
			{Name: "cargo-only", From: []string{"tool"}},
			{Name: "container-first", From: []string{"cli-in-image", "tool-in-image"}},
			{Name: "mixed", From: []string{"web", "cli-in-image", "tool-in-image", "cli"}},
		},
	}

	if err := config.Derive(cfg); err != nil {
		t.Fatal(err)
	}

	want := map[string]projection{
		"artifact-first":  {types: []projecttype.Type{projecttype.Go, projecttype.Cargo}, goName: "cli", cargo: "tool"},
		"cargo-only":      {types: []projecttype.Type{projecttype.Cargo}, cargo: "tool"},
		"container-first": {types: []projecttype.Type{projecttype.Go, projecttype.Cargo}, analyzed: true},
		"mixed":           {types: []projecttype.Type{projecttype.NPM, projecttype.Go, projecttype.Cargo}, goName: "cli", analyzed: true},
	}

	for _, c := range cfg.Containers {
		got := projection{types: c.ArtifactTypes, goName: c.GoArtifactName, cargo: c.CargoArtifactName, analyzed: c.EnableAnalyzedContainerSBOM}
		w := want[c.Name]

		if !slices.Equal(got.types, w.types) || got.goName != w.goName || got.cargo != w.cargo || got.analyzed != w.analyzed {
			t.Errorf("container %s = %+v, want %+v", c.Name, got, w)
		}
	}
}

func TestDerive_InvalidSBOMNamesArtifact(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Artifacts: []config.Artifact{{Name: "bad", ProjectType: projecttype.NPM, SBOMs: "build,bogus"}}}

	err := config.Derive(cfg)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	// Both halves: which artifact, and what was wrong with it. Either alone
	// leaves the operator hunting through artifacts.yml.
	for _, want := range []string{`artifact "bad"`, "unknown token"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to mention %q", err, want)
		}
	}
}

func TestAnyRequireAuthorization_IsTrueWhenAnyArtifactRequiresIt(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		artifacts []config.Artifact
		want      bool
	}{
		{"later true", []config.Artifact{{RequireAuthorization: false}, {RequireAuthorization: true}}, true},
		{"first true", []config.Artifact{{RequireAuthorization: true}, {RequireAuthorization: false}}, true},
		{"all false", []config.Artifact{{RequireAuthorization: false}, {RequireAuthorization: false}}, false},
		{"empty", []config.Artifact{}, false},
		{"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := config.AnyRequireAuthorization(tc.artifacts); got != tc.want {
				t.Errorf("AnyRequireAuthorization = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDerive_BuildArgsFraming(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args map[string]string
		want string
	}{
		{"nil", nil, ""},
		{"empty", map[string]string{}, ""},
		{
			"sorted with equals and empty values",
			map[string]string{"ZETA": "last", "ALPHA": "one=two,three=four", "EMPTY": "", "WORDS": "two words"},
			"ALPHA=one=two,three=four\nEMPTY=\nWORDS=two words\nZETA=last",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Config{
				Artifacts:  []config.Artifact{{Name: "app", ProjectType: projecttype.Maven}},
				Containers: []config.Container{{Name: "image", From: []string{"app"}, BuildArgs: tc.args}},
			}
			require.NoError(t, config.Validate(cfg))
			require.NoError(t, config.Derive(cfg))
			// The CLI's --build-args consumes newline-separated KEY=VALUE lines,
			// not comma-separated values or shell words.
			require.Equal(t, tc.want, cfg.Containers[0].BuildArgsString)
		})
	}
}
