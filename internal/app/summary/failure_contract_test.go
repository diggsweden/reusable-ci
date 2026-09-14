// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	domainsummary "github.com/diggsweden/reusable-ci/v3/internal/domain/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakejobresultstore"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakemanifestsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

//nolint:gocognit // Keep each append contract and the snapshot positive control in one table.
func TestSummary_AppendFailureContract(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		run        func(context.Context, ci.SummarySink, *strings.Builder) error
		wantReport []string
		wantBanner string
	}{
		{
			name: "Prerequisites",
			run: func(ctx context.Context, sink ci.SummarySink, _ *strings.Builder) error {
				return appsummary.Prerequisites(ctx, sink, nil, appsummary.PrerequisitesSummaryInput{
					TagName: "v2.3.4", ProjectTypes: "npm", JobStatus: "success", Now: fixedNow(),
				})
			},
			wantReport: []string{"Release Prerequisites Validation Report\n", "- **Tag:** `v2.3.4`\n", "All selected prerequisite checks passed!\n", "*Generated at: 2026-05-10 14:30:00 UTC*\n"},
		},
		{
			name: "PRSummary",
			run: func(ctx context.Context, sink ci.SummarySink, _ *strings.Builder) error {
				return appsummary.PRSummary(ctx, sink, appsummary.PRSummaryInput{
					Branch: "feature/sink-contract", Now: fixedNow(),
					QualityStageResultJSON: `{"version":1,"stage":"pr-quality","result":"success","ran":true,"targets":{"nanolinter":"success"}}`,
				})
			},
			wantReport: []string{"# Pull Request Summary\n", "| **Branch** | `feature/sink-contract` |\n", "| Nanolinter | \u2713 |\n", "| **Checked At** | 2026-05-10 14:30:00 UTC |\n"},
		},
		{
			name: "SnapshotReleaseSummary",
			run: func(ctx context.Context, sink ci.SummarySink, log *strings.Builder) error {
				return appsummary.SnapshotReleaseSummary(ctx, sink, log, appsummary.SnapshotReleaseSummaryInput{
					ProjectType: projecttype.NPM, ReleaseRef: "dev/sink-contract", Now: fixedNow(),
					BuildStageJSON: `{"version":1,"stage":"dev-build","result":"success","ran":true,"targets":{"npm":"success"}}`,
				})
			},
			wantReport: []string{"# Dev Release Summary\n", "| **Branch** | `dev/sink-contract` |\n", "| Build NPM | \u2713 |\n", "| **Built At** | 2026-05-10 14:30:00 UTC |\n"},
			wantBanner: "\u2713 Dev release summary generated successfully\n",
		},
		{
			name: "ReleaseSummary",
			run: func(ctx context.Context, sink ci.SummarySink, _ *strings.Builder) error {
				return appsummary.ReleaseSummary(ctx, sink, appsummary.ReleaseSummaryInput{
					ReleaseVersion: "v3.4.5", CreateReleaseResult: "success", Now: fixedNow(),
				})
			},
			wantReport: []string{"# Release Summary\n", "| **Version** | `v3.4.5` |\n", "| Forge Release | \u2713 |\n", "| **Released At** | 2026-05-10 14:30:00 UTC |\n"},
		},
		{
			name: "QualityCheckStatus",
			run: func(ctx context.Context, sink ci.SummarySink, _ *strings.Builder) error {
				return appsummary.QualityCheckStatus(ctx, sink, []appsummary.QualityCheck{{Name: "Contract lint", Enabled: true, Result: "success"}})
			},
			wantReport: []string{"## Pull Request Check Status\n", "| Contract lint | \u2713 Pass |\n", "### \u2713 All enabled checks passed\n"},
		},
		{
			name: "SBOMCountStatus",
			run: func(ctx context.Context, sink ci.SummarySink, _ *strings.Builder) error {
				// Skipped generation reaches Append without filesystem discovery.
				return appsummary.SBOMCountStatus(ctx, sink, appsummary.SBOMCountStatusInput{Kind: "go", Outcome: "skipped"})
			},
			wantReport: []string{"### Go Build SBOM\n", "- \u2298 Generation disabled; release continues without a build SBOM\n"},
		},
		{
			name: "MavenCentralPublish",
			run: func(ctx context.Context, sink ci.SummarySink, _ *strings.Builder) error {
				return appsummary.MavenCentralPublish(ctx, sink, appsummary.MavenCentralPublishInput{Version: "4.5.6", Now: fixedNow()})
			},
			wantReport: []string{"## Published to Maven Central ", "- **Version:** 4.5.6\n", "*Published at 2026-05-10 14:30:00 UTC*\n"},
		},
		{
			name: "ForgePackagesPublish",
			run: func(ctx context.Context, sink ci.SummarySink, _ *strings.Builder) error {
				return appsummary.ForgePackagesPublish(ctx, sink, appsummary.ForgePackagesPublishInput{Repository: "synthetic/package", PackageType: "npm", RegistryName: "Contract Registry", Now: fixedNow()})
			},
			wantReport: []string{"## Published to Contract Registry ", "- **Repository:** synthetic/package\n", "*Published at 2026-05-10 14:30:00 UTC*\n"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			sentinel := errors.New(tc.name + " append sentinel") //nolint:err113 // Each row needs a distinct injected error identity.

			var (
				attempts []string
				log      strings.Builder
			)

			sink := appendFailureSink(func(gotCtx context.Context, markdown string) error {
				if gotCtx != ctx {
					t.Error("Append did not receive the caller's exact context")
				}

				attempts = append(attempts, markdown)

				return sentinel
			})

			err := tc.run(ctx, sink, &log)
			if !errors.Is(err, sentinel) || err.Error() != sentinel.Error() {
				t.Errorf("append error = %v, want unchanged sentinel %v", err, sentinel)
			}

			if len(attempts) != 1 {
				t.Fatalf("Append attempts = %d, want exactly one", len(attempts))
			}

			for _, want := range tc.wantReport {
				if !strings.Contains(attempts[0], want) {
					t.Errorf("attempted report missing %q:\n%s", want, attempts[0])
				}
			}

			if tc.wantBanner == "" {
				return
			}
			// Job-success prose in the attempted report is valid; only the
			// post-append completion announcement must be suppressed.
			if strings.Contains(log.String(), tc.wantBanner) {
				t.Errorf("completion banner after failed Append: %q", log.String())
			}

			log.Reset()

			successCalls := 0

			successSink := appendFailureSink(func(gotCtx context.Context, markdown string) error {
				successCalls++

				if gotCtx != ctx || markdown != attempts[0] {
					t.Error("successful Append changed context or report")
				}

				if strings.Contains(log.String(), tc.wantBanner) {
					t.Error("completion banner preceded successful Append")
				}

				return nil
			})
			if err := tc.run(ctx, successSink, &log); err != nil {
				t.Fatal(err)
			}

			if successCalls != 1 || strings.Count(log.String(), tc.wantBanner) != 1 {
				t.Errorf("success: Append calls = %d, want 1; completion banner missing or repeated: %q", successCalls, log.String())
			}
		})
	}
}

