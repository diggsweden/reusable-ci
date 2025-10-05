// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
	"github.com/stretchr/testify/require"
)

func TestParseJobResultsMap_RejectsDuplicateConsumedMembers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, body string }{
		{"failure_then_success", `{"cargo":{"result":"success"},"npm":{"result":"failure","result":"success"}}`},
		{"success_then_failure", `{"npm":{"result":"success","result":"failure"}}`},
		{"identical", `{"npm":{"result":"success","result":"success"}}`},
		{"null_then_valid", `{"npm":{"result":null,"result":"success"}}`},
		{"valid_then_null", `{"npm":{"result":"success","result":null}}`},
		{"both_null", `{"npm":{"result":null,"result":null}}`},
		{"case_alias_last", `{"npm":{"result":"failure","RESULT":"success"}}`},
		{"case_alias_first", `{"npm":{"RESULT":"failure","result":"success"}}`},
		{"escaped_alias_last", `{"npm":{"result":"failure","re\u0073ult":"success"}}`},
		{"escaped_alias_first", `{"npm":{"re\u0073ult":"failure","result":"success"}}`},
		{"unicode_alias_last", `{"npm":{"result":"failure","re\u017fult":"success"}}`},
		{"unicode_alias_first", `{"npm":{"re\u017fult":"failure","result":"success"}}`},
		{"no_exact_key", `{"npm":{"RESULT":"failure","re\u017fult":"success"}}`},
		{"private_value_first", `{"npm":{"result":"p405-private-value","result":"success"}}`},
		{"private_value_last", `{"npm":{"result":"success","result":"p405-private-value"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			records, err := summary.ParseJobResultsMap([]byte(tc.body))
			require.ErrorIs(t, err, errs.ErrMalformedInput)
			require.Nil(t, records, "no partial records, even after a valid earlier cargo")
			require.Contains(t, err.Error(), `duplicate field "result"`)
			require.Contains(t, err.Error(), `job "npm"`)
			require.NotContains(t, err.Error(), "p405-private-value")
		})
	}
}

func TestParseJobResultEnvelope_RejectsDuplicateConsumedMembers(t *testing.T) {
	t.Parallel()
	// Each document duplicates only one consumed field so no other guard can
	// account for its refusal. Key spellings are literal wire JSON, not marshaled.
	for _, tc := range []struct{ name, field, body string }{
		{"result_failure_then_success", "result", `{"version":1,"job":"npm","result":"failure","result":"success"}`},
		{"result_success_then_failure", "result", `{"version":1,"job":"npm","result":"success","result":"failure"}`},
		{"result_identical", "result", `{"version":1,"job":"npm","result":"success","result":"success"}`},
		{"result_null_then_valid", "result", `{"version":1,"job":"npm","result":null,"result":"success"}`},
		{"result_valid_then_null", "result", `{"version":1,"job":"npm","result":"success","result":null}`},
		{"result_both_null", "result", `{"version":1,"job":"npm","result":null,"result":null}`},
		{"result_case_last", "result", `{"version":1,"job":"npm","result":"failure","RESULT":"success"}`},
		{"result_case_first", "result", `{"version":1,"job":"npm","RESULT":"failure","result":"success"}`},
		{"result_escaped_last", "result", `{"version":1,"job":"npm","result":"failure","re\u0073ult":"success"}`},
		{"result_escaped_first", "result", `{"version":1,"job":"npm","re\u0073ult":"failure","result":"success"}`},
		{"result_unicode_last", "result", `{"version":1,"job":"npm","result":"failure","re\u017fult":"success"}`},
		{"result_unicode_first", "result", `{"version":1,"job":"npm","re\u017fult":"failure","result":"success"}`},
		{"result_no_exact_key", "result", `{"version":1,"job":"npm","RESULT":"failure","re\u017fult":"success"}`},
		{"result_private_first", "result", `{"version":1,"job":"npm","result":"p405-private-value","result":"success"}`},
		{"result_private_last", "result", `{"version":1,"job":"npm","result":"success","result":"p405-private-value"}`},
		{"version_two_then_one", "version", `{"version":2,"version":1,"job":"npm","result":"success"}`},
		{"version_one_then_two", "version", `{"version":1,"version":2,"job":"npm","result":"success"}`},
		{"version_identical", "version", `{"version":1,"version":1,"job":"npm","result":"success"}`},
		{"version_null_then_valid", "version", `{"version":null,"version":1,"job":"npm","result":"success"}`},
		{"version_valid_then_null", "version", `{"version":1,"version":null,"job":"npm","result":"success"}`},
		{"version_both_null", "version", `{"version":null,"version":null,"job":"npm","result":"success"}`},
		{"version_case_last", "version", `{"version":2,"VERSION":1,"job":"npm","result":"success"}`},
		{"version_case_first", "version", `{"VERSION":2,"version":1,"job":"npm","result":"success"}`},
		{"version_escaped_last", "version", `{"version":2,"ver\u0073ion":1,"job":"npm","result":"success"}`},
		{"version_escaped_first", "version", `{"ver\u0073ion":2,"version":1,"job":"npm","result":"success"}`},
		{"version_unicode_last", "version", `{"version":2,"ver\u017fion":1,"job":"npm","result":"success"}`},
		{"version_unicode_first", "version", `{"ver\u017fion":2,"version":1,"job":"npm","result":"success"}`},
		{"version_no_exact_key", "version", `{"VERSION":2,"ver\u017fion":1,"job":"npm","result":"success"}`},
		{"job_rename_failure", "job", `{"version":1,"job":"npm","job":"unrelated","result":"failure"}`},
		{"job_rename_reverse", "job", `{"version":1,"job":"unrelated","job":"npm","result":"failure"}`},
		{"job_identical", "job", `{"version":1,"job":"npm","job":"npm","result":"success"}`},
		{"job_null_then_valid", "job", `{"version":1,"job":null,"job":"npm","result":"success"}`},
		{"job_valid_then_null", "job", `{"version":1,"job":"npm","job":null,"result":"success"}`},
		{"job_both_null", "job", `{"version":1,"job":null,"job":null,"result":"success"}`},
		{"job_case_last", "job", `{"version":1,"job":"npm","JOB":"unrelated","result":"failure"}`},
		{"job_case_first", "job", `{"version":1,"JOB":"npm","job":"unrelated","result":"failure"}`},
		{"job_escaped_last", "job", `{"version":1,"job":"npm","j\u006fb":"unrelated","result":"failure"}`},
		{"job_escaped_first", "job", `{"version":1,"j\u006fb":"npm","job":"unrelated","result":"failure"}`},
		{"job_private_first", "job", `{"version":1,"job":"p405-private-value","job":"npm","result":"success"}`},
		{"job_private_last", "job", `{"version":1,"job":"npm","job":"p405-private-value","result":"success"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			record, err := summary.ParseJobResultEnvelope([]byte(tc.body))
			require.ErrorIs(t, err, errs.ErrMalformedInput)
			require.Equal(t, summary.JobResultEnvelope{}, record)
			require.Contains(t, err.Error(), `duplicate field "`+tc.field+`"`)
			require.NotContains(t, err.Error(), "p405-private-value")
		})
	}
}

