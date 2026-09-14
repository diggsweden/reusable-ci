// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	domainsummary "github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakejobresultstore"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakemanifestsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

func TestStageIntegrityBoundary_ReservedFieldsAndRoundTrip(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"version", "stage", "result", "ran", "targets", "stage-result", "stage-ran", "result-json", "custom"} {
		out := fakeoutputsink.New(t)
		manifest := fakemanifestsink.New(t)

		in := appsummary.StageResultInput{StagePlanJSON: `{"version":1,"stage":"build","targets":{"npm":{"runs":true}}}`, Results: []domainsummary.KeyValue{{Key: "npm", Value: "success"}}}
		if strings.Contains(key, "-") {
			in.JSONOutputKey = key
		} else {
			in.Extras = []domainsummary.KeyValue{{Key: key, Value: "value"}}
		}

		env, err := appsummary.StageResult(t.Context(), out, manifest, fakejobresultstore.New(t), in)
		if key != "custom" {
			if !errors.Is(err, errs.ErrUsage) || env != nil || len(out.Keys()) != 0 || len(manifest.Stages()) != 0 {
				t.Fatalf("key=%s err=%v outputs=%v", key, err, out.AllScalar())
			}

			continue
		}

		if err != nil {
			t.Fatal(err)
		}

		body := manifest.Body("build")

		parsed, err := domainsummary.ParseStageResultEnvelope(body)
		if err != nil {
			t.Fatalf("producer emitted unreadable envelope: %v", err)
		}

		if parsed.Result != domainsummary.ResultSuccess || strings.Count(body, `"version"`) != 1 || out.Single("stage-result") != "success" {
			t.Fatalf("envelope=%s", body)
		}
	}
}

func TestJobAliasBoundary_ConflictsNeverBecomeSuccess(t *testing.T) { //nolint:gocognit // compare both spellings, permutations and map/store sources with the same independent failure expectation.
	t.Parallel()

	for _, target := range []string{"gradle_android", "gradle-android"} {
		for _, reverse := range []bool{false, true} {
			for _, storeMode := range []bool{false, true} {
				jobs := []domainsummary.JobResultEnvelope{{Job: "gradle_android", Result: domainsummary.ResultSuccess}, {Job: "gradle-android", Result: domainsummary.ResultFailure}}
				if reverse {
					jobs[0], jobs[1] = jobs[1], jobs[0]
				}

				store := fakejobresultstore.New(t)

				for i, job := range jobs {
					body, err := json.Marshal(job)
					if err != nil {
						t.Fatal(err)
					}

					store.Seed(strconv.Itoa(i), string(body))
				}

				in := appsummary.StageResultInput{StagePlanJSON: fmt.Sprintf(`{"version":1,"stage":"build","targets":{%q:{"runs":true}}}`, target)}
				if !storeMode {
					in.JobResultsMap = `{"gradle_android":{"result":"success"},"gradle-android":{"result":"failure"}}`
				}

				out := fakeoutputsink.New(t)

				env, err := appsummary.StageResult(t.Context(), out, fakemanifestsink.New(t), store, in)
				if err != nil || env.Result != domainsummary.ResultFailure || out.Single("stage-result") != "failure" {
					t.Fatalf("target=%s reverse=%v store=%v err=%v env=%+v", target, reverse, storeMode, err, env)
				}
			}
		}
	}

	for _, jobs := range [][]domainsummary.JobResultEnvelope{{{Job: "x", Result: domainsummary.ResultFailure}, {Job: "x", Result: domainsummary.ResultSuccess}}, {{Job: "x", Result: domainsummary.ResultSuccess}, {Job: "x", Result: domainsummary.ResultFailure}}} {
		if got := domainsummary.ResolveTargetResults([]string{"x"}, jobs)["x"]; got != domainsummary.ResultFailure {
			t.Fatalf("duplicate exact identity=%s", got)
		}
	}

	for _, body := range []string{`{"x":{"result":"failure"},"x":{"result":"success"}}`, `{"x":{"result":"success"},"x":{"result":"failure"}}`} {
		records, err := domainsummary.ParseJobResultsMap([]byte(body))
		if err != nil {
			t.Fatal(err)
		}

		if got := domainsummary.ResolveTargetResults([]string{"x"}, records)["x"]; got != domainsummary.ResultFailure {
			t.Fatalf("duplicate map key hid failure: %s", got)
		}
	}

	for _, jobs := range [][]domainsummary.JobResultEnvelope{
		{{Job: "gradle_android", Result: domainsummary.ResultFailure}, {Job: "gradle-android", Result: domainsummary.ResultSuccess}},
		{{Job: "gradle-android", Result: domainsummary.ResultSuccess}, {Job: "gradle_android", Result: domainsummary.ResultFailure}},
	} {
		if got := domainsummary.ResolveTargetResults([]string{"gradle_android"}, jobs)["gradle_android"]; got != domainsummary.ResultFailure {
			t.Fatalf("alias normalization hid a failure: %s", got)
		}
	}
}

