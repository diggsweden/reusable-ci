// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/config"
)

func TestExpandSBOMs(t *testing.T) {
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
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("err = %v, want substring %q", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPipelineSBOMs(t *testing.T) {
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
