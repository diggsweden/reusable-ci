// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainsummary "github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakejobresultstore"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakemanifestsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

const resultMatrixPlan = `{"version":1,"stage":"build","targets":{"cargo":{"runs":true},"npm":{"runs":true},"xcode":{"runs":true}}}`

func TestStageResult_StatusAssignmentMatrix(t *testing.T) {
	t.Parallel()
	// Target traversal stays cargo/npm/xcode. Permuting only the input slice
	// would not exercise a decisive result in each aggregation position.
	for _, tc := range []struct {
		name     string
		statuses [3]string
		want     string
	}{
		{"failure_cancelled_success", [3]string{"failure", "cancelled", "success"}, "failure"},
		{"failure_success_cancelled", [3]string{"failure", "success", "cancelled"}, "failure"},
		{"cancelled_failure_success", [3]string{"cancelled", "failure", "success"}, "failure"},
		{"cancelled_success_failure", [3]string{"cancelled", "success", "failure"}, "failure"},
		{"success_failure_cancelled", [3]string{"success", "failure", "cancelled"}, "failure"},
		{"success_cancelled_failure", [3]string{"success", "cancelled", "failure"}, "failure"},
		{"cancelled_success_success", [3]string{"cancelled", "success", "success"}, "cancelled"},
		{"success_cancelled_success", [3]string{"success", "cancelled", "success"}, "cancelled"},
		{"success_success_cancelled", [3]string{"success", "success", "cancelled"}, "cancelled"},
		{"all_success", [3]string{"success", "success", "success"}, "success"},
	} {
		for _, source := range []string{"explicit", "map", "store"} {
			t.Run(tc.name+"/"+source, func(t *testing.T) {
				t.Parallel()
				in, jobs := resultMatrixInput(t, source, resultMatrixPlan, []domainsummary.KeyValue{
					{Key: "cargo", Value: tc.statuses[0]},
					{Key: "npm", Value: tc.statuses[1]},
					{Key: "xcode", Value: tc.statuses[2]},
				})
				out, manifest := fakeoutputsink.New(t), fakemanifestsink.New(t)

				env, err := appsummary.StageResult(t.Context(), out, manifest, jobs, in)
				if err != nil {
					t.Fatal(err)
				}

				assertResultMatrixPublication(t, env, out, manifest, tc.statuses, tc.want, true)
				jobs.assertCalls(source)
			})
		}
	}
}

func TestStageResult_PlannedRunsMatrix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		plan    string
		results []domainsummary.KeyValue
		want    [3]string
		result  string
		ran     bool
	}{
		{
			name: "all_disabled_with_missing_record",
			plan: `{"version":1,"stage":"build","targets":{"cargo":{"runs":false},"npm":{"runs":false},"xcode":{"runs":false}}}`,
			results: []domainsummary.KeyValue{
				{Key: "cargo", Value: "failure"}, {Key: "npm", Value: "cancelled"},
			},
			want: [3]string{"skipped", "skipped", "skipped"}, result: "skipped", ran: false,
		},
		{
			name: "only_middle_enabled_with_missing_disabled_record",
			plan: `{"version":1,"stage":"build","targets":{"cargo":{"runs":false},"npm":{"runs":true},"xcode":{"runs":false}}}`,
			results: []domainsummary.KeyValue{
				{Key: "cargo", Value: "failure"}, {Key: "npm", Value: "success"},
			},
			want: [3]string{"skipped", "success", "skipped"}, result: "success", ran: true,
		},
		{
			name: "only_last_enabled",
			plan: `{"version":1,"stage":"build","targets":{"cargo":{"runs":false},"npm":{"runs":false},"xcode":{"runs":true}}}`,
			results: []domainsummary.KeyValue{
				{Key: "cargo", Value: "failure"}, {Key: "npm", Value: "cancelled"}, {Key: "xcode", Value: "success"},
			},
			want: [3]string{"skipped", "skipped", "success"}, result: "success", ran: true,
		},
	} {
		for _, source := range []string{"explicit", "map", "store"} {
			t.Run(tc.name+"/"+source, func(t *testing.T) {
				t.Parallel()
				in, jobs := resultMatrixInput(t, source, tc.plan, tc.results)
				out, manifest := fakeoutputsink.New(t), fakemanifestsink.New(t)

				env, err := appsummary.StageResult(t.Context(), out, manifest, jobs, in)
				if err != nil {
					t.Fatal(err)
				}

				assertResultMatrixPublication(t, env, out, manifest, tc.want, tc.result, tc.ran)
				jobs.assertCalls(source)
			})
		}
	}
}

