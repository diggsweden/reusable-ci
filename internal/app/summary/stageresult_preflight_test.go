// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainsummary "github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStageResult_RetainedNamesRefuseBeforeSources(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ field, name string }{
		{"stage", "build\n"}, {"stage", "build "}, {"stage", " build"},
		{"stage", "build\t"}, {"stage", "build\u00a0"},
		{"stage", "Build"}, {"stage", "dev_build"}, {"stage", "-build"},
		{"stage", "build-"}, {"stage", "build/linux"},
		{"target", " npm "}, {"target", "npm "}, {"target", "npm\n"},
		{"target", "npm\u00a0"}, {"target", "Npm"}, {"target", "npm/linux"},
	} {
		for _, runs := range []bool{true, false} {
			for _, source := range []string{"explicit", "map", "store", "poisoned-store"} {
				t.Run(fmt.Sprintf("%s/%q/runs=%t/%s", tc.field, tc.name, runs, source), func(t *testing.T) {
					t.Parallel()

					stage, target := "build", "npm"

					wantText := fmt.Sprintf("stage plan target %q has invalid name", tc.name)
					if tc.field == "stage" {
						stage = tc.name
						wantText = fmt.Sprintf("stage plan has invalid stage name %q", tc.name)
					} else {
						target = tc.name
					}

					stageJSON, err := json.Marshal(stage)
					require.NoError(t, err)
					targetJSON, err := json.Marshal(target)
					require.NoError(t, err)

					in := appsummary.StageResultInput{
						StagePlanJSON: fmt.Sprintf(`{"version":1,"stage":%s,"targets":{"cargo":{"runs":true},%s:{"runs":%t}}}`, stageJSON, targetJSON, runs),
					}
					ports := newResultFailurePorts(t, 0)
					ports.jobs.Seed("cargo", `{"version":1,"job":"cargo","result":"success"}`).
						Seed("npm", `{"version":1,"job":"npm","result":"success"}`)

					switch source {
					case "explicit":
						in.Results = []domainsummary.KeyValue{{Key: "cargo", Value: "success"}, {Key: "npm", Value: "success"}}
					case "map":
						in.JobResultsMap = `{"cargo":{"result":"success"},"npm":{"result":"success"}}`
					case "poisoned-store":
						ports.failAt = 1
					}

					env, err := appsummary.StageResult(t.Context(), ports, ports, ports, in)
					assertStagePairPreflightRefusal(t, env, err, errs.ErrInvalidConfig, wantText, ports)
				})
			}
		}
	}
}

func TestStageResult_PairPreflightRejectsBeforeCollectionOrMapParsing(t *testing.T) {
	t.Parallel()

	for _, label := range []string{"result", "extra", "json-field"} {
		cases := []struct{ key, want string }{
			{"", label + ` key "" has invalid name`},
			{"bad\nkey", label + ` key "bad\nkey" has invalid name`},
			{"bad/key", label + ` key "bad/key" has invalid name`},
			{"bad\"key", label + ` key "bad\"key" has invalid name`},
			{"npm", fmt.Sprintf(`duplicate %s key "npm"`, label)},
			{" \tnpm\r\n ", fmt.Sprintf(`duplicate %s key "npm"`, label)},
		}
		if label == "extra" {
			for _, reserved := range []string{"version", "stage", "result", "ran", "targets"} {
				cases = append(cases, struct{ key, want string }{
					" \t" + reserved + "\n ", fmt.Sprintf("extra key %q is reserved", reserved),
				})
			}
		}

		for _, tc := range cases {
			for _, runs := range []bool{true, false} {
				for _, source := range []string{"poisoned-store", "malformed-map"} {
					t.Run(fmt.Sprintf("%s/%q/runs=%t/%s", label, tc.key, runs, source), func(t *testing.T) {
						t.Parallel()
						ports := newResultFailurePorts(t, 1)
						in := appsummary.StageResultInput{
							StagePlanJSON: fmt.Sprintf(`{"version":1,"stage":"build","targets":{"cargo":{"runs":true},"npm":{"runs":%t}}}`, runs),
							Extras:        []domainsummary.KeyValue{{Key: "project_type", Value: "npm"}},
							JSONOutputKey: "artifacts-json",
							JSONFields:    []domainsummary.KeyValue{{Key: "package_name", Value: "fixture"}},
						}
						pairs := []domainsummary.KeyValue{
							{Key: "cargo", Value: "success"}, {Key: "npm", Value: "success"}, {Key: tc.key, Value: "success"},
						}

						switch label {
						case "result":
							in.Results = pairs
						case "extra":
							in.Extras = pairs
						case "json-field":
							in.JSONFields = pairs
						}

						if source == "malformed-map" {
							in.JobResultsMap = "not-json"
						}

						before := in
						before.Results, before.Extras, before.JSONFields = slices.Clone(in.Results), slices.Clone(in.Extras), slices.Clone(in.JSONFields)
						env, err := appsummary.StageResult(t.Context(), ports, ports, ports, in)
						assert.Equal(t, before, in, "refusal changed caller-owned pairs")
						assertStagePairPreflightRefusal(t, env, err, errs.ErrUsage, tc.want, ports)
					})
				}
			}
		}
	}
}