type appendFailureSink func(context.Context, string) error

func (sink appendFailureSink) Append(ctx context.Context, markdown string) error {
	return sink(ctx, markdown)
}

//nolint:gocognit // The six failure prefixes share one success sequence and retained-state oracle.
func TestStageResult_SinkFailureContract(t *testing.T) {
	t.Parallel()

	const (
		canonicalJSON = `{"version":1,"stage":"build","result":"success","ran":true,"project_type":"npm","targets":{"npm":"success"}}`
		optionalJSON  = `{"package_name":"synthetic-package","package_version":"2.3.4"}`
		jobJSON       = `{"version":1,"job":"npm","result":"success"}`
	)

	wantEvents := []resultSinkEvent{
		{"CollectJobs", "", ""},
		{"WriteJSON", "build", canonicalJSON},
		{"SetBool", "stage-ran", "true"},
		{"Set", "stage-result", "success"},
		{"Set", "result-json", canonicalJSON},
		{"Set", "artifacts-json", optionalJSON},
	}

	for _, tc := range []struct {
		name   string
		failAt int
		prefix string
	}{
		{"CollectJobs", 1, "collect job results: "},
		{"WriteJSON", 2, "write build manifest: "},
		{"SetBool_stage-ran", 3, ""},
		{"Set_stage-result", 4, ""},
		{"Set_result-json", 5, ""},
		{"Set_artifacts-json", 6, "set artifacts-json: "},
		{"success", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ports := newResultFailurePorts(t, tc.failAt)
			ports.jobs.Seed("npm", jobJSON)
			env, err := appsummary.StageResult(t.Context(), ports, ports, ports, appsummary.StageResultInput{
				StagePlanJSON: `{"version":1,"stage":"build","targets":{"npm":{"runs":true}}}`,
				Extras:        []domainsummary.KeyValue{{Key: "project_type", Value: "npm"}},
				JSONOutputKey: "artifacts-json",
				JSONFields: []domainsummary.KeyValue{
					{Key: "package_version", Value: "2.3.4"},
					{Key: "package_name", Value: "synthetic-package"},
				},
			})
			attempted, completed := len(wantEvents), len(wantEvents)

			if tc.failAt == 0 && (err != nil || env == nil) {
				t.Fatalf("success returned envelope = %#v, error = %v", env, err)
			}

			if tc.failAt == 0 {
				body, marshalErr := env.MarshalJSON()
				if marshalErr != nil || string(body) != canonicalJSON {
					t.Errorf("returned envelope = %s, error = %v; want %s", body, marshalErr, canonicalJSON)
				}
			}

			if tc.failAt != 0 {
				attempted, completed = tc.failAt, tc.failAt-1

				if env != nil {
					t.Errorf("failure returned nonnil envelope: %#v", env)
				}

				if !errors.Is(err, ports.sentinel) || err.Error() != tc.prefix+ports.sentinel.Error() {
					t.Errorf("error = %v, want identity and text %q", err, tc.prefix+ports.sentinel.Error())
				}
			}

			if !slices.Equal(ports.events, wantEvents[:attempted]) {
				t.Errorf("attempts = %#v, want exact prefix %#v", ports.events, wantEvents[:attempted])
			}
			// These fakes reject the failed operation before storing it. Real
			// sinks may partially write; earlier successful writes are not rolled
			// back, even when only the optional JSON output fails.
			wantManifest := ""
			if completed >= 2 {
				wantManifest = canonicalJSON
			}

			if got := ports.manifest.Body("build"); got != wantManifest {
				t.Errorf("retained manifest = %q, want %q", got, wantManifest)
			}

			wantScalars := map[string]string{}
			for _, event := range wantEvents[2:max(2, completed)] {
				wantScalars[event.key] = event.body
			}

			if got := ports.out.AllScalar(); !reflect.DeepEqual(got, wantScalars) {
				t.Errorf("retained outputs = %#v, want %#v", got, wantScalars)
			}

			if got := ports.jobs.Body("npm"); got != jobJSON {
				t.Errorf("collected job changed: %q", got)
			}
		})
	}
}