func TestStageResult_MissingResultMatrix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		missing string
		results []domainsummary.KeyValue
		want    [3]string
	}{
		{
			missing: "npm",
			results: []domainsummary.KeyValue{{Key: "cargo", Value: "success"}, {Key: "xcode", Value: "success"}},
			want:    [3]string{"success", "failure", "success"},
		},
		{
			missing: "xcode",
			results: []domainsummary.KeyValue{{Key: "cargo", Value: "success"}, {Key: "npm", Value: "success"}},
			want:    [3]string{"success", "success", "failure"},
		},
	} {
		for _, source := range []string{"explicit", "map", "store"} {
			t.Run(tc.missing+"/"+source, func(t *testing.T) {
				t.Parallel()

				in, jobs := resultMatrixInput(t, source, resultMatrixPlan, tc.results)
				if source == "explicit" {
					// Both lower sources have the missing target, but neither may
					// complete an authoritative, incomplete explicit result set.
					in.JobResultsMap = `{"cargo":{"result":"success"},"npm":{"result":"success"},"xcode":{"result":"success"}}`

					for _, name := range []string{"cargo", "npm", "xcode"} {
						jobs.Seed(name, fmt.Sprintf(`{"version":1,"job":%q,"result":"success"}`, name))
					}

					in.JSONOutputKey = "artifacts-json"
					in.JSONFields = []domainsummary.KeyValue{{Key: "package", Value: "synthetic"}}
				}

				out, manifest := fakeoutputsink.New(t), fakemanifestsink.New(t)

				env, err := appsummary.StageResult(t.Context(), out, manifest, jobs, in)
				if source == "explicit" {
					assertResultMatrixRefusal(t, env, err, errs.ErrUsage, fmt.Sprintf("stage target %q runs but no result was provided", tc.missing), out, manifest)
				} else {
					if err != nil {
						t.Fatal(err)
					}

					assertResultMatrixPublication(t, env, out, manifest, tc.want, "failure", true)
				}

				jobs.assertCalls(source)
			})
		}
	}
}

func TestStageResult_ResultSourcePriorityMatrix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		explicit      []domainsummary.KeyValue
		jobMap        string
		storeStatus   string
		poisonStore   bool
		selected      string
		want          [3]string
		result        string
		wantErr       error
		wantErrorText string
	}{
		{
			name: "explicit_over_conflicting_map_and_store",
			explicit: []domainsummary.KeyValue{
				{Key: "cargo", Value: "success"}, {Key: "npm", Value: "cancelled"}, {Key: "xcode", Value: "success"},
			},
			jobMap:      `{"cargo":{"result":"failure"},"npm":{"result":"failure"},"xcode":{"result":"failure"}}`,
			storeStatus: "failure", selected: "explicit",
			want: [3]string{"success", "cancelled", "success"}, result: "cancelled",
		},
		{
			name: "explicit_ignores_malformed_map_and_poisoned_store",
			explicit: []domainsummary.KeyValue{
				{Key: "cargo", Value: "success"}, {Key: "npm", Value: "success"}, {Key: "xcode", Value: "success"},
			},
			jobMap: "not-json", poisonStore: true, selected: "explicit",
			want: [3]string{"success", "success", "success"}, result: "success",
		},
		{
			name:        "map_over_conflicting_store",
			jobMap:      `{"cargo":{"result":"success"},"npm":{"result":"cancelled"},"xcode":{"result":"success"}}`,
			storeStatus: "failure", selected: "map",
			want: [3]string{"success", "cancelled", "success"}, result: "cancelled",
		},
		{
			name:        "map_ignores_poisoned_store",
			jobMap:      `{"cargo":{"result":"success"},"npm":{"result":"success"},"xcode":{"result":"success"}}`,
			poisonStore: true, selected: "map",
			want: [3]string{"success", "success", "success"}, result: "success",
		},
		{
			name:   "empty_object_is_authoritative",
			jobMap: `{}`, storeStatus: "success", selected: "map",
			want: [3]string{"failure", "failure", "failure"}, result: "failure",
		},
		{
			name:   "whitespace_map_uses_store",
			jobMap: " \t\r\n ", storeStatus: "success", selected: "store",
			want: [3]string{"success", "success", "success"}, result: "success",
		},
		{
			name:   "malformed_later_map_entry_does_not_fallback",
			jobMap: `{"cargo":{"result":"success"},"npm":`, storeStatus: "success", selected: "map",
			wantErr: errs.ErrMalformedInput, wantErrorText: "read job result",
		},
		{
			name:     "explicit_missing_with_empty_lower_sources_is_usage_not_verdict",
			explicit: []domainsummary.KeyValue{{Key: "cargo", Value: "success"}, {Key: "npm", Value: "success"}},
			jobMap:   `{}`, selected: "explicit",
			wantErr: errs.ErrUsage, wantErrorText: `stage target "xcode" runs but no result was provided`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			jobs := newResultMatrixStore(t)

			jobs.poisonCollect = tc.poisonStore
			if tc.storeStatus != "" {
				for _, name := range []string{"cargo", "npm", "xcode"} {
					jobs.Seed(name, fmt.Sprintf(`{"version":1,"job":%q,"result":%q}`, name, tc.storeStatus))
				}
			}

			if tc.poisonStore {
				jobs.Seed("cargo", "not-json")
			}

			in := appsummary.StageResultInput{StagePlanJSON: resultMatrixPlan, Results: tc.explicit, JobResultsMap: tc.jobMap}
			if tc.wantErr != nil {
				in.JSONOutputKey = "artifacts-json"
				in.JSONFields = []domainsummary.KeyValue{{Key: "package", Value: "synthetic"}}
			}

			out, manifest := fakeoutputsink.New(t), fakemanifestsink.New(t)

			env, err := appsummary.StageResult(t.Context(), out, manifest, jobs, in)
			if tc.wantErr != nil {
				assertResultMatrixRefusal(t, env, err, tc.wantErr, tc.wantErrorText, out, manifest)
			} else {
				if err != nil {
					t.Fatal(err)
				}

				assertResultMatrixPublication(t, env, out, manifest, tc.want, tc.result, true)
			}

			jobs.assertCalls(tc.selected)
		})
	}
}

