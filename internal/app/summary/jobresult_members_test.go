// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"bytes"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainsummary "github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
	"github.com/stretchr/testify/require"
)

func TestStageResult_DuplicateJobMembersRefuseBeforePublication(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		jobMap string
		stored string
		field  string
	}{
		{
			name:   "map_result_failure_then_success",
			jobMap: `{"cargo":{"result":"success"},"npm":{"result":"failure","result":"success"}}`,
			stored: `{"version":1,"job":"npm","result":"success"}`, field: "result",
		},
		{
			name:   "stored_result_failure_then_success",
			stored: `{"version":1,"job":"npm","result":"failure","result":"success"}`, field: "result",
		},
		{
			name:   "stored_version_two_then_one",
			stored: `{"version":2,"version":1,"job":"npm","result":"success"}`, field: "version",
		},
		{
			name:   "stored_job_rename_hides_failure",
			stored: `{"version":1,"job":"npm","job":"unrelated","result":"failure"}`, field: "job",
		},
		{
			name:   "map_unicode_result",
			jobMap: `{"cargo":{"result":"success"},"npm":{"re\u017fult":"failure","result":"success"}}`,
			stored: `{"version":1,"job":"npm","result":"success"}`, field: "result",
		},
		{
			name:   "map_identical_result",
			jobMap: `{"cargo":{"result":"success"},"npm":{"result":"success","result":"success"}}`,
			stored: `{"version":1,"job":"npm","result":"success"}`, field: "result",
		},
		{
			name:   "map_null_then_valid",
			jobMap: `{"cargo":{"result":"success"},"npm":{"result":null,"result":"success"}}`,
			stored: `{"version":1,"job":"npm","result":"success"}`, field: "result",
		},
		{
			name:   "map_private_result",
			jobMap: `{"cargo":{"result":"success"},"npm":{"result":"p405-private-value","result":"success"}}`,
			stored: `{"version":1,"job":"npm","result":"success"}`, field: "result",
		},
		{
			name:   "stored_unicode_version",
			stored: `{"ver\u017fion":2,"version":1,"job":"npm","result":"success"}`, field: "version",
		},
		{
			name:   "stored_escaped_result",
			stored: `{"version":1,"job":"npm","result":"failure","re\u0073ult":"success"}`, field: "result",
		},
		{
			name:   "stored_case_job_private_value",
			stored: `{"version":1,"JOB":"p405-private-value","job":"npm","result":"success"}`, field: "job",
		},
		{
			name:   "stored_null_then_valid",
			stored: `{"version":1,"job":"npm","result":null,"result":"success"}`, field: "result",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ports := newResultFailurePorts(t, 0)

			const (
				cargo    = `{"version":1,"job":"cargo","result":"success"}`
				laterNPM = `{"version":1,"job":"npm","result":"success"}`
			)

			ports.jobs.Seed("cargo", cargo)
			ports.jobs.Seed("npm", tc.stored)
			ports.jobs.Seed("z-npm", laterNPM)
			require.NoError(t, ports.out.Set(t.Context(), "stage-result", "seeded-result"))
			require.NoError(t, ports.out.SetMultiline(t.Context(), "retained-lines", []string{"seeded", "lines"}))
			require.NoError(t, ports.manifest.Write(t.Context(), "build", map[string]any{"seeded": true}))
			beforeManifest := ports.manifest.Body("build")
			beforeScalars := ports.out.AllScalar()
			beforeKeys := ports.out.Keys()
			beforeJobs, err := ports.jobs.CollectJobs(t.Context())
			require.NoError(t, err)

			for i := range beforeJobs {
				beforeJobs[i] = bytes.Clone(beforeJobs[i])
			}

			env, err := appsummary.StageResult(t.Context(), ports, ports, ports, appsummary.StageResultInput{
				StagePlanJSON: `{"version":1,"stage":"build","targets":{"cargo":{"runs":true},"npm":{"runs":true}}}`,
				JobResultsMap: tc.jobMap,
				JSONOutputKey: "artifacts-json",
				JSONFields:    []domainsummary.KeyValue{{Key: "package", Value: "synthetic"}},
			})
			require.ErrorIs(t, err, errs.ErrMalformedInput)
			require.Nil(t, env)
			require.Contains(t, err.Error(), `duplicate field "`+tc.field+`"`)
			require.NotContains(t, err.Error(), "p405-private-value")

			wantEvents := []resultSinkEvent(nil)
			if tc.jobMap == "" {
				wantEvents = []resultSinkEvent{{"CollectJobs", "", ""}}
			} else {
				require.Contains(t, err.Error(), `job "npm"`)
			}

			require.Equal(t, wantEvents, ports.events, "no publication, fallback or job writes")
			require.Equal(t, beforeManifest, ports.manifest.Body("build"))
			require.Equal(t, []string{"build"}, ports.manifest.Stages())
			require.Equal(t, beforeScalars, ports.out.AllScalar())
			require.Equal(t, beforeKeys, ports.out.Keys())
			require.Equal(t, []string{"seeded", "lines"}, ports.out.Multiline("retained-lines"))
			require.Zero(t, ports.out.CloseCount())
			afterJobs, err := ports.jobs.CollectJobs(t.Context())
			require.NoError(t, err)
			require.Equal(t, beforeJobs, afterJobs)
		})
	}
}

