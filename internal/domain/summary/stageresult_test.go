// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
)

func TestAggregateResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		given []summary.Result
		want  summary.Result
	}{
		{
			name:  "failure_wins",
			given: []summary.Result{summary.ResultSuccess, summary.ResultFailure, summary.ResultCancelled},
			want:  summary.ResultFailure,
		},
		{
			name:  "cancelled_over_success",
			given: []summary.Result{summary.ResultSuccess, summary.ResultCancelled, summary.ResultSuccess},
			want:  summary.ResultCancelled,
		},
		{
			name:  "all_success",
			given: []summary.Result{summary.ResultSuccess, summary.ResultSuccess, summary.ResultSkipped},
			want:  summary.ResultSuccess,
		},
		{name: "empty_defaults_to_success", want: summary.ResultSuccess},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := summary.AggregateResults(testCase.given); got != testCase.want {
				t.Errorf("got %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestStageResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		ran   bool
		given []summary.Result
		want  summary.Result
	}{
		{name: "not_ran_is_skipped", given: []summary.Result{summary.ResultFailure}, want: summary.ResultSkipped},
		{name: "ran_aggregates", ran: true, given: []summary.Result{summary.ResultFailure, summary.ResultSuccess}, want: summary.ResultFailure},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := summary.StageResult(testCase.ran, testCase.given); got != testCase.want {
				t.Errorf("got %q, want %q", got, testCase.want)
			}
		})
	}
}

//nolint:cyclop // verifies every JSON envelope field on one fixture.
func TestStageResultEnvelope_JSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		envelope     summary.StageResultEnvelope
		wantResult   string
		wantRan      bool
		wantTargets  map[string]string
		wantExtraKey string
		wantExtraVal string
	}{
		{
			name: "targets",
			envelope: summary.StageResultEnvelope{
				Stage:  "build", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Result: summary.ResultSuccess,
				Ran:    true,
				Targets: []summary.Target{
					{Name: "maven", Result: summary.ResultSuccess},
					{Name: "npm", Result: summary.ResultSkipped}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				},
			},
			wantResult:  "success", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			wantRan:     true,
			wantTargets: map[string]string{"maven": "success", "npm": "skipped"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		{
			name: "extras",
			envelope: summary.StageResultEnvelope{
				Stage:   "build",
				Result:  summary.ResultSuccess,
				Ran:     true,
				Extras:  []summary.KeyValue{{Key: "project_type", Value: "npm"}},
				Targets: []summary.Target{{Name: "npm", Result: summary.ResultSuccess}},
			},
			wantResult:   "success",
			wantRan:      true,
			wantTargets:  map[string]string{"npm": "success"},
			wantExtraKey: "project_type",
			wantExtraVal: "npm",
		},
		{
			name:        "not_ran_false",
			envelope:    summary.StageResultEnvelope{Stage: "build", Result: summary.ResultSkipped, Ran: false},
			wantResult:  "skipped",
			wantTargets: map[string]string{},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			b, err := testCase.envelope.MarshalJSON() //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			if err != nil {
				t.Fatal(err)
			}

			var got map[string]any
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("invalid JSON %s: %v", b, err)
			}

			if got["version"] != float64(summary.StageResultEnvelopeVersion) || got["stage"] != "build" || got["result"] != testCase.wantResult || got["ran"] != testCase.wantRan {
				t.Errorf("base fields = %+v", got)
			}

			targets, ok := got["targets"].(map[string]any)
			if !ok {
				t.Fatalf("targets = %#v", got["targets"])
			}

			if len(targets) != len(testCase.wantTargets) {
				t.Fatalf("targets = %#v, want %#v", targets, testCase.wantTargets)
			}

			for name, want := range testCase.wantTargets {
				if targets[name] != want {
					t.Errorf("target %q = %q, want %q", name, targets[name], want)
				}
			}

			if testCase.wantExtraKey != "" && got[testCase.wantExtraKey] != testCase.wantExtraVal {
				t.Errorf("extra %q = %q, want %q", testCase.wantExtraKey, got[testCase.wantExtraKey], testCase.wantExtraVal)
			}
		})
	}
}

func TestParseStageResultEnvelope(t *testing.T) {
	t.Parallel()

	env, err := summary.ParseStageResultEnvelope(`{"version":1,"stage":"build","result":"failure","ran":true,"targets":{"npm":"success","maven":"failure"}}`)
	if err != nil {
		t.Fatal(err)
	}

	if env.Version != summary.StageResultEnvelopeVersion || env.Stage != "build" || env.Result != summary.ResultFailure || !env.Ran {
		t.Errorf("envelope = %+v", env)
	}

	if got := env.TargetResult("npm"); got != summary.ResultSuccess {
		t.Errorf("npm = %q", got)
	}

	if got := env.TargetResult("missing"); got != summary.ResultSkipped {
		t.Errorf("missing = %q", got)
	}
}

func TestParseStageResultEnvelope_RejectsInvalidContracts(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		json string
	}{
		{name: "missing version", json: `{"stage":"build","result":"success","targets":{}}`},
		{name: "bad result", json: `{"version":1,"stage":"build","result":"in_progress","targets":{}}`},
		{name: "bad target result", json: `{"version":1,"stage":"build","result":"success","targets":{"npm":"in_progress"}}`},
		{name: "missing stage", json: `{"version":1,"result":"success","targets":{}}`},
		// Wrong JSON shape and bad syntax are caller-input errors too, so
		// they must classify as malformed input (EX_DATAERR), not the
		// unclassified internal-bug default.
		{name: "array instead of object", json: `[{"name":"lint"}]`},
		{name: "syntax error", json: `{not json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := summary.ParseStageResultEnvelope(tc.json)
			if err == nil {
				t.Fatal("expected invalid contract to fail")
			}

			if !errors.Is(err, errs.ErrMalformedInput) {
				t.Errorf("err should wrap errs.ErrMalformedInput, got %v", err)
			}
		})
	}
}
