// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary_test

import (
	"encoding/json"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/summary"
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
				Stage:  "build",
				Result: summary.ResultSuccess,
				Ran:    true,
				Targets: []summary.Target{
					{Name: "maven", Result: summary.ResultSuccess},
					{Name: "npm", Result: summary.ResultSkipped},
				},
			},
			wantResult:  "success",
			wantRan:     true,
			wantTargets: map[string]string{"maven": "success", "npm": "skipped"},
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
			b, err := testCase.envelope.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("invalid JSON %s: %v", b, err)
			}
			if got["stage"] != "build" || got["result"] != testCase.wantResult || got["ran"] != testCase.wantRan {
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

func TestSortTargetsByName_ReturnsSortedCopy(t *testing.T) {
	t.Parallel()

	in := []summary.Target{
		{Name: "z", Result: summary.ResultSuccess},
		{Name: "a", Result: summary.ResultFailure},
	}
	got := summary.SortTargetsByName(in)
	if got[0].Name != "a" || got[1].Name != "z" {
		t.Errorf("sorted targets = %+v", got)
	}
	if in[0].Name != "z" || in[1].Name != "a" {
		t.Errorf("input was mutated: %+v", in)
	}
}