func TestParseJobResultsMap_PreservesSingleAliasesAndAdditiveMembers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, body string }{
		{"case", `{"npm":{"RESULT":"success"}}`},
		{"escaped", `{"npm":{"re\u0073ult":"success"}}`},
		{"unicode", `{"npm":{"re\u017fult":"success"}}`},
		{"unconsumed_envelope_fields", `{"npm":{"version":2,"version":1,"job":"other","job":"npm","result":"success"}}`},
		{"repeated_unknown", `{"npm":{"outputs":null,"result":"success","outputs":{"name":"value"}}}`},
		{"nested_duplicates", `{"npm":{"outputs":{"result":"failure","result":"success","items":[{"version":2,"version":1,"job":"other","job":"npm"}]},"result":"success"}}`},
		{"large_number_before", `{"npm":{"future":1e10000,"result":"success"}}`},
		{"large_number_after", `{"npm":{"result":"success","future":-1e10000}}`},
		{"large_nested_number", `{"npm":{"future":[{"n":1e10000,"n":9007199254740993}],"result":"success"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			records, err := summary.ParseJobResultsMap([]byte(tc.body))
			require.NoError(t, err)
			require.Equal(t, []summary.JobResultEnvelope{{Version: 1, Job: "npm", Result: summary.ResultSuccess}}, records)
		})
	}
}

func TestParseJobResultEnvelope_PreservesSingleAliasesAndAdditiveMembers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, body string }{
		{"version_case", `{"VERSION":1,"job":"npm","result":"success"}`},
		{"version_escaped", `{"ver\u0073ion":1,"job":"npm","result":"success"}`},
		{"version_unicode", `{"ver\u017fion":1,"job":"npm","result":"success"}`},
		{"job_case", `{"version":1,"JOB":"npm","result":"success"}`},
		{"job_escaped", `{"version":1,"j\u006fb":"npm","result":"success"}`},
		{"result_case", `{"version":1,"job":"npm","RESULT":"success"}`},
		{"result_escaped", `{"version":1,"job":"npm","re\u0073ult":"success"}`},
		{"result_unicode", `{"version":1,"job":"npm","re\u017fult":"success"}`},
		{"repeated_unknown", `{"outputs":null,"version":1,"job":"npm","result":"success","outputs":{"name":"value"}}`},
		{"nested_duplicates", `{"outputs":{"version":2,"version":1,"job":"other","job":"npm","items":[{"result":"failure","result":"success"}]},"version":1,"job":"npm","result":"success"}`},
		{"large_number_before", `{"future":1e10000,"version":1,"job":"npm","result":"success"}`},
		{"large_number_after", `{"version":1,"job":"npm","result":"success","future":-1e10000}`},
		{"large_nested_number", `{"version":1,"job":"npm","future":[{"n":1e10000,"n":9007199254740993}],"result":"success"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			record, err := summary.ParseJobResultEnvelope([]byte(tc.body))
			require.NoError(t, err)
			require.Equal(t, summary.JobResultEnvelope{Version: 1, Job: "npm", Result: summary.ResultSuccess}, record)
		})
	}
}

