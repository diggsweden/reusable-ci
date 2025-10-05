// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
)

// targetKeyCase pairs a Target* constant with the struct field whose JSON tag
// must equal it.
type targetKeyCase struct {
	want   string
	target any
	field  string
}

// targetKeyCases is the pairing, one row per stage-target field. It is
// deliberately explicit rather than reflection-derived, so "the tag must match
// the constant" is visible in the test source -- and TestTargetKeys_EveryTargetFieldIsPinned
// below makes sure the list cannot fall behind the structs.
func targetKeyCases() []targetKeyCase {
	return []targetKeyCase{
		// Release build-stage targets.
		{pipeline.TargetMaven, pipeline.ReleaseBuildTargets{}, "Maven"},
		{pipeline.TargetNPM, pipeline.ReleaseBuildTargets{}, "NPM"},
		{pipeline.TargetGradle, pipeline.ReleaseBuildTargets{}, "Gradle"},
		{pipeline.TargetGradleAndroid, pipeline.ReleaseBuildTargets{}, "GradleAndroid"},
		{pipeline.TargetXcodeIOS, pipeline.ReleaseBuildTargets{}, "XcodeIOS"},
		{pipeline.TargetGo, pipeline.ReleaseBuildTargets{}, "Go"},
		{pipeline.TargetCargo, pipeline.ReleaseBuildTargets{}, "Cargo"},

		// Dev build-stage targets (same names, separate type).
		{pipeline.TargetMaven, pipeline.DevBuildTargets{}, "Maven"},
		{pipeline.TargetNPM, pipeline.DevBuildTargets{}, "NPM"},
		{pipeline.TargetGradle, pipeline.DevBuildTargets{}, "Gradle"},
		{pipeline.TargetGradleAndroid, pipeline.DevBuildTargets{}, "GradleAndroid"},
		{pipeline.TargetXcodeIOS, pipeline.DevBuildTargets{}, "XcodeIOS"},
		{pipeline.TargetGo, pipeline.DevBuildTargets{}, "Go"},
		{pipeline.TargetCargo, pipeline.DevBuildTargets{}, "Cargo"},

		// Release prepare-stage targets.
		{pipeline.TargetVersionBump, pipeline.ReleasePrepareTargets{}, "VersionBump"},

		// Release publish-stage targets.
		{pipeline.TargetForgePackages, pipeline.ReleasePublishTargets{}, "ForgePackages"},
		{pipeline.TargetMavenCentral, pipeline.ReleasePublishTargets{}, "MavenCentral"},
		{pipeline.TargetGooglePlay, pipeline.ReleasePublishTargets{}, "GooglePlay"},
		{pipeline.TargetXcodeIOS, pipeline.ReleasePublishTargets{}, "XcodeIOS"},
		{pipeline.TargetContainers, pipeline.ReleasePublishTargets{}, "Containers"},
		{pipeline.TargetCargoContainerFirst, pipeline.ReleasePublishTargets{}, "CargoContainerFirst"},
		{pipeline.TargetGoContainerFirst, pipeline.ReleasePublishTargets{}, "GoContainerFirst"},

		// Dev publish-stage targets.
		{pipeline.TargetNPM, pipeline.DevPublishTargets{}, "NPM"},
		{pipeline.TargetCargoContainerFirst, pipeline.DevPublishTargets{}, "CargoContainerFirst"},
		{pipeline.TargetGoContainerFirst, pipeline.DevPublishTargets{}, "GoContainerFirst"},
		{pipeline.TargetGoArtifactFirst, pipeline.DevPublishTargets{}, "GoArtifactFirst"},
		{pipeline.TargetCargoArtifactFirst, pipeline.DevPublishTargets{}, "CargoArtifactFirst"},
		{pipeline.TargetSBOM, pipeline.DevPublishTargets{}, "SBOM"},

		// PR quality-stage targets.
		{pipeline.TargetNanolinter, pipeline.PRQualityTargets{}, "Nanolinter"},
		{pipeline.TargetMegalinter, pipeline.PRQualityTargets{}, "Megalinter"},
		{pipeline.TargetSwift, pipeline.PRQualityTargets{}, "Swift"},
	}
}

// TestTargetKeys_MatchStructTags pins each Target* constant to the JSON tag on
// the matching stage-target struct field. If anyone renames a tag or a constant
// without updating the other, this test fails -- preventing the silent
// producer/consumer drift the SSOT rule exists to prevent.
func TestTargetKeys_MatchStructTags(t *testing.T) {
	t.Parallel()

	for _, tc := range targetKeyCases() {
		t.Run(tc.field+"/"+tc.want, func(t *testing.T) {
			t.Parallel()

			got := jsonTagOf(t, tc.target, tc.field)
			if got != tc.want {
				t.Errorf("%T.%s json tag = %q, constant = %q (drift between SSOT constant and struct tag)",
					tc.target, tc.field, got, tc.want)
			}
		})
	}
}

// TestTargetKeys_EveryTargetFieldIsPinned is the tripwire for the table above.
//
// The pairing only proves the rows it contains. A new stage target -- a
// constant plus a struct field -- added without a row would leave the pairing
// green while the very drift it guards against went unwatched. That is not
// hypothetical: TargetMegalinter and PRQualityTargets.Megalinter existed with
// no row until this test was written.
//
// Every field of these structs is a TargetPlan, so the rule is simply that
// each one appears above.
func TestTargetKeys_EveryTargetFieldIsPinned(t *testing.T) {
	t.Parallel()

	pinned := map[string]bool{}
	for _, tc := range targetKeyCases() {
		pinned[reflect.TypeOf(tc.target).Name()+"."+tc.field] = true
	}

	for _, target := range []any{
		pipeline.ReleaseBuildTargets{},
		pipeline.DevBuildTargets{},
		pipeline.ReleasePrepareTargets{},
		pipeline.ReleasePublishTargets{},
		pipeline.DevPublishTargets{},
		pipeline.PRQualityTargets{},
	} {
		rt := reflect.TypeOf(target)
		for i := range rt.NumField() {
			key := rt.Name() + "." + rt.Field(i).Name
			if !pinned[key] {
				t.Errorf("%s has no row in targetKeyCases: its json tag is unguarded, "+
					"so it can drift from its Target* constant unnoticed", key)
			}
		}
	}
}

// jsonTagOf returns the json tag name of a named struct field, after stripping
// any ",omitempty"-style options. Fails the test if the field doesn't exist.
func jsonTagOf(t *testing.T, target any, field string) string {
	t.Helper()

	rt := reflect.TypeOf(target)

	f, ok := rt.FieldByName(field)
	if !ok {
		t.Fatalf("%T has no field %q", target, field)
	}

	tag := f.Tag.Get("json")
	if comma := strings.IndexByte(tag, ','); comma >= 0 {
		tag = tag[:comma]
	}

	return tag
}