func TestQualityTruthBoundary_UnconfirmedCannotPass(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"lint|true|", "lint|true|unknown", "lint|true|success|extra", "lint|yes|success", "lint|true|skipped", "malformed"} {
		sink := &fakeSummarySink{}
		if err := appsummary.QualityCheckStatus(t.Context(), sink, appsummary.ParseQualityChecks([]string{input})); err != nil {
			t.Fatal(err)
		}

		if strings.Contains(sink.buf.String(), "All enabled checks passed") || strings.Contains(sink.buf.String(), "Disabled") {
			t.Fatalf("unconfirmed input=%q summary=%s", input, &sink.buf)
		}
	}

	for _, input := range [][]string{nil, {"lint|true|success"}, {"lint|false|failure"}} {
		sink := &fakeSummarySink{}
		if err := appsummary.QualityCheckStatus(t.Context(), sink, appsummary.ParseQualityChecks(input)); err != nil {
			t.Fatal(err)
		}

		if !strings.Contains(sink.buf.String(), "All enabled checks passed") {
			t.Fatalf("intentional empty/disabled/success policy changed: %s", &sink.buf)
		}
	}
}

func TestPrerequisiteTruthBoundary_NoUnsupportedPasses(t *testing.T) {
	t.Parallel()

	for _, body := range []string{"", "object fake\ntype commit\ntag v1.0.0\n", "-----BEGIN PGP SIGNATURE-----"} {
		for _, failedRead := range []bool{false, true} {
			git := &fakeGitInfo{tagBody: body}
			if failedRead {
				git.tagErr = errs.ErrMissingInput
			}

			sink := &fakeSummarySink{}
			if err := appsummary.Prerequisites(t.Context(), sink, git, appsummary.PrerequisitesSummaryInput{TagName: "v1.0.0", RefType: provider.RefTypeTag, JobStatus: domainsummary.ResultFailure, Now: fixedNow()}); err != nil {
				t.Fatal(err)
			}

			if strings.Contains(sink.buf.String(), "| Tag Type | \u2713 Pass") || strings.Contains(sink.buf.String(), "| Tag Signature | \u2713 Pass") {
				t.Fatalf("unsupported verification: %s", &sink.buf)
			}

			if failedRead && !strings.Contains(sink.buf.String(), "Tag Signature:** Unavailable") {
				t.Fatalf("missing evidence reported as fact: %s", &sink.buf)
			}
		}
	}

	for _, user := range []bool{false, true} {
		for _, password := range []bool{false, true} {
			sink := &fakeSummarySink{}
			if err := appsummary.Prerequisites(t.Context(), sink, nil, appsummary.PrerequisitesSummaryInput{PublishTo: "maven-central", HasMavenCentralUsername: user, HasMavenCentralPassword: password, Now: fixedNow()}); err != nil {
				t.Fatal(err)
			}

			pass := strings.Contains(sink.buf.String(), "| Maven Central | \u2713 Pass | Credentials configured |")
			if pass != (user && password) {
				t.Fatalf("user=%v password=%v summary=%s", user, password, &sink.buf)
			}
		}
	}
}

func TestBuildSBOMEvidenceBoundary_OnlyRegularFiles(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"missing", "regular", "directory", "dangling link"} {
		root := t.TempDir()

		target := filepath.Join(root, "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}

		path := filepath.Join(target, "bom.json")

		switch kind {
		case "regular":
			if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		case "directory":
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		case "dangling link":
			if err := os.Symlink("missing", path); err != nil {
				t.Fatal(err)
			}
		}

		sink := &fakeSummarySink{}
		if err := appsummary.BuildSBOMStatus(t.Context(), sink, appsummary.BuildSBOMStatusInput{Ecosystem: "maven", Outcome: "success", WorkDir: root}); err != nil {
			t.Fatal(err)
		}

		if strings.Contains(sink.buf.String(), "`"+path+"`") != (kind == "regular") {
			t.Fatalf("kind=%s summary=%s", kind, &sink.buf)
		}
	}
}
