// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	domainsummary "github.com/diggsweden/reusable-ci/internal/domain/summary"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakemanifestsink"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
)

func TestStageResult_GenericPlan(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)

	_, err := appsummary.StageResult(context.Background(), out, mf, appsummary.StageResultInput{
		StagePlanJSON: `{"version":1,"stage":"build","targets":{"npm":{"runs":true},"maven":{"runs":false}}}`,
		Results: []domainsummary.KeyValue{
			{Key: "npm", Value: "success"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			{Key: "maven", Value: "failure"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		},
		Extras: []domainsummary.KeyValue{{Key: "project_type", Value: "npm"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := out.Single("stage-result"); got != "success" {
		t.Errorf("stage-result = %q", got)
	}

	body := mf.Body("build")
	for _, want := range []string{`"project_type":"npm"`, `"maven":"skipped"`, `"npm":"success"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}

func TestStageResult_EmitsJSONOutput(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)

	_, err := appsummary.StageResult(context.Background(), out, mf, appsummary.StageResultInput{
		StagePlanJSON: `{"version":1,"stage":"dev-publish","targets":{"npm":{"runs":true}}}`,
		Results:       []domainsummary.KeyValue{{Key: "npm", Value: "success"}},
		JSONOutputKey: "artifacts-json",
		JSONFields: []domainsummary.KeyValue{
			{Key: "npm_package_name", Value: "pkg"},
			{Key: "npm_package_version", Value: "1.2.3"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			{Key: "npm_publish_status", Value: "published"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{`"npm_package_name":"pkg"`, `"npm_package_version":"1.2.3"`, `"npm_publish_status":"published"`} {
		if got := out.Single("artifacts-json"); !strings.Contains(got, want) {
			t.Errorf("artifacts-json missing %q: %s", want, got)
		}
	}
}

func TestStageResult_MissingResultForRunningTargetFails(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)

	_, err := appsummary.StageResult(context.Background(), out, mf, appsummary.StageResultInput{
		StagePlanJSON: `{"version":1,"stage":"build","targets":{"maven":{"runs":true}}}`, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})
	if err == nil || !strings.Contains(err.Error(), `stage target "maven" runs but no result was provided`) {
		t.Fatalf("err = %v", err)
	}
}

func TestStageResult_RejectsInvalidRunningResults(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		result string
		want   string
	}{
		{name: "skipped", result: "skipped", want: "planned target returned skipped"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{name: "unknown", result: "in_progress", want: `invalid result "in_progress"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := fakeoutputsink.New(t)
			mf := fakemanifestsink.New(t)

			_, err := appsummary.StageResult(context.Background(), out, mf, appsummary.StageResultInput{
				StagePlanJSON: `{"version":1,"stage":"build","targets":{"maven":{"runs":true}}}`,
				Results:       []domainsummary.KeyValue{{Key: "maven", Value: tc.result}},
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestStageResult_RejectsInvalidPlanAndKeys(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   appsummary.StageResultInput
		want string
	}{
		{
			name: "unsupported version",
			in:   appsummary.StageResultInput{StagePlanJSON: `{"version":2,"stage":"build","targets":{"maven":{"runs":false}}}`},
			want: "unsupported version 2",
		},
		{
			name: "invalid stage",
			in:   appsummary.StageResultInput{StagePlanJSON: `{"version":1,"stage":"../build","targets":{"maven":{"runs":false}}}`},
			want: "invalid stage name",
		},
		{
			name: "duplicate result",
			in: appsummary.StageResultInput{
				StagePlanJSON: `{"version":1,"stage":"build","targets":{"maven":{"runs":true}}}`,
				Results: []domainsummary.KeyValue{
					{Key: "maven", Value: "success"},
					{Key: "maven", Value: "failure"},
				},
			},
			want: `duplicate result key "maven"`,
		},
		{
			name: "unknown result target",
			in: appsummary.StageResultInput{
				StagePlanJSON: `{"version":1,"stage":"build","targets":{"maven":{"runs":false}}}`,
				Results:       []domainsummary.KeyValue{{Key: "npm", Value: "success"}},
			},
			want: `result provided for unknown stage target "npm"`,
		},
		{
			name: "reserved extra",
			in: appsummary.StageResultInput{
				StagePlanJSON: `{"version":1,"stage":"build","targets":{"maven":{"runs":false}}}`,
				Extras:        []domainsummary.KeyValue{{Key: "result", Value: "success"}},
			},
			want: `extra key "result" is reserved`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := fakeoutputsink.New(t)
			mf := fakemanifestsink.New(t)

			_, err := appsummary.StageResult(context.Background(), out, mf, tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestStageResult_NeedsJSON(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)

	_, err := appsummary.StageResult(context.Background(), out, mf, appsummary.StageResultInput{
		StagePlanJSON: `{"version":1,"stage":"build","targets":{"maven":{"runs":true},"gradle_android":{"runs":true},"npm":{"runs":false}}}`,
		NeedsJSON: `{
			"maven": {"result": "success", "outputs": {}},
			"gradle-android": {"result": "failure", "outputs": {}},
			"npm": {"result": "skipped", "outputs": {}},
			"some-unrelated-status-job": {"result": "success", "outputs": {}}
		}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := out.Single("stage-result"); got != "failure" {
		t.Errorf("stage-result = %q", got)
	}

	body := mf.Body("build")
	for _, want := range []string{`"maven":"success"`, `"gradle_android":"failure"`, `"npm":"skipped"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}

func TestStageResult_NeedsJSON_RejectsCombinedWithResults(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)

	_, err := appsummary.StageResult(context.Background(), out, mf, appsummary.StageResultInput{
		StagePlanJSON: `{"version":1,"stage":"build","targets":{"maven":{"runs":true}}}`,
		Results:       []domainsummary.KeyValue{{Key: "maven", Value: "success"}},
		NeedsJSON:     `{"maven":{"result":"success"}}`,
	})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("err = %v", err)
	}
}

func TestStageResult_NeedsJSON_MissingResultForRunningTargetFails(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)

	_, err := appsummary.StageResult(context.Background(), out, mf, appsummary.StageResultInput{
		StagePlanJSON: `{"version":1,"stage":"build","targets":{"maven":{"runs":true}}}`,
		NeedsJSON:     `{"someone-else":{"result":"success"}}`,
	})
	if err == nil || !strings.Contains(err.Error(), `stage target "maven" runs but no result was provided`) {
		t.Fatalf("err = %v", err)
	}
}

func TestStageResult_NeedsJSON_InvalidPayload(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)

	_, err := appsummary.StageResult(context.Background(), out, mf, appsummary.StageResultInput{
		StagePlanJSON: `{"version":1,"stage":"build","targets":{"maven":{"runs":true}}}`,
		NeedsJSON:     `not-json`,
	})
	if err == nil || !strings.Contains(err.Error(), "parse needs-json") {
		t.Fatalf("err = %v", err)
	}
}

func TestStageResult_FailurePriorityAndSkippedTargets(t *testing.T) {
	t.Parallel()
	out := fakeoutputsink.New(t)
	mf := fakemanifestsink.New(t)

	_, err := appsummary.StageResult(context.Background(), out, mf, appsummary.StageResultInput{
		StagePlanJSON: `{"version":1,"stage":"publish","targets":{"containers":{"runs":true},"cargo":{"runs":true},"npm":{"runs":false}}}`,
		Results: []domainsummary.KeyValue{
			{Key: "containers", Value: "cancelled"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			{Key: "cargo", Value: "failure"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			{Key: "npm", Value: "failure"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := out.Single("stage-result"); got != "failure" {
		t.Errorf("stage-result = %q", got)
	}

	body := mf.Body("publish")
	for _, want := range []string{`"cargo":"failure"`, `"containers":"cancelled"`, `"npm":"skipped"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}