func TestJobResult_WriteFailureContract(t *testing.T) {
	t.Parallel()
	ports := newResultFailurePorts(t, 1)

	err := appsummary.JobResult(t.Context(), ports, appsummary.JobResultInput{Job: "contract-job", Status: "success"})
	if !errors.Is(err, ports.sentinel) || err.Error() != "record job result: "+ports.sentinel.Error() {
		t.Errorf("error = %v, want record job result context and sentinel %v", err, ports.sentinel)
	}

	want := []resultSinkEvent{{"WriteJob", "contract-job", `{"version":1,"job":"contract-job","result":"success"}`}}
	if !slices.Equal(ports.events, want) {
		t.Errorf("attempts = %#v, want only %#v (no collection)", ports.events, want)
	}

	if got := ports.jobs.Body("contract-job"); got != "" {
		t.Errorf("rejecting fake stored failed job write: %q", got)
	}
}

type resultSinkEvent struct {
	method string
	key    string
	body   string
}

type resultFailurePorts struct {
	t            *testing.T
	checkContext func(context.Context)
	failAt       int
	sentinel     error
	events       []resultSinkEvent
	out          *fakeoutputsink.Sink
	manifest     *fakemanifestsink.Sink
	jobs         *fakejobresultstore.Store
}

func newResultFailurePorts(t *testing.T, failAt int) *resultFailurePorts {
	t.Helper()
	ctx := t.Context()

	return &resultFailurePorts{
		t: t, failAt: failAt, sentinel: errors.New(t.Name() + " sink sentinel"), //nolint:err113 // Each row needs a distinct injected error identity.
		checkContext: func(got context.Context) {
			t.Helper()

			if got != ctx {
				t.Error("sink did not receive the caller's exact context")
			}
		},
		out: fakeoutputsink.New(t), manifest: fakemanifestsink.New(t), jobs: fakejobresultstore.New(t),
	}
}

