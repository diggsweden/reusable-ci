// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	appsecurity "github.com/diggsweden/reusable-ci/v3/internal/app/security"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type fakeOpengrep struct {
	exitCode int
	args     []string

	// writeJSON, if set, is written to the file named by the --json-output
	// flag, standing in for the report opengrep itself would produce.
	writeJSON string
	calls     [][]string

	// writeOthers also writes the SARIF, text and GitLab reports, so the
	// publication of every output can be observed.
	writeOthers bool
	ctx         context.Context //nolint:containedctx // recorded to assert the caller's context is forwarded.
	w, stderr   io.Writer
}

func (f *fakeOpengrep) RunInherit(ctx context.Context, w, stderr io.Writer, args ...string) (int, error) {
	f.ctx, f.w, f.stderr = ctx, w, stderr

	// Every call is kept; args stays the most recent for the existing
	// assertions, but a second invocation no longer overwrites the evidence of
	// the first.
	f.args = append([]string{}, args...)
	f.calls = append(f.calls, f.args)

	if f.writeJSON != "" {
		output := ""

		for i, a := range args { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			if a == "--json-output" && i+1 < len(args) {
				output = args[i+1]
			}
		}

		writeFixtureReport(output, f.writeJSON, args)
	}

	if f.writeOthers {
		for flag, body := range map[string]string{
			"--sarif-output":       `{"from":"--sarif-output"}`,
			"--gitlab-sast-output": `{"from":"--gitlab-sast-output"}`,
			"--text-output":        "text report",
		} {
			writeFixtureReport(flagValue(args, flag), body, args)
		}
	}

	return f.exitCode, nil
}

type appSummaryBuf struct{ buf bytes.Buffer }

func (s *appSummaryBuf) Append(_ context.Context, md string) error {
	s.buf.WriteString(md)

	return nil
}

func TestRunOpengrep_CleanScan(t *testing.T) {
	testfs.NewReal(t).Chdir()

	out := fakeoutputsink.New(t)
	summary := &appSummaryBuf{}
	ops := &fakeOpengrep{
		exitCode:  0,
		writeJSON: `{"results":[]}`,
	}

	err := appsecurity.RunOpengrep(context.Background(), ops, out, summary, io.Discard, io.Discard, output.Annotator{}, appsecurity.RunOpengrepInput{
		Config: "p/default",
	})
	if err != nil {
		t.Fatalf("RunOpengrep: %v", err)
	}

	if got := out.Single("opengrep-result"); got != "success" {
		t.Errorf("opengrep-result = %q, want success", got)
	}

	if got := out.Single("opengrep-findings-total"); got != "0" {
		t.Errorf("findings-total = %q", got)
	}

	if !strings.Contains(summary.buf.String(), "Passed with `0` findings.") {
		t.Errorf("missing clean-pass line:\n%s", summary.buf.String())
	}
}

func TestRunOpengrep_FindingsBlockBySeverityThreshold(t *testing.T) {
	testfs.NewReal(t).Chdir()

	out := fakeoutputsink.New(t)
	summary := &appSummaryBuf{}
	ops := &fakeOpengrep{
		exitCode: 0,
		writeJSON: `{"results":[
{"check_id":"a","extra":{"severity":"ERROR"}},
{"check_id":"b","extra":{"severity":"WARNING"}}
]}`,
	}

	err := appsecurity.RunOpengrep(context.Background(), ops, out, summary, io.Discard, io.Discard, output.Annotator{}, appsecurity.RunOpengrepInput{
		FailOnSeverity: "high",
	})
	// A blocked scan is a domain-rule failure, not a broken tool: ErrValidation
	// so the CLI exits 1 rather than 70 ("file a bug").
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if got := out.Single("opengrep-result"); got != "failure" {
		t.Errorf("opengrep-result = %q, want failure", got)
	}

	if got := out.Single("opengrep-findings-error"); got != "1" {
		t.Errorf("findings-error = %q", got)
	}
}