func TestParseJobResultsMap_RootDuplicatesRemainSeparateAndConservative(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, body string
		results    []summary.JobResultEnvelope
		want       summary.Result
	}{
		{
			name: "failure_then_success", body: `{"npm":{"result":"failure"},"npm":{"result":"success"}}`,
			results: []summary.JobResultEnvelope{{Version: 1, Job: "npm", Result: summary.ResultFailure}, {Version: 1, Job: "npm", Result: summary.ResultSuccess}},
			want:    summary.ResultFailure,
		},
		{
			name: "success_then_failure", body: `{"npm":{"result":"success"},"npm":{"result":"failure"}}`,
			results: []summary.JobResultEnvelope{{Version: 1, Job: "npm", Result: summary.ResultSuccess}, {Version: 1, Job: "npm", Result: summary.ResultFailure}},
			want:    summary.ResultFailure,
		},
		{
			name: "escaped_identity", body: `{"npm":{"result":"failure"},"n\u0070m":{"result":"success"}}`,
			results: []summary.JobResultEnvelope{{Version: 1, Job: "npm", Result: summary.ResultFailure}, {Version: 1, Job: "npm", Result: summary.ResultSuccess}},
			want:    summary.ResultFailure,
		},
		{
			name: "identical_successes", body: `{"npm":{"result":"success"},"npm":{"result":"success"}}`,
			results: []summary.JobResultEnvelope{{Version: 1, Job: "npm", Result: summary.ResultSuccess}, {Version: 1, Job: "npm", Result: summary.ResultSuccess}},
			want:    summary.ResultSuccess,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			records, err := summary.ParseJobResultsMap([]byte(tc.body))
			require.NoError(t, err)
			require.Equal(t, tc.results, records)
			require.Equal(t, map[string]summary.Result{"npm": tc.want}, summary.ResolveTargetResults([]string{"npm"}, records))
		})
	}
}

func TestParseJobResultsMap_PreservesFailClosedEmptyChildren(t *testing.T) {
	t.Parallel()

	for _, body := range []string{`{"npm":null}`, `{"npm":{}}`, `{"npm":{"result":null}}`} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()

			records, err := summary.ParseJobResultsMap([]byte(body))
			require.NoError(t, err)
			require.Equal(t, []summary.JobResultEnvelope{{Version: 1, Job: "npm", Result: summary.ResultFailure}}, records)
		})
	}

	records, err := summary.ParseJobResultsMap([]byte(`{}`))
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestParseJobResultsMap_RetainsTypedAndSyntaxChecks(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`null`, `[]`, `true`, `1e10000`, `"object"`,
		`{"cargo":{"result":"success"},"npm":[]}`,
		`{"cargo":{"result":"success"},"npm":true}`,
		`{"cargo":{"result":"success"},"npm":1e10000}`,
		`{"cargo":{"result":"success"},"npm":"success"}`,
		`{"npm":{"result":1}}`, `{"npm":{"result":true}}`,
		`{"npm":{"result":[]}}`, `{"npm":{"result":{}}}`,
		`{"npm":{"result":"success","future":1e}}`,
		`{"npm":{"result":"success","future":[{"n":1},]}}`,
		`{"npm":{"result":"success"}`, `{"npm":{"result":"success"}} null`,
	} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()

			records, err := summary.ParseJobResultsMap([]byte(body))
			require.ErrorIs(t, err, errs.ErrMalformedInput)
			require.Nil(t, records)
		})
	}
}

func TestParseJobResultEnvelope_RetainsTypedSyntaxAndStrictStatusChecks(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`null`, `[]`, `true`, `1e10000`, `"object"`, `{}`,
		`{"version":"1","job":"npm","result":"success"}`,
		`{"version":1.0,"job":"npm","result":"success"}`,
		`{"version":1e10000,"job":"npm","result":"success"}`,
		`{"version":1,"job":123,"result":"success"}`,
		`{"version":1,"job":{},"result":"success"}`,
		`{"version":1,"job":"npm","result":123}`,
		`{"version":1,"job":"npm","result":true}`,
		`{"version":1,"job":"npm","result":[]}`,
		`{"version":1,"job":"npm","result":{}}`,
		`{"version":1,"job":"npm","result":null}`,
		`{"version":1,"job":"npm","result":""}`,
		`{"version":1,"job":"npm","result":"failed"}`,
		`{"version":1,"job":"npm","result":"canceled"}`,
		`{"version":1,"job":"npm","result":" success "}`,
		`{"version":1,"job":"npm","result":"SUCCESS"}`,
		`{"version":1,"job":"npm","result":"success","future":1e}`,
		`{"version":1,"job":"npm","result":"success","future":[{"n":1},]}`,
		`{"version":1,"job":"npm","result":"success"`,
		`{"version":1,"job":"npm","result":"success"} null`,
	} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()

			record, err := summary.ParseJobResultEnvelope([]byte(body))
			require.ErrorIs(t, err, errs.ErrMalformedInput)
			require.Equal(t, summary.JobResultEnvelope{}, record)
		})
	}
}
