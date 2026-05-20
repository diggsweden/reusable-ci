// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
)

func TestJobResultEnvelope_MarshalRoundTrip(t *testing.T) {
	t.Parallel()

	env := summary.JobResultEnvelope{Job: "nanolinter", Result: summary.ResultSuccess}

	raw, err := env.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	if got, want := string(raw), `{"version":1,"job":"nanolinter","result":"success"}`; got != want {
		t.Fatalf("marshal = %s, want %s", got, want)
	}

	parsed, err := summary.ParseJobResultEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}

	if parsed.Job != "nanolinter" || parsed.Result != summary.ResultSuccess {
		t.Errorf("parsed = %+v", parsed)
	}
}

func TestParseJobResultEnvelope_Rejects(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{name: "not json", in: `nope`, want: "not a valid job-result object"},
		{name: "bad version", in: `{"version":2,"job":"x","result":"success"}`, want: "unsupported version 2"},
		{name: "missing job", in: `{"version":1,"job":"","result":"success"}`, want: "missing job"},
		{name: "invalid result", in: `{"version":1,"job":"x","result":"running"}`, want: `invalid result "running"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := summary.ParseJobResultEnvelope([]byte(tc.in))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseJobResultsMap(t *testing.T) {
	t.Parallel()

	// The toJson(needs) shape: extra fields (outputs) are ignored, unknown
	// status is fail-closed, records sort by name.
	records, err := summary.ParseJobResultsMap([]byte(`{
		"npm": {"result": "success", "outputs": {}},
		"maven": {"result": "failure"},
		"weird": {"result": "in_progress"}
	}`))
	if err != nil {
		t.Fatal(err)
	}

	got := make(map[string]summary.Result, len(records))
	for _, rec := range records {
		got[rec.Job] = rec.Result
	}

	if got["npm"] != summary.ResultSuccess || got["maven"] != summary.ResultFailure {
		t.Errorf("records = %+v", got)
	}
	// Fail-closed: an unrecognised status becomes failure, never skipped.
	if got["weird"] != summary.ResultFailure {
		t.Errorf("weird = %q, want failure (fail-closed)", got["weird"])
	}
}

func TestParseJobResultsMap_Invalid(t *testing.T) {
	t.Parallel()

	if _, err := summary.ParseJobResultsMap([]byte(`not-json`)); err == nil ||
		!strings.Contains(err.Error(), "not a valid {job:{result}} object") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveTargetResults_MatchingAndKebabSnake(t *testing.T) {
	t.Parallel()

	jobs := []summary.JobResultEnvelope{
		{Version: 1, Job: "maven", Result: summary.ResultSuccess},
		{Version: 1, Job: "gradle-android", Result: summary.ResultFailure},
		{Version: 1, Job: "unrelated-status", Result: summary.ResultSuccess},
	}

	got := summary.ResolveTargetResults([]string{"maven", "gradle_android", "npm"}, jobs)

	if got["maven"] != summary.ResultSuccess {
		t.Errorf("maven = %q", got["maven"])
	}
	// snake_case target matches the kebab-case job record.
	if got["gradle_android"] != summary.ResultFailure {
		t.Errorf("gradle_android = %q", got["gradle_android"])
	}
	// Unmatched target is omitted (caller fail-closes it).
	if _, ok := got["npm"]; ok {
		t.Errorf("npm should be absent, got %q", got["npm"])
	}
	// Unrelated record does not leak in.
	if _, ok := got["unrelated-status"]; ok {
		t.Errorf("unrelated-status should not appear")
	}
}
