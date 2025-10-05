// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package pipeline_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/stretchr/testify/require"
)

func TestDecodeConfigPlan_DuplicateConsumedMembers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ prefix, first, second, suffix, path string }{
		{`{`, `"version":1`, `"VERSION":2`, `}`, "version"},
		{`{`, `"version":1`, `"ver\u0073ion":1`, `}`, "version"},
		{`{`, `"artifacts":{}`, `"ARTIFACTS":null`, `}`, "artifacts"},
		{`{`, `"containers":{}`, `"containers":null`, `}`, "containers"},
		{`{`, `"any_require_authorization":false`, `"ANY_REQUIRE_AUTHORIZATION":true`, `}`, "any_require_authorization"},
		{`{`, `"pipeline_sboms":"none"`, `"pipeline_sboms":"all"`, `}`, "pipeline_sboms"},
		{`{`, `"fallback_project_type":"maven"`, `"fallback_project_type":"cargo"`, `}`, "fallback_project_type"},
		{`{`, `"sign":{}`, `"sign":{"method":"gpg"}`, `}`, "sign"},
		{`{`, `"git_signing":{}`, `"git_signing":{"method":"ssh"}`, `}`, "git_signing"},
		{`{"artifacts":{"all":[{}, {`, `"require_authorization":false`, `"require_authorization":true`, `}]}}`, "artifacts.all[1].require_authorization"},
		{`{"artifacts":{"all":[{`, `"name":"first"`, `"NAME":"later"`, `}]}}`, "artifacts.all[0].name"},
		{`{"artifacts":{"all":[{`, `"effective_sboms":[]`, `"effective_sboms":["build"]`, `}]}}`, "artifacts.all[0].effective_sboms"},
		{`{"artifacts":{"maven_central":[{`, `"working_directory":"one"`, `"working_directory":"two"`, `}]}}`, "artifacts.maven_central[0].working_directory"},
		{`{"artifacts":{"maven":[{"maven":{`, `"settings_path":"one"`, `"\u0073ETTINGS_PATH":"two"`, `}}]}}`, "artifacts.maven[0].maven.settings_path"},
		{`{"artifacts":{"npmjs":[{"npm":{`, `"node_version":"22"`, `"node_version":"24"`, `}}]}}`, "artifacts.npmjs[0].npm.node_version"},
		{`{"artifacts":{"gradle":[{"gradle":{`, `"gradle_tasks":"build"`, `"gradle_tasks":"publish"`, `}}]}}`, "artifacts.gradle[0].gradle.gradle_tasks"},
		{`{"artifacts":{"google_play":[{"gradle_android":{`, `"include_aab":null`, `"include_aab":false`, `}}]}}`, "artifacts.google_play[0].gradle_android.include_aab"},
		{`{"artifacts":{"xcode_ios":[{"xcode_ios":{`, `"enable_code_signing":false`, `"enable_code_signing":true`, `}}]}}`, "artifacts.xcode_ios[0].xcode_ios.enable_code_signing"},
		{`{"artifacts":{"go_container_first":[{"go":{`, `"build_mode":"container-first"`, `"build_mode":"artifact-first"`, `}}]}}`, "artifacts.go_container_first[0].go.build_mode"},
		{`{"artifacts":{"cargo_artifact_first":[{"cargo":{`, `"skip_tests":false`, `"\u017fKIP_TESTS":true`, `}}]}}`, "artifacts.cargo_artifact_first[0].cargo.skip_tests"},
		{`{"containers":{`, `"has_containers":false`, `"has_containers":true`, `}}`, "containers.has_containers"},
		{`{"containers":{`, `"all":[]`, `"ALL":[{}]`, `}}`, "containers.all"},
		{`{"containers":{"all":[{}, {`, `"enable_slsa":false`, `"enable_slsa":true`, `}]}}`, "containers.all[1].enable_slsa"},
		{`{"containers":{"all":[{`, `"from":[]`, `"from":["app"]`, `}]}}`, "containers.all[0].from"},
		{`{"containers":{"all":[{"build_args":{`, `"MODE":"one"`, `"M\u004fDE":"two"`, `}}]}}`, `containers.all[0].build_args["MODE"]`},
		{`{"sign":{`, `"method":"gpg"`, `"method":"sigstore"`, `}}`, "sign.method"},
		{`{"sign":{`, `"requires_id_token":false`, `"requires_id_token":true`, `}}`, "sign.requires_id_token"},
		{`{"sign":{`, `"key":"one"`, `"\u212aEY":"two"`, `}}`, "sign.key"},
		{`{"git_signing":{`, `"imports_gpg_key":false`, `"imports_gpg_key":true`, `}}`, "git_signing.imports_gpg_key"},
	} {
		for _, pair := range []struct{ name, first, second string }{
			{"equal", tc.first, tc.first}, {"forward", tc.first, tc.second}, {"reverse", tc.second, tc.first},
		} {
			t.Run(tc.path+"/"+pair.name, func(t *testing.T) {
				raw := tc.prefix + pair.first + "," + pair.second + tc.suffix
				require.True(t, json.Valid([]byte(raw)), raw)
				plan, err := pipeline.DecodeConfigPlan(raw)
				require.ErrorIs(t, err, errs.ErrInvalidConfig)
				require.EqualError(t, err, "parse config-plan-json: config-plan."+tc.path+" has duplicate consumed member: "+errs.ErrInvalidConfig.Error())
				require.Equal(t, pipeline.ConfigPlan{}, plan)
			})
		}
	}
}

