// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
)

func TestAggregateResults_PicksTheWorstResultAndDefaultsToSuccess(t *testing.T) {
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

func TestStageResult_IsSkippedUnlessTheStageRan(t *testing.T) {
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

func TestParseStageResultEnvelope_ReadsVersionStageResultAndTargets(t *testing.T) {
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

// TestParseStageResultEnvelope_ResultAndRanMustFollowTheTargets: the stage
// result and ran flag are derived from the targets when the envelope is
// written, so an envelope that disagrees with its own targets is refused
// rather than trusted: success over a failed or cancelled target, cancelled
// over a failure, a stage that ran with every target skipped, a stage that
// did not run with a running target, or skipped for a stage that ran. The
// consistent envelopes, including the empty skipped stage, still parse.
func TestParseStageResultEnvelope_ResultAndRanMustFollowTheTargets(t *testing.T) {
	t.Parallel()

	envelope := func(result string, ran bool, targets string) string {
		return fmt.Sprintf(`{"version":1,"stage":"build","result":%q,"ran":%t,"targets":{%s}}`, result, ran, targets)
	}

	for name, tc := range map[string]struct {
		json string
		ok   bool
	}{
		"success over a failed target":        {json: envelope("success", true, `"maven":"success","npm":"failure"`)},
		"success over a cancelled target":     {json: envelope("success", true, `"npm":"cancelled"`)},
		"cancelled over a failed target":      {json: envelope("cancelled", true, `"maven":"cancelled","npm":"failure"`)},
		"ran with every target skipped":       {json: envelope("success", true, `"npm":"skipped"`)},
		"not ran with a running target":       {json: envelope("skipped", false, `"npm":"success"`)},
		"skipped result for a stage that ran": {json: envelope("skipped", true, `"npm":"success"`)},
		"success for an empty stage":          {json: envelope("success", false, ``)},
		"failure wins":                        {json: envelope("failure", true, `"maven":"cancelled","npm":"failure","go":"skipped"`), ok: true},
		"cancelled over success":              {json: envelope("cancelled", true, `"maven":"success","npm":"cancelled"`), ok: true},
		"skipped targets do not run a stage":  {json: envelope("skipped", false, `"npm":"skipped"`), ok: true},
		"empty stage is skipped":              {json: envelope("skipped", false, ``), ok: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := summary.ParseStageResultEnvelope(tc.json)
			if tc.ok {
				if err != nil {
					t.Fatalf("consistent envelope refused: %v", err)
				}

				return
			}

			if !errors.Is(err, errs.ErrMalformedInput) {
				t.Errorf("err = %v, want ErrMalformedInput for %s", err, tc.json)
			}
		})
	}
}

// TestParseStageResultEnvelope_RefusesDuplicateMembersAndTargets refuses an
// envelope that names a consumed member or a target twice, including a member
// spelled in another case, which encoding/json binds to the same field. Each
// would let a later value replace an earlier one: a failed target followed by
// the same target's success used to parse as a clean success. A repeated
// member the contract does not consume stays accepted, as unknown extensions
// are.
func TestParseStageResultEnvelope_RefusesDuplicateMembersAndTargets(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, in string
		wantErr  bool
	}{
		{"duplicate target hides a failure", `{"version":1,"stage":"build","result":"success","ran":true,"targets":{"a":"failure","a":"success"}}`, true},
		{"duplicate target under a failed stage", `{"version":1,"stage":"build","result":"failure","ran":true,"targets":{"a":"success","a":"failure"}}`, true},
		{"duplicate result", `{"version":1,"stage":"build","result":"failure","ran":true,"targets":{"a":"success"},"result":"success"}`, true},
		{"result in another case", `{"version":1,"stage":"build","result":"failure","ran":true,"targets":{"a":"success"},"Result":"success"}`, true},
		{"duplicate targets member", `{"version":1,"stage":"build","result":"success","ran":true,"targets":{"a":"failure"},"targets":{"a":"success"}}`, true},
		{"target that is not a result string", `{"version":1,"stage":"build","result":"success","ran":true,"targets":{"a":1}}`, true},
		{"targets that are not an object", `{"version":1,"stage":"build","result":"success","ran":true,"targets":["a"]}`, true},
		{"repeated unconsumed extension", `{"version":1,"stage":"build","result":"success","ran":true,"targets":{"a":"success"},"note":"x","note":"y"}`, false},
		{"distinct case-variant target names", `{"version":1,"stage":"build","result":"success","ran":true,"targets":{"a":"success","A":"success"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelope, err := summary.ParseStageResultEnvelope(tc.in)
			if tc.wantErr {
				if !errors.Is(err, errs.ErrMalformedInput) {
					t.Fatalf("envelope = %+v, err = %v, want ErrMalformedInput", envelope, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("err = %v, want accepted", err)
			}
		})
	}
}