func TestJobResult_StatusNormalizationMatrix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, status, want string }{
		{"success", "success", "success"},
		{"failure", "failure", "failure"},
		{"cancelled", "cancelled", "cancelled"},
		{"skipped", "skipped", "skipped"},
		{"failed", "failed", "failure"},
		{"canceled", "canceled", "cancelled"},
		{"unknown", "in_progress", "failure"},
		{"empty", "", "failure"},
		{"padded_success", " \tsuccess\r\n ", "success"},
		{"padded_failure", " \tfailure\r\n ", "failure"},
		{"padded_cancelled", " \tcancelled\r\n ", "cancelled"},
		{"padded_skipped", " \tskipped\r\n ", "skipped"},
		{"padded_failed", " \tfailed\r\n ", "failure"},
		{"padded_canceled", " \tcanceled\r\n ", "cancelled"},
		{"padded_unknown", " \tin_progress\r\n ", "failure"},
		{"whitespace", " \t\r\n ", "failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			jobs := newResultMatrixStore(t)

			jobs.poisonCollect = true
			if err := appsummary.JobResult(t.Context(), jobs, appsummary.JobResultInput{Job: "matrix-job", Status: tc.status}); err != nil {
				t.Fatal(err)
			}

			wantBody := fmt.Sprintf(`{"version":1,"job":"matrix-job","result":%q}`, tc.want)
			if jobs.writeCalls != 1 || jobs.collectCalls != 0 || jobs.writtenJob != "matrix-job" || jobs.writtenBody != wantBody {
				t.Errorf("WriteJob calls/job/body = %d/%q/%q; CollectJobs = %d; want 1/matrix-job/%q and no collection", jobs.writeCalls, jobs.writtenJob, jobs.writtenBody, jobs.collectCalls, wantBody)
			}

			if got := jobs.Body("matrix-job"); got != wantBody {
				t.Errorf("stored body = %q, want %q", got, wantBody)
			}
		})
	}
}

