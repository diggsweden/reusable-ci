// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsecurity "github.com/diggsweden/reusable-ci/internal/app/security"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

type fakeOpengrep struct {
	exitCode int
	runErr   error
	args     []string

	// writeOnRun: if set, writes the body to the file pointed at by
	// the --json-output flag in args. Lets tests simulate opengrep
	// producing its output files.
	writeJSON string
	writeText string
}

func (f *fakeOpengrep) RunInherit(_ context.Context, _, _ io.Writer, args ...string) (int, error) {
	f.args = args
	if f.runErr != nil {
		return -1, f.runErr
	}

	for i, a := range args { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if a == "--json-output" && i+1 < len(args) && f.writeJSON != "" {
			_ = os.WriteFile(args[i+1], []byte(f.writeJSON), 0o644) //nolint:gosec // test fixture
		}

		if a == "--text-output" && i+1 < len(args) && f.writeText != "" {
			_ = os.WriteFile(args[i+1], []byte(f.writeText), 0o644) //nolint:gosec // test fixture
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
{"check_id":"a","severity":"ERROR"},
{"check_id":"b","severity":"WARNING"}
]}`,
	}

	err := appsecurity.RunOpengrep(context.Background(), ops, out, summary, io.Discard, io.Discard, output.Annotator{}, appsecurity.RunOpengrepInput{
		FailOnSeverity: "high",
	})
	if err == nil {
		t.Fatal("expected error when threshold met")
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
	if err == nil {
		t.Fatal("expected error from non-zero scan exit")
	}

	if !strings.Contains(err.Error(), "status 2") {
		t.Errorf("error = %v", err)
	}

	if !strings.Contains(summary.buf.String(), "OpenGrep exited with status 2") {
		t.Errorf("missing failure summary:\n%s", summary.buf.String())
	}
}

func TestRunOpengrep_RejectsUnsupportedSeverity(t *testing.T) {
	out := fakeoutputsink.New(t)
	summary := &appSummaryBuf{}

	var stderr bytes.Buffer

	err := appsecurity.RunOpengrep(context.Background(), &fakeOpengrep{}, out, summary, io.Discard, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appsecurity.RunOpengrepInput{
		FailOnSeverity: "severe",
	})
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(stderr.String(), "unsupported OPENGREP_FAIL_ON_SEVERITY") {
		t.Errorf("missing error line:\n%s", stderr.String())
	}
}

func TestRunOpengrep_PassesConfigArgs(t *testing.T) {
	testfs.NewReal(t).Chdir()

	out := fakeoutputsink.New(t)
	summary := &appSummaryBuf{}

	ops := &fakeOpengrep{writeJSON: `{}`}
	if err := appsecurity.RunOpengrep(context.Background(), ops, out, summary, io.Discard, io.Discard, output.Annotator{}, appsecurity.RunOpengrepInput{
		Config:     " p/default, my-rules.yaml ",
		TargetPath: "src",
	}); err != nil {
		t.Fatal(err)
	}
	// Must have at least two `--config` pairs in args.
	count := 0

	for i, a := range ops.args {
		if a == "--config" && i+1 < len(ops.args) {
			count++
		}
	}

	if count != 2 {
		t.Errorf("expected 2 --config args, got %d (full args: %v)", count, ops.args)
	}
	// Target path must be last.
	if filepath.Base(ops.args[len(ops.args)-1]) != "src" {
		t.Errorf("target path not last arg: %v", ops.args)
	}
}