func TestDecodeConfigPlan_EveryProjection(t *testing.T) {
	t.Parallel()

	for _, group := range []string{"all", "maven", "npm", "gradle", "gradle_android", "xcode_ios", "python", "go", "cargo", "meta", "go_artifact_first", "go_container_first", "cargo_artifact_first", "cargo_container_first", "forge_packages", "maven_central", "google_play", "npmjs"} {
		t.Run(group, func(t *testing.T) {
			for _, pair := range []string{`[] , "` + strings.ToUpper(group) + `":[]`, `null , "` + group + `":[]`, `[] , "` + group + `":null`} {
				_, err := pipeline.DecodeConfigPlan(`{"artifacts":{"` + group + `":` + pair + `}}`)
				require.ErrorContains(t, err, "config-plan.artifacts."+group+" has duplicate consumed member")
			}
		})
	}
}

func TestDecodeConfigPlan_WireCompatibility(t *testing.T) {
	t.Parallel()

	const empty = `"pipeline_sboms":"none","sign":{"method":"gpg","imports_gpg_key":true},"git_signing":{"method":"gpg","imports_gpg_key":true}`
	for _, raw := range []string{
		`{"version":1,` + empty + `}`,
		`{"VERSION":1,"artifacts":{"all":null,"cargo":[]},"containers":null,` + empty + `}`,
		`{"ver\u0073ion":1,"extension":{"version":1,"version":2},"extension":false,` + empty + `}`,
		`{"version":1,"artifacts":{"future":[{"name":"a","name":"b"}]},"containers":{"future":1,"future":2},` + empty + `}`,
	} {
		plan, err := pipeline.DecodeConfigPlan(raw)
		require.NoError(t, err)
		require.NoError(t, pipeline.ValidateConfigPlan(plan))
		require.Equal(t, 1, plan.Version)
	}
	// Identical names in distinct objects are not duplicate members. Unknown
	// nested fields, including empty Python config, remain additive extensions.
	raw := `{"artifacts":{"all":[{"name":"a","maven":{"future":1,"future":2}},{"name":"b","python":{"name":1,"name":2}}]},"containers":{"all":[{"build_args":{"MODE":"upper","mode":"lower"}}]}}`
	plan, err := pipeline.DecodeConfigPlan(raw)
	require.NoError(t, err)
	require.Equal(t, "a", plan.Artifacts.All[0].Name)
	require.Equal(t, "b", plan.Artifacts.All[1].Name)
	require.Equal(t, map[string]string{"MODE": "upper", "mode": "lower"}, plan.Containers.All[0].BuildArgs)
}

func TestDecodeConfigPlan_MalformedAndTypedErrors(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "{", `{} {}`, `{"version":1} true`, `[]`, `true`, `{"version":"1"}`, `{"artifacts":{"all":{}}}`, `{"sign":[]}`, `{"containers":{"all":[{"build_args":[]}]}}`} {
		plan, err := pipeline.DecodeConfigPlan(raw)
		require.ErrorIs(t, err, errs.ErrInvalidConfig, raw)
		require.ErrorContains(t, err, "parse config-plan-json:")
		require.Equal(t, pipeline.ConfigPlan{}, plan)
	}
	// Null/version policy remains the validator's responsibility.
	for _, raw := range []string{`null`, `{}`, `{"version":999}`} {
		plan, err := pipeline.DecodeConfigPlan(raw)
		require.NoError(t, err)
		require.ErrorIs(t, pipeline.ValidateConfigPlan(plan), errs.ErrInvalidConfig)
	}
}