func TestRunOpengrep_ScanFailureWritesFailureSummary(t *testing.T) {
	testfs.NewReal(t).Chdir()

	out := fakeoutputsink.New(t)
	summary := &appSummaryBuf{}
	ops := &fakeOpengrep{exitCode: 2}

	err := appsecurity.RunOpengrep(context.Background(), ops, out, summary, io.Discard, io.Discard, output.Annotator{}, appsecurity.RunOpengrepInput{})
	// The scanner itself failed, so this is not a finding: ErrDependencyUnavailable
	// (exit 69) keeps a broken tool distinct from a blocked scan (ErrValidation).
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("err = %v, want ErrDependencyUnavailable", err)
	}

	if !strings.Contains(err.Error(), "status 2") {
		t.Errorf("error = %v, want it to carry the exit status", err)
	}

	if !strings.Contains(summary.buf.String(), "OpenGrep exited with status 2") {
		t.Errorf("missing failure summary:\n%s", summary.buf.String())
	}
}

func TestRunOpengrep_RejectsUnsupportedSeverity(t *testing.T) {
	out := fakeoutputsink.New(t)
	summary := &appSummaryBuf{}

	var stderr bytes.Buffer

	ops := &fakeOpengrep{}

	err := appsecurity.RunOpengrep(context.Background(), ops, out, summary, io.Discard, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appsecurity.RunOpengrepInput{
		FailOnSeverity: "severe",
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(stderr.String(), "unsupported OPENGREP_FAIL_ON_SEVERITY") {
		t.Errorf("missing error line:\n%s", stderr.String())
	}

	// Refused before scanning: running under a threshold the gate cannot
	// interpret would produce a verdict nobody can trust.
	if ops.args != nil {
		t.Errorf("opengrep ran under an unrecognised threshold: %v", ops.args)
	}
}

func TestRunOpengrep_PassesConfigArgs(t *testing.T) {
	testfs.NewReal(t).Chdir()

	out := fakeoutputsink.New(t)
	summary := &appSummaryBuf{}

	ops := &fakeOpengrep{writeJSON: `{"results":[]}`}
	if err := appsecurity.RunOpengrep(context.Background(), ops, out, summary, io.Discard, io.Discard, output.Annotator{}, appsecurity.RunOpengrepInput{
		Config:     " p/default, my-rules.yaml ",
		TargetPath: "src",
	}); err != nil {
		t.Fatal(err)
	}

	// The fixture deliberately carries surrounding whitespace on both entries.
	// Counting `--config` occurrences would pass even if " p/default" reached
	// opengrep with its leading space, which is not a config it can resolve.
	// The list is also the tail of the invocation, immediately before the
	// target path, so the assertion pins the ordering too.
	wantTail := []string{"--config", "p/default", "--config", "my-rules.yaml"}
	if len(ops.args) < len(wantTail)+1 {
		t.Fatalf("args too short to carry the config list: %v", ops.args)
	}

	gotTail := ops.args[len(ops.args)-len(wantTail)-1 : len(ops.args)-1]
	if !slices.Equal(gotTail, wantTail) {
		t.Errorf("config args = %v, want %v (full args: %v)", gotTail, wantTail, ops.args)
	}

	// Target path must be last.
	if filepath.Base(ops.args[len(ops.args)-1]) != "src" {
		t.Errorf("target path not last arg: %v", ops.args)
	}
}

type ctxKey struct{}

// TestRunOpengrep_InvokesTheScannerOnceWithTheWholeCommand pins the complete
// argv. The config test above reads a tail and compares only the target's base
// name, so a target of other/src, a dropped flag or two outputs pointed at one
// file all passed. The four reports go to distinct files in a fresh private
// directory and are published to their destinations only after the scan; the
// caller's context and writers reach the scanner unchanged.
func TestRunOpengrep_InvokesTheScannerOnceWithTheWholeCommand(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	ctx := context.WithValue(context.Background(), ctxKey{}, "caller")
	ops := &fakeOpengrep{writeJSON: `{"results":[]}`, writeOthers: true}

	var w, stderr bytes.Buffer

	if err := appsecurity.RunOpengrep(ctx, ops, fakeoutputsink.New(t), &appSummaryBuf{}, &w, &stderr, output.Annotator{}, appsecurity.RunOpengrepInput{
		Config:     " p/default, my-rules.yaml ",
		TargetPath: "src",
	}); err != nil {
		t.Fatal(err)
	}

	if len(ops.calls) != 1 {
		t.Fatalf("opengrep calls = %d, want 1", len(ops.calls))
	}

	args := ops.calls[0]
	value := func(flag string) string {
		i := slices.Index(args, flag)
		if i < 0 || i+1 >= len(args) {
			t.Fatalf("%s missing from %q", flag, args)
		}

		return args[i+1]
	}

	jsonOut, sarifOut, textOut, gitlabOut := value("--json-output"), value("--sarif-output"), value("--text-output"), value("--gitlab-sast-output")

	want := []string{
		"scan", "--quiet", "--disable-version-check", "--exclude", ".github-shared", "--taint-intrafile", "--dataflow-traces",
		"--json-output", jsonOut, "--sarif-output", sarifOut, "--text-output", textOut, "--gitlab-sast-output", gitlabOut,
		"--config", "p/default", "--config", "my-rules.yaml", "src",
	}
	if !slices.Equal(args, want) {
		t.Errorf("argv =\n%q\nwant\n%q", args, want)
	}

	outputs := []string{jsonOut, sarifOut, textOut, gitlabOut}
	if len(slices.Compact(slices.Sorted(slices.Values(outputs)))) != len(outputs) {
		t.Errorf("report outputs are not distinct: %q", outputs)
	}

	for _, path := range outputs {
		if filepath.Dir(path) != filepath.Dir(jsonOut) || filepath.Dir(path) == fsys.Root {
			t.Errorf("report %s is not in the scan's private directory", path)
		}
	}

	for name, body := range map[string]string{
		"opengrep-results.json":             `{"results":[]}`,
		"opengrep-results.sarif":            `{"from":"--sarif-output"}`,
		"opengrep-results.txt":              "text report",
		"opengrep-results.gitlab-sast.json": `{"from":"--gitlab-sast-output"}`,
	} {
		if got := string(fsys.ReadFile(name)); got != body {
			t.Errorf("published %s = %q, want %q", name, got, body)
		}
	}

	if ops.ctx.Value(ctxKey{}) != "caller" || ops.w != &w || ops.stderr != &stderr {
		t.Errorf("the scanner did not receive the caller's context and writers")
	}
}

// TestRunOpengrep_OutputsFollowTheThreshold uses a report whose three
// severity counts all differ, so a swapped output shows, and walks each
// threshold across its edge: medium ignores INFO, low does not, high ignores
// WARNING, none ignores everything. All six outputs are compared whole.
func TestRunOpengrep_OutputsFollowTheThreshold(t *testing.T) {
	const (
		mixed = `{"results":[
{"check_id":"e1","extra":{"severity":"ERROR"}},{"check_id":"e2","extra":{"severity":"ERROR"}},{"check_id":"e3","extra":{"severity":"ERROR"}},
{"check_id":"w1","extra":{"severity":"WARNING"}},{"check_id":"w2","extra":{"severity":"WARNING"}},
{"check_id":"i1","extra":{"severity":"INFO"}}]}`
		infoOnly    = `{"results":[{"check_id":"i1","extra":{"severity":"INFO"}}]}`
		warningOnly = `{"results":[{"check_id":"w1","extra":{"severity":"WARNING"}}]}`
	)

	outputs := func(result, total, errs, warnings, info, threshold string) map[string]string {
		return map[string]string{
			"opengrep-result": result, "opengrep-findings-total": total, "opengrep-findings-error": errs,
			"opengrep-findings-warning": warnings, "opengrep-findings-info": info, "opengrep-fail-threshold": threshold,
		}
	}

	for name, tc := range map[string]struct {
		report, threshold string
		blocked           bool
		want              map[string]string
	}{
		"high blocks errors":     {report: mixed, threshold: "high", blocked: true, want: outputs("failure", "6", "3", "2", "1", "high")},
		"none never blocks":      {report: mixed, threshold: "none", want: outputs("success", "6", "3", "2", "1", "none")},
		"medium ignores info":    {report: infoOnly, threshold: "medium", want: outputs("success", "1", "0", "0", "1", "medium")},
		"low blocks info":        {report: infoOnly, threshold: "low", blocked: true, want: outputs("failure", "1", "0", "0", "1", "low")},
		"high ignores warnings":  {report: warningOnly, threshold: "high", want: outputs("success", "1", "0", "1", "0", "high")},
		"medium blocks warnings": {report: warningOnly, threshold: "warning", blocked: true, want: outputs("failure", "1", "0", "1", "0", "medium")},
	} {
		t.Run(name, func(t *testing.T) {
			testfs.NewReal(t).Chdir()

			sink := fakeoutputsink.New(t)

			var w bytes.Buffer

			err := appsecurity.RunOpengrep(context.Background(), &fakeOpengrep{writeJSON: tc.report}, sink, &appSummaryBuf{}, &w, io.Discard, output.Annotator{}, appsecurity.RunOpengrepInput{FailOnSeverity: tc.threshold})

			if tc.blocked != errors.Is(err, errs.ErrValidation) || (!tc.blocked && err != nil) {
				t.Errorf("err = %v, want blocked=%v", err, tc.blocked)
			}

			if got := sink.AllScalar(); !maps.Equal(got, tc.want) {
				t.Errorf("outputs =\n%v\nwant\n%v", got, tc.want)
			}

			if tc.blocked == strings.Contains(w.String(), "completed successfully") {
				t.Errorf("stdout = %q; the success line must appear only when not blocked", w.String())
			}
		})
	}
}

// TestRunOpengrep_StopsAtTheFirstPublicationFailure fails the summary and
// then each output in turn. The error is returned with its cause, nothing
// after the failing write happens, and no success line is printed. Outputs
// already written stay written; nothing promises to roll them back.
func TestRunOpengrep_StopsAtTheFirstPublicationFailure(t *testing.T) {
	keys := []string{"opengrep-result", "opengrep-findings-total", "opengrep-findings-error", "opengrep-findings-warning", "opengrep-findings-info", "opengrep-fail-threshold"}
	errWriteFailed := errors.New("write failed") //nolint:err113 // a unique value to find in the chain.

	run := func(t *testing.T, sink *failingSink, summary interface {
		Append(ctx context.Context, markdown string) error
	},
	) (string, error) {
		t.Helper()
		testfs.NewReal(t).Chdir()

		var w bytes.Buffer

		err := appsecurity.RunOpengrep(context.Background(), &fakeOpengrep{writeJSON: `{"results":[]}`}, sink, summary, &w, io.Discard, output.Annotator{}, appsecurity.RunOpengrepInput{})

		return w.String(), err
	}

	t.Run("summary", func(t *testing.T) {
		sink := &failingSink{Sink: fakeoutputsink.New(t)}

		stdout, err := run(t, sink, failingSummary{err: errWriteFailed})
		if !errors.Is(err, errWriteFailed) || len(sink.Keys()) != 0 || strings.Contains(stdout, "completed successfully") {
			t.Errorf("err = %v, outputs = %v, stdout = %q; want the cause, no outputs, no success line", err, sink.Keys(), stdout)
		}
	})

	for i, key := range keys {
		t.Run(key, func(t *testing.T) {
			sink := &failingSink{Sink: fakeoutputsink.New(t), failKey: key, err: errWriteFailed}

			stdout, err := run(t, sink, &appSummaryBuf{})
			if !errors.Is(err, errWriteFailed) || strings.Contains(stdout, "completed successfully") {
				t.Errorf("err = %v, stdout = %q; want the cause and no success line", err, stdout)
			}

			if got, want := sink.Order(), keys[:i]; !slices.Equal(got, want) {
				t.Errorf("outputs written = %v, want only those before %s: %v", got, key, want)
			}
		})
	}
}

type failingSummary struct{ err error }

func (f failingSummary) Append(context.Context, string) error { return f.err }

// flagValue returns the value following flag in args, or "" when absent.
func flagValue(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}

	return ""
}
