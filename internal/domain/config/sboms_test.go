// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestExpandSBOMs_ExpandsAllToEveryLayer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    []config.SBOMLayer
		wantErr string
	}{
		{name: "all expands to all three layers", in: "all", want: []config.SBOMLayer{
			config.SBOMLayerBuild, config.SBOMLayerAnalyzedArtifact, config.SBOMLayerAnalyzedContainer,
		}},
		{name: "none expands to empty", in: "none", want: []config.SBOMLayer{}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{name: "single layer build", in: "build", want: []config.SBOMLayer{config.SBOMLayerBuild}},
		{name: "single analyzed-container", in: "analyzed-container", want: []config.SBOMLayer{config.SBOMLayerAnalyzedContainer}},
		{name: "comma list", in: "build,analyzed-artifact", want: []config.SBOMLayer{
			config.SBOMLayerBuild, config.SBOMLayerAnalyzedArtifact,
		}},
		{name: "whitespace tolerated", in: "build, analyzed-artifact", want: []config.SBOMLayer{
			config.SBOMLayerBuild, config.SBOMLayerAnalyzedArtifact,
		}},
		{name: "whitespace only rejected", in: "   ", wantErr: "value required"},
		{name: "duplicates deduped", in: "build,build,analyzed-artifact", want: []config.SBOMLayer{
			config.SBOMLayerBuild, config.SBOMLayerAnalyzedArtifact,
		}},
		{name: "empty value rejected", in: "", wantErr: "value required"},
		{name: "leading comma rejected", in: ",build", wantErr: "empty token"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{name: "trailing comma rejected", in: "build,", wantErr: "empty token"},
		{name: "duplicate comma rejected", in: "build,,analyzed-artifact", wantErr: "empty token"},
		{name: "all with extra rejected", in: "all,build", wantErr: "shortcut and cannot be combined"},
		{name: "none with extra rejected", in: "build,none", wantErr: "shortcut and cannot be combined"},
		{name: "unknown token rejected", in: "build,bogus", wantErr: "unknown token"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.ExpandSBOMs(tc.in)
			if tc.wantErr != "" {
				// Every rejection here is a bad value in artifacts.yml, so
				// they all classify the same way: ErrValidation, exit 1.
				if !errors.Is(err, errs.ErrValidation) {
					t.Fatalf("err = %v, want ErrValidation", err)
				}

				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("err = %v, want substring %q", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPipelineSBOMs_UnionsLayersAcrossArtifacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arts []config.Artifact
		want string
	}{
		{name: "empty list", arts: nil, want: "none"},
		{name: "single artifact: all", want: "build,analyzed-artifact,analyzed-container", arts: []config.Artifact{
			{EffectiveSBOMs: []config.SBOMLayer{config.SBOMLayerBuild, config.SBOMLayerAnalyzedArtifact, config.SBOMLayerAnalyzedContainer}},
		}},
		{name: "two artifacts: union deduped, canonical order", want: "build,analyzed-artifact,analyzed-container", arts: []config.Artifact{
			{EffectiveSBOMs: []config.SBOMLayer{config.SBOMLayerAnalyzedContainer, config.SBOMLayerBuild}},
			{EffectiveSBOMs: []config.SBOMLayer{config.SBOMLayerAnalyzedArtifact}},
		}},
		{name: "all none → none", arts: []config.Artifact{
			{EffectiveSBOMs: []config.SBOMLayer{}},
			{EffectiveSBOMs: nil},
		}, want: "none"},
		{name: "single layer", arts: []config.Artifact{
			{EffectiveSBOMs: []config.SBOMLayer{config.SBOMLayerBuild}},
		}, want: "build"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := config.PipelineSBOMs(tc.arts); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestExpandSBOMs_AllReturnsAnIndependentSlice proves a caller cannot reach the
// package's canonical layer list through what it was handed.
//
// "all" is the only branch that could return the shared slice, and returning it
// would make ExpandSBOMs a way to rewrite the SBOM policy for the rest of the
// process: one caller appending to or reordering its result would change what
// every later caller — and the generated JSON schema, which derives its `sboms`
// pattern from the same list — sees. The copy is there; nothing checked it, so
// removing it was invisible.
func TestExpandSBOMs_AllReturnsAnIndependentSlice(t *testing.T) {
	t.Parallel()

	first, err := config.ExpandSBOMs("all")
	if err != nil {
		t.Fatal(err)
	}

	if len(first) == 0 {
		t.Fatal("all expanded to nothing; the assertions below would be vacuous")
	}

	want := slices.Clone(first)

	// Overwrite and reorder what the caller was given.
	for i := range first {
		first[i] = config.SBOMLayer("mutated")
	}

	slices.Reverse(first)

	second, err := config.ExpandSBOMs("all")
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(second, want) {
		t.Errorf("a second expansion returned %v, want %v: the first caller's writes reached the canonical list", second, want)
	}

	// Two expansions must also not alias each other, or the second caller
	// inherits whatever the first one does next.
	third, err := config.ExpandSBOMs("all")
	if err != nil {
		t.Fatal(err)
	}

	third[0] = config.SBOMLayer("mutated-again")

	if second[0] == config.SBOMLayer("mutated-again") {
		t.Error("two expansions share one backing array")
	}
}
