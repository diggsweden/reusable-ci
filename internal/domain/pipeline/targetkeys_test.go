// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline

import (
	"reflect"
	"strings"
	"testing"
)

// TestTargetKeys_MatchStructTags pins each Target* constant to the
// JSON tag on the matching stage-target struct field. If anyone
// renames a tag or a constant without updating the other, this test
// fails — preventing the silent producer/consumer drift the SSOT rule
// exists to prevent.
//
// The pairing is intentionally explicit (one entry per constant /
// field) rather than reflection-derived from the constant value, so
// "the tag must match the constant" is visible in the test source.
func TestTargetKeys_MatchStructTags(t *testing.T) {
	t.Parallel()

	cases := []struct {
		want   string
		target any
		field  string
	}{
		// Release build-stage targets.
		{TargetMaven, ReleaseBuildTargets{}, "Maven"},
		{TargetNPM, ReleaseBuildTargets{}, "NPM"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{TargetGradle, ReleaseBuildTargets{}, "Gradle"},
		{TargetGradleAndroid, ReleaseBuildTargets{}, "GradleAndroid"},
		{TargetXcodeIOS, ReleaseBuildTargets{}, "XcodeIOS"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{TargetGo, ReleaseBuildTargets{}, "Go"},
		{TargetCargo, ReleaseBuildTargets{}, "Cargo"},

		// Dev build-stage targets (same names, separate type).
		{TargetMaven, DevBuildTargets{}, "Maven"},
		{TargetNPM, DevBuildTargets{}, "NPM"},
		{TargetGradle, DevBuildTargets{}, "Gradle"},
		{TargetGradleAndroid, DevBuildTargets{}, "GradleAndroid"},
		{TargetXcodeIOS, DevBuildTargets{}, "XcodeIOS"},
		{TargetGo, DevBuildTargets{}, "Go"},
		{TargetCargo, DevBuildTargets{}, "Cargo"},

		// Release prepare-stage targets.
		{TargetVersionBump, ReleasePrepareTargets{}, "VersionBump"},

		// Release publish-stage targets.
		{TargetForgePackages, ReleasePublishTargets{}, "ForgePackages"},
		{TargetMavenCentral, ReleasePublishTargets{}, "MavenCentral"},
		{TargetGooglePlay, ReleasePublishTargets{}, "GooglePlay"},
		{TargetXcodeIOS, ReleasePublishTargets{}, "XcodeIOS"},
		{TargetContainers, ReleasePublishTargets{}, "Containers"},
		{TargetCargoContainerFirst, ReleasePublishTargets{}, "CargoContainerFirst"},
		{TargetGoContainerFirst, ReleasePublishTargets{}, "GoContainerFirst"},

		// Dev publish-stage targets.
		{TargetNPM, DevPublishTargets{}, "NPM"},
		{TargetCargoContainerFirst, DevPublishTargets{}, "CargoContainerFirst"},
		{TargetGoContainerFirst, DevPublishTargets{}, "GoContainerFirst"},
		{TargetGoArtifactFirst, DevPublishTargets{}, "GoArtifactFirst"},
		{TargetCargoArtifactFirst, DevPublishTargets{}, "CargoArtifactFirst"},
		{TargetSBOM, DevPublishTargets{}, "SBOM"},

		// PR quality-stage targets.
		{TargetNanolinter, PRQualityTargets{}, "Nanolinter"},
		{TargetSwift, PRQualityTargets{}, "Swift"},
	}

	for _, tc := range cases {
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

// jsonTagOf returns the json tag name of a named struct field, after
// stripping any ",omitempty"-style options. Fails the test if the
// field doesn't exist on the target.
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