func TestStageResult_MapStatusNormalizationMatrix(t *testing.T) {
	t.Parallel()
	// Unlike the writer, the raw map feed normalizes without trimming.
	for _, tc := range []struct{ name, status, want string }{
		{"failed", "failed", "failure"},
		{"canceled", "canceled", "cancelled"},
		{"unknown", "in_progress", "failure"},
		{"empty", "", "failure"},
		{"padded_success", " \tsuccess\r\n ", "failure"},
		{"padded_failure", " \tfailure\r\n ", "failure"},
		{"padded_cancelled", " \tcancelled\r\n ", "failure"},
		{"padded_failed", " \tfailed\r\n ", "failure"},
		{"padded_canceled", " \tcanceled\r\n ", "failure"},
		{"padded_unknown", " \tin_progress\r\n ", "failure"},
		{"whitespace", " \t\r\n ", "failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in, jobs := resultMatrixInput(t, "map", resultMatrixPlan, []domainsummary.KeyValue{
				{Key: "cargo", Value: "success"}, {Key: "npm", Value: "success"}, {Key: "xcode", Value: tc.status},
			})
			out, manifest := fakeoutputsink.New(t), fakemanifestsink.New(t)

			env, err := appsummary.StageResult(t.Context(), out, manifest, jobs, in)
			if err != nil {
				t.Fatal(err)
			}

			assertResultMatrixPublication(t, env, out, manifest, [3]string{"success", "success", tc.want}, tc.want, true)
			jobs.assertCalls("map")
		})
	}
}

func TestStageResult_ExplicitStatusRefusalMatrix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, status string }{
		{"failed", "failed"},
		{"canceled", "canceled"},
		{"unknown", "in_progress"},
		{"empty", ""},
		{"skipped_running", "skipped"},
		{"padded_success", " \tsuccess\r\n "},
		{"padded_failure", " \tfailure\r\n "},
		{"padded_cancelled", " \tcancelled\r\n "},
		{"padded_skipped", " \tskipped\r\n "},
		{"padded_failed", " \tfailed\r\n "},
		{"padded_canceled", " \tcanceled\r\n "},
		{"padded_unknown", " \tin_progress\r\n "},
		{"whitespace", " \t\r\n "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in, jobs := resultMatrixInput(t, "explicit", resultMatrixPlan, []domainsummary.KeyValue{
				{Key: "cargo", Value: "success"}, {Key: "npm", Value: "success"}, {Key: "xcode", Value: tc.status},
			})
			in.JSONOutputKey = "artifacts-json"
			in.JSONFields = []domainsummary.KeyValue{{Key: "package", Value: "synthetic"}}
			out, manifest := fakeoutputsink.New(t), fakemanifestsink.New(t)
			env, err := appsummary.StageResult(t.Context(), out, manifest, jobs, in)

			wantText := fmt.Sprintf("stage target \"xcode\": invalid result %q", tc.status)
			if tc.status == "skipped" {
				wantText = `stage target "xcode": planned target returned skipped`
			}

			assertResultMatrixRefusal(t, env, err, errs.ErrUsage, wantText, out, manifest)
			jobs.assertCalls("explicit")
		})
	}
}

func resultMatrixInput(t *testing.T, source, plan string, values []domainsummary.KeyValue) (appsummary.StageResultInput, *resultMatrixStore) {
	t.Helper()

	in := appsummary.StageResultInput{StagePlanJSON: plan}
	jobs := newResultMatrixStore(t)

	jobs.poisonCollect = source != "store"
	switch source {
	case "explicit":
		in.Results = values
		in.JobResultsMap = "unused malformed map"
	case "map":
		records := make(map[string]map[string]string, len(values))
		for _, value := range values {
			records[value.Key] = map[string]string{"result": value.Value}
		}

		body, err := json.Marshal(records)
		if err != nil {
			t.Fatal(err)
		}

		in.JobResultsMap = string(body)
	case "store":
		for _, value := range values {
			jobs.Seed(value.Key, fmt.Sprintf(`{"version":1,"job":%q,"result":%q}`, value.Key, value.Value))
		}
	default:
		t.Fatalf("unknown matrix source %q", source)
	}

	return in, jobs
}