//nolint:testifylint // Exact JSON bytes protect extra order and escaping, not just semantic equality.
func TestStageResult_PaddedPairsPublishNormalizedCopies(t *testing.T) {
	t.Parallel()

	for _, outputKey := range []string{"artifacts-json", "artifacts_json"} {
		t.Run(outputKey, func(t *testing.T) {
			t.Parallel()
			ports := newResultFailurePorts(t, 0)
			in := appsummary.StageResultInput{
				StagePlanJSON: `{"version":1,"stage":"build","targets":{"npm":{"runs":true}}}`,
				Results:       []domainsummary.KeyValue{{Key: " \tnpm\r\n ", Value: "success"}},
				JobResultsMap: "unused malformed map",
				Extras: []domainsummary.KeyValue{
					{Key: "project_type\n", Value: " npm=\"quoted\"\n "},
					{Key: " package_name ", Value: " underscore=\"two\"\n "},
					{Key: "\tpackage-name\r\n", Value: " hyphen=\"one\"\n "},
				},
				JSONOutputKey: outputKey,
				JSONFields: []domainsummary.KeyValue{
					{Key: " package_name\n", Value: " underscore=\"two\"\n "},
					{Key: "\tpackage-name ", Value: " hyphen=\"one\"\n "},
					{Key: " version\n", Value: " field version "},
				},
			}
			before := in
			before.Results, before.Extras, before.JSONFields = slices.Clone(in.Results), slices.Clone(in.Extras), slices.Clone(in.JSONFields)
			env, err := appsummary.StageResult(t.Context(), retainedPairOutput{ports}, ports, ports, in)
			require.NoError(t, err)
			assert.Equal(t, before, in, "normalization mutated caller-owned slices")
			require.NotNil(t, env)
			assert.Equal(t, &domainsummary.StageResultEnvelope{
				Version: 0, Stage: "build", Result: domainsummary.ResultSuccess, Ran: true,
				Extras: []domainsummary.KeyValue{
					{Key: "project_type", Value: " npm=\"quoted\"\n "},
					{Key: "package_name", Value: " underscore=\"two\"\n "},
					{Key: "package-name", Value: " hyphen=\"one\"\n "},
				},
				Targets: []domainsummary.Target{{Name: "npm", Result: domainsummary.ResultSuccess}},
			}, env)

			const (
				wantJSON     = `{"version":1,"stage":"build","result":"success","ran":true,"project_type":" npm=\"quoted\"\n ","package_name":" underscore=\"two\"\n ","package-name":" hyphen=\"one\"\n ","targets":{"npm":"success"}}`
				optionalJSON = `{"package-name":" hyphen=\"one\"\n ","package_name":" underscore=\"two\"\n ","version":" field version "}`
			)

			body, err := env.MarshalJSON()
			require.NoError(t, err)
			assert.True(t, json.Valid(body), "envelope must be valid JSON: %q", body)
			assert.Equal(t, wantJSON, string(body))
			assert.Equal(t, []resultSinkEvent{
				{"WriteJSON", "build", wantJSON},
				{"SetBool", "stage-ran", "true"},
				{"Set", "stage-result", "success"},
				{"Set", "result-json", wantJSON},
				{"Set", outputKey, optionalJSON},
			}, ports.events)
			assert.Equal(t, []string{"build"}, ports.manifest.Stages())
			assert.Equal(t, wantJSON, ports.manifest.Body("build"))
			require.Len(t, ports.events, 5)
			assert.True(t, json.Valid([]byte(ports.events[4].body)))

			in.Extras[0].Key, in.Extras[0].Value = "changed", "changed"
			body, err = env.MarshalJSON()
			require.NoError(t, err)
			assert.Equal(t, wantJSON, string(body), "returned extras still alias caller storage")
		})
	}
}