func TestStageResult_JobMemberPolicyPreservesSourcePriorityAndPublication(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		explicit []domainsummary.KeyValue
		jobMap   string
		result   string
	}{
		{
			name:     "explicit_ignores_duplicate_lower_map_and_store",
			explicit: []domainsummary.KeyValue{{Key: "cargo", Value: "success"}, {Key: "npm", Value: "cancelled"}},
			jobMap:   `{"cargo":{"result":"success"},"npm":{"result":"failure","result":"success"}}`,
			result:   "cancelled",
		},
		{
			name:   "map_ignores_duplicate_stored_members",
			jobMap: `{"cargo":{"result":"success"},"npm":{"result":"success"}}`,
			result: "success",
		},
		{
			name:   "selected_map_single_alias_and_opaque_additions",
			jobMap: `{"cargo":{"result":"success"},"npm":{"outputs":1e10000,"re\u017fult":"success","outputs":{"result":"failure","result":"success"}}}`,
			result: "success",
		},
		{
			name:   "selected_store_single_alias_and_opaque_additions",
			result: "success",
		},
		{
			name:   "outer_duplicate_failure_then_success",
			jobMap: `{"cargo":{"result":"success"},"npm":{"result":"failure"},"npm":{"result":"success"}}`,
			result: "failure",
		},
		{
			name:   "outer_duplicate_success_then_failure",
			jobMap: `{"cargo":{"result":"success"},"npm":{"result":"success"},"npm":{"result":"failure"}}`,
			result: "failure",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ports := newResultFailurePorts(t, 0)
			ports.jobs.Seed("cargo", `{"version":1,"job":"cargo","result":"success"}`)

			if tc.jobMap == "" {
				ports.jobs.Seed("npm", `{"ver\u017fion":1,"JOB":"npm","re\u017fult":"success","outputs":1e10000,"outputs":{"version":2,"version":1,"job":"other","job":"npm","result":"failure","result":"success"}}`)
			} else {
				ports.jobs.Seed("npm", `{"version":1,"job":"npm","result":"failure","result":"success"}`)
				ports.jobs.Seed("version", `{"version":2,"version":1,"job":"npm","result":"failure"}`)
				ports.jobs.Seed("renamed", `{"version":1,"job":"npm","job":"other","result":"failure"}`)
			}

			beforeJobs, err := ports.jobs.CollectJobs(t.Context())
			require.NoError(t, err)

			for i := range beforeJobs {
				beforeJobs[i] = bytes.Clone(beforeJobs[i])
			}

			env, err := appsummary.StageResult(t.Context(), ports, ports, ports, appsummary.StageResultInput{
				StagePlanJSON: `{"version":1,"stage":"build","targets":{"cargo":{"runs":true},"npm":{"runs":true}}}`,
				Results:       tc.explicit, JobResultsMap: tc.jobMap,
				JSONOutputKey: "artifacts-json",
				JSONFields:    []domainsummary.KeyValue{{Key: "package", Value: "synthetic"}},
			})
			require.NoError(t, err)
			require.NotNil(t, env)
			require.Equal(t, "build", env.Stage)
			require.Equal(t, domainsummary.Result(tc.result), env.Result)
			require.True(t, env.Ran)
			require.Empty(t, env.Extras)
			require.Equal(t, []domainsummary.Target{{Name: "cargo", Result: domainsummary.ResultSuccess}, {Name: "npm", Result: domainsummary.Result(tc.result)}}, env.Targets)
			wantJSON := `{"version":1,"stage":"build","result":"` + tc.result + `","ran":true,"targets":{"cargo":"success","npm":"` + tc.result + `"}}`
			body, err := env.MarshalJSON()
			require.NoError(t, err)

			if string(body) != wantJSON {
				t.Errorf("returned JSON = %s, want exact bytes %s", body, wantJSON)
			}

			const optionalJSON = `{"package":"synthetic"}`

			wantEvents := []resultSinkEvent{}
			if tc.jobMap == "" {
				wantEvents = append(wantEvents, resultSinkEvent{"CollectJobs", "", ""})
			}

			wantEvents = append(wantEvents,
				resultSinkEvent{"WriteJSON", "build", wantJSON},
				resultSinkEvent{"SetBool", "stage-ran", "true"},
				resultSinkEvent{"Set", "stage-result", tc.result},
				resultSinkEvent{"Set", "result-json", wantJSON},
				resultSinkEvent{"Set", "artifacts-json", optionalJSON},
			)
			require.Equal(t, wantEvents, ports.events)
			require.Equal(t, []string{"build"}, ports.manifest.Stages())

			if got := ports.manifest.Body("build"); got != wantJSON {
				t.Errorf("manifest JSON = %s, want exact bytes %s", got, wantJSON)
			}

			require.Equal(t, map[string]string{"stage-ran": "true", "stage-result": tc.result, "result-json": wantJSON, "artifacts-json": optionalJSON}, ports.out.AllScalar())
			require.Equal(t, []string{"artifacts-json", "result-json", "stage-ran", "stage-result"}, ports.out.Keys())

			for _, key := range ports.out.Keys() {
				require.Nil(t, ports.out.Multiline(key))
			}

			require.Zero(t, ports.out.CloseCount())
			afterJobs, err := ports.jobs.CollectJobs(t.Context())
			require.NoError(t, err)
			require.Equal(t, beforeJobs, afterJobs)
		})
	}
}