func assertResultMatrixPublication(t *testing.T, env *domainsummary.StageResultEnvelope, out *fakeoutputsink.Sink, manifest *fakemanifestsink.Sink, targets [3]string, result string, ran bool) {
	t.Helper()

	if env == nil {
		t.Fatal("successful StageResult returned a nil envelope")
	}

	wantTargets := []domainsummary.Target{
		{Name: "cargo", Result: domainsummary.Result(targets[0])},
		{Name: "npm", Result: domainsummary.Result(targets[1])},
		{Name: "xcode", Result: domainsummary.Result(targets[2])},
	}
	if env.Stage != "build" || string(env.Result) != result || env.Ran != ran || len(env.Extras) != 0 || !slices.Equal(env.Targets, wantTargets) {
		t.Errorf("envelope = %#v, want build/%s/ran=%t, no extras, targets %#v", env, result, ran, wantTargets)
	}
	// Expectations are literal table verdicts, never the production aggregator,
	// parser, envelope builder, or the aggregate-computing stageResultJSON helper.
	wantJSON := fmt.Sprintf(`{"version":1,"stage":"build","result":%q,"ran":%t,"targets":{"cargo":%q,"npm":%q,"xcode":%q}}`, result, ran, targets[0], targets[1], targets[2])

	body, err := env.MarshalJSON()
	if err != nil || string(body) != wantJSON {
		t.Errorf("returned JSON = %q, error = %v; want %q", body, err, wantJSON)
	}

	if got := manifest.Stages(); !slices.Equal(got, []string{"build"}) {
		t.Errorf("manifest stages = %q, want only build", got)
	}

	if got := manifest.Body("build"); got != wantJSON {
		t.Errorf("manifest JSON = %q, want %q", got, wantJSON)
	}

	wantOutputs := map[string]string{"stage-ran": strconv.FormatBool(ran), "stage-result": result, "result-json": wantJSON}
	if got := out.AllScalar(); !reflect.DeepEqual(got, wantOutputs) {
		t.Errorf("scalar outputs = %#v, want %#v", got, wantOutputs)
	}

	if got := out.Keys(); !slices.Equal(got, []string{"result-json", "stage-ran", "stage-result"}) {
		t.Errorf("output keys = %q, want exactly result-json/stage-ran/stage-result", got)
	}

	for _, key := range out.Keys() {
		if got := out.Multiline(key); got != nil {
			t.Errorf("unexpected multiline output %q = %q", key, got)
		}
	}

	if got := out.CloseCount(); got != 0 {
		t.Errorf("output Close calls = %d, want 0", got)
	}
}

func assertResultMatrixRefusal(t *testing.T, env *domainsummary.StageResultEnvelope, err, wantErr error, wantText string, out *fakeoutputsink.Sink, manifest *fakemanifestsink.Sink) {
	t.Helper()

	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want %v", err, wantErr)
	}

	if err == nil || !strings.Contains(err.Error(), wantText) {
		t.Errorf("error = %v, want context %q", err, wantText)
	}

	if env != nil {
		t.Errorf("refusal returned nonnil envelope: %#v", env)
	}

	if got := out.Keys(); len(got) != 0 {
		t.Errorf("refusal published output keys: %q", got)
	}

	if got := out.CloseCount(); got != 0 {
		t.Errorf("refusal closed output %d times", got)
	}

	if got := manifest.Stages(); len(got) != 0 {
		t.Errorf("refusal published manifests: %q", got)
	}
}

type resultMatrixStore struct {
	*fakejobresultstore.Store
	t             *testing.T
	poisonCollect bool
	collectCalls  int
	writeCalls    int
	writtenJob    string
	writtenBody   string
}

func newResultMatrixStore(t *testing.T) *resultMatrixStore {
	t.Helper()

	return &resultMatrixStore{Store: fakejobresultstore.New(t), t: t}
}

func (s *resultMatrixStore) CollectJobs(ctx context.Context) ([][]byte, error) {
	s.t.Helper()

	s.collectCalls++
	if ctx != s.t.Context() {
		s.t.Error("CollectJobs did not receive the caller's exact context")
	}

	if s.poisonCollect {
		return nil, errors.New("unused result store was collected") //nolint:err113 // Poison for a source that must never be consulted.
	}

	return s.Store.CollectJobs(ctx)
}

func (s *resultMatrixStore) WriteJob(ctx context.Context, job string, body interface{ MarshalJSON() ([]byte, error) }) error {
	s.t.Helper()

	s.writeCalls++
	if ctx != s.t.Context() {
		s.t.Error("WriteJob did not receive the caller's exact context")
	}

	raw, err := body.MarshalJSON()
	if err != nil {
		s.t.Fatal(err)
	}

	s.writtenJob, s.writtenBody = job, string(raw)

	return s.Store.WriteJob(ctx, job, body)
}

func (s *resultMatrixStore) assertCalls(source string) {
	s.t.Helper()

	wantCollect := 0
	if source == "store" {
		wantCollect = 1
	}

	if s.collectCalls != wantCollect || s.writeCalls != 0 {
		s.t.Errorf("store calls: CollectJobs = %d, WriteJob = %d; want %d, 0", s.collectCalls, s.writeCalls, wantCollect)
	}
}