//nolint:testifylint // Exact JSON bytes protect target order and wire spelling.
func TestStageResult_RetainedGenericNamesAndTargetAliases(t *testing.T) {
	t.Parallel()

	for _, stage := range []string{"build", "dev-build", "prepare", "publish", "dev-publish", "pr-quality", "build2", "build--linux"} {
		for _, source := range []string{"explicit", "map", "store"} {
			t.Run(stage+"/"+source, func(t *testing.T) {
				t.Parallel()
				ports := newResultFailurePorts(t, 0)
				in := appsummary.StageResultInput{
					StagePlanJSON: fmt.Sprintf(`{"version":1,"stage":%q,"targets":{"npm2":{"runs":true},"gradle_android":{"runs":true},"gradle-android":{"runs":true}}}`, stage),
				}

				var wantEvents []resultSinkEvent

				switch source {
				case "explicit":
					in.Results = []domainsummary.KeyValue{
						{Key: " gradle_android\n", Value: "success"}, {Key: "gradle-android", Value: "success"}, {Key: "npm2", Value: "cancelled"},
					}
				case "map":
					in.JobResultsMap = `{"gradle_android":{"result":"success"},"npm2":{"result":"cancelled"}}`
				case "store":
					ports.jobs.Seed("gradle-android", `{"version":1,"job":"gradle-android","result":"success"}`).
						Seed("npm2", `{"version":1,"job":"npm2","result":"cancelled"}`)

					wantEvents = append(wantEvents, resultSinkEvent{"CollectJobs", "", ""})
				}

				env, err := appsummary.StageResult(t.Context(), ports, ports, ports, in)
				require.NoError(t, err)
				require.NotNil(t, env)
				assert.Zero(t, env.Version, "Go zero-version default must stay independent of wire version 1")
				assert.Equal(t, stage, env.Stage)
				assert.Equal(t, domainsummary.ResultCancelled, env.Result)
				assert.True(t, env.Ran)
				assert.Equal(t, []domainsummary.Target{
					{Name: "gradle-android", Result: domainsummary.ResultSuccess},
					{Name: "gradle_android", Result: domainsummary.ResultSuccess},
					{Name: "npm2", Result: domainsummary.ResultCancelled},
				}, env.Targets)

				wantJSON := fmt.Sprintf(`{"version":1,"stage":%q,"result":"cancelled","ran":true,"targets":{"gradle-android":"success","gradle_android":"success","npm2":"cancelled"}}`, stage)
				body, err := env.MarshalJSON()
				require.NoError(t, err)
				assert.Equal(t, wantJSON, string(body))
				wantEvents = append(wantEvents,
					resultSinkEvent{"WriteJSON", stage, wantJSON},
					resultSinkEvent{"SetBool", "stage-ran", "true"},
					resultSinkEvent{"Set", "stage-result", "cancelled"},
					resultSinkEvent{"Set", "result-json", wantJSON},
				)
				assert.Equal(t, wantEvents, ports.events)
				assert.Equal(t, []string{stage}, ports.manifest.Stages())
				assert.Equal(t, wantJSON, ports.manifest.Body(stage))
				assert.Equal(t, map[string]string{"stage-ran": "true", "stage-result": "cancelled", "result-json": wantJSON}, ports.out.AllScalar())
			})
		}
	}
}

//nolint:testifylint // The inactive control must preserve the complete literal wire document.
func TestStageResult_InactiveJSONFieldsIgnoreInvalidAndDuplicateKeys(t *testing.T) {
	t.Parallel()
	ports := newResultFailurePorts(t, 0)
	ports.jobs.Seed("npm", `{"version":1,"job":"npm","result":"success"}`)

	in := appsummary.StageResultInput{
		StagePlanJSON: `{"version":1,"stage":"build","targets":{"npm":{"runs":true}}}`,
		JSONFields: []domainsummary.KeyValue{
			{Key: "package", Value: " first "}, {Key: " package\n", Value: " second "}, {Key: "bad/key", Value: " ignored "},
		},
	}
	before := slices.Clone(in.JSONFields)
	env, err := appsummary.StageResult(t.Context(), ports, ports, ports, in)
	require.NoError(t, err)
	require.NotNil(t, env)
	assert.Equal(t, before, in.JSONFields)

	const wantJSON = `{"version":1,"stage":"build","result":"success","ran":true,"targets":{"npm":"success"}}`
	assert.Equal(t, []resultSinkEvent{
		{"CollectJobs", "", ""},
		{"WriteJSON", "build", wantJSON},
		{"SetBool", "stage-ran", "true"},
		{"Set", "stage-result", "success"},
		{"Set", "result-json", wantJSON},
	}, ports.events)
	assert.Equal(t, []string{"build"}, ports.manifest.Stages())
	assert.Equal(t, wantJSON, ports.manifest.Body("build"))
	assert.Equal(t, map[string]string{"stage-ran": "true", "stage-result": "success", "result-json": wantJSON}, ports.out.AllScalar())
}

func assertStagePairPreflightRefusal(t *testing.T, env *domainsummary.StageResultEnvelope, err, wantErr error, wantText string, ports *resultFailurePorts) {
	t.Helper()
	assert.Nil(t, env)
	assert.Equal(t, []resultSinkEvent(nil), ports.events, "want exact zero-attempt vector across every port method")
	assert.Empty(t, ports.manifest.Stages())
	assert.Empty(t, ports.out.AllScalar())
	assert.Empty(t, ports.out.Keys())
	assert.Zero(t, ports.out.CloseCount())
	require.ErrorIs(t, err, wantErr)
	require.ErrorContains(t, err, wantText)
	require.NotErrorIs(t, err, ports.sentinel, "collection poison masked preflight")
	require.NotErrorIs(t, err, errs.ErrMalformedInput)
	require.NotErrorIs(t, err, errs.ErrValidation)

	if errors.Is(wantErr, errs.ErrUsage) {
		require.NotErrorIs(t, err, errs.ErrInvalidConfig)
	} else {
		require.NotErrorIs(t, err, errs.ErrUsage)
	}
}

// Record raw scalar bytes without letting the fake sink's newline guard mask
// malformed envelope JSON or prevent the optional JSON output from being reached.
type retainedPairOutput struct{ *resultFailurePorts }

func (p retainedPairOutput) Set(ctx context.Context, key, value string) error {
	return p.record(ctx, "Set", key, value)
}