func (p *resultFailurePorts) Set(ctx context.Context, key, value string) error {
	if err := p.record(ctx, "Set", key, value); err != nil {
		return err
	}

	return p.out.Set(ctx, key, value)
}

func (p *resultFailurePorts) SetBool(ctx context.Context, key string, value bool) error {
	if err := p.record(ctx, "SetBool", key, strconv.FormatBool(value)); err != nil {
		return err
	}

	return p.out.SetBool(ctx, key, value)
}

func (p *resultFailurePorts) SetMultiline(ctx context.Context, key string, lines []string) error {
	if err := p.record(ctx, "SetMultiline", key, strings.Join(lines, "\n")); err != nil {
		return err
	}

	return p.out.SetMultiline(ctx, key, lines)
}

func (p *resultFailurePorts) Close(ctx context.Context) error {
	if err := p.record(ctx, "Close", "", ""); err != nil {
		return err
	}

	return p.out.Close(ctx)
}

func (p *resultFailurePorts) Write(ctx context.Context, stage string, result map[string]any) error {
	if err := p.record(ctx, "Write", stage, ""); err != nil {
		return err
	}

	return p.manifest.Write(ctx, stage, result)
}

func (p *resultFailurePorts) WriteJSON(ctx context.Context, stage string, body interface{ MarshalJSON() ([]byte, error) }) error {
	if err := p.record(ctx, "WriteJSON", stage, p.jsonBody(body)); err != nil {
		return err
	}

	return p.manifest.WriteJSON(ctx, stage, body)
}

func (p *resultFailurePorts) WriteJob(ctx context.Context, job string, body interface{ MarshalJSON() ([]byte, error) }) error {
	if err := p.record(ctx, "WriteJob", job, p.jsonBody(body)); err != nil {
		return err
	}

	return p.jobs.WriteJob(ctx, job, body)
}

func (p *resultFailurePorts) CollectJobs(ctx context.Context) ([][]byte, error) {
	if err := p.record(ctx, "CollectJobs", "", ""); err != nil {
		return nil, err
	}

	return p.jobs.CollectJobs(ctx)
}

func (p *resultFailurePorts) record(ctx context.Context, method, key, body string) error {
	p.checkContext(ctx)

	p.events = append(p.events, resultSinkEvent{method, key, body})
	if len(p.events) == p.failAt {
		return p.sentinel
	}

	return nil
}

func (p *resultFailurePorts) jsonBody(body json.Marshaler) string {
	p.t.Helper()

	raw, err := body.MarshalJSON()
	if err != nil {
		p.t.Fatal(err)
	}

	return string(raw)
}
