// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	appsecurity "github.com/diggsweden/reusable-ci/v3/internal/app/security"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type fakeTrivy struct {
	calls [][]string
	// writePerCall: per-call body to write to the file pointed at by
	// "--output" arg. If too few entries, writes nothing for that call.
	writePerCall []string
	exitCodes    []int
	err          error
}

func (f *fakeTrivy) RunInherit(_ context.Context, _, _ io.Writer, args ...string) (int, error) {
	idx := len(f.calls)

	f.calls = append(f.calls, args)
	if f.err != nil {
		return -1, f.err
	}

	if idx < len(f.writePerCall) && f.writePerCall[idx] != "" {
		for i, a := range args {
			if a == "--output" && i+1 < len(args) {
				_ = os.WriteFile(args[i+1], []byte(f.writePerCall[idx]), 0o644) //nolint:gosec // test fixture

				break
			}
		}
	}

	if idx < len(f.exitCodes) {
		return f.exitCodes[idx], nil
	}

	return 0, nil
}

type fakeGit struct {
	allow map[string]bool // worktree-add ref → succeed?
	calls [][]string
}

func (g *fakeGit) Run(_ context.Context, args ...string) (string, error) {
	g.calls = append(g.calls, args)
	if len(args) >= 4 && args[0] == "worktree" && args[1] == "add" {
		ref := args[len(args)-1]
		if g.allow[ref] {
			return "", nil
		}

		return "", errors.New("worktree add failed") //nolint:err113 // test mock error
	}

	return "", nil
}

func TestScanDependencies_DiffModeFindsNew(t *testing.T) {
	baseJSON := `{"Results":[{"Vulnerabilities":[{"VulnerabilityID":"CVE-1"}]}]}`
	headJSON := `{"Results":[{"Vulnerabilities":[
		{"VulnerabilityID":"CVE-1"},
		{"VulnerabilityID":"CVE-NEW","Severity":"HIGH","PkgName":"foo","InstalledVersion":"1.0","FixedVersion":"1.1"}
	]}]}`
	trivy := &fakeTrivy{
		// 1st call: HEAD scan → write headJSON
		// 2nd call: base scan → write baseJSON
		// 3rd call: trivy convert (we don't care about output)
		writePerCall: []string{headJSON, baseJSON, ""},
	}
	git := &fakeGit{allow: map[string]bool{"origin/main": true}}
	sink := &appSummaryBuf{}

	fsys := testfs.NewReal(t)

	err := appsecurity.ScanDependencies(context.Background(), trivy, git, sink, io.Discard, io.Discard, output.Annotator{}, appsecurity.ScanDependenciesInput{
		FailOnSeverity: "high",
		ScanMode:       security.ScanModeDiff,
		BaseRef:        "main",
		SARIFFile:      fsys.Path("trivy-dependency-results.sarif"),
		GitLabDepFile:  fsys.Path("gl-dependency-scanning-report.json"),
	})
	if err == nil {
		t.Fatal("expected error from new vulnerability")
	}

	if !strings.Contains(err.Error(), "1 new vulnerability") {
		t.Errorf("error = %v", err)
	}

	body := sink.buf.String()
	for _, want := range []string{
		"| Mode | diff |",
		"| New vulnerabilities | **1** |",
		"| CVE-NEW | HIGH | foo | 1.0 | 1.1 |",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in summary:\n%s", want, body)
		}
	}
}

func TestScanDependencies_DiffModeNoBaseRefFallsBackToFull(t *testing.T) {
	headJSON := `{"Results":[{"Vulnerabilities":[]}]}`
	trivy := &fakeTrivy{
		writePerCall: []string{headJSON, ""},
	}
	sink := &appSummaryBuf{}

	var stderr bytes.Buffer

	fsys := testfs.NewReal(t)

	err := appsecurity.ScanDependencies(context.Background(), trivy, &fakeGit{}, sink, io.Discard, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appsecurity.ScanDependenciesInput{
		ScanMode:      security.ScanModeDiff,
		BaseRef:       "",
		SARIFFile:     fsys.Path("trivy-dependency-results.sarif"),
		GitLabDepFile: fsys.Path("gl-dependency-scanning-report.json"),
	})
	if err != nil {
		t.Fatalf("ScanDependencies: %v", err)
	}

	if !strings.Contains(stderr.String(), "::warning::Diff mode requested but no base ref") {
		t.Errorf("missing fallback warning:\n%s", stderr.String())
	}

	if !strings.Contains(sink.buf.String(), "| Mode | full (no base ref) |") {
		t.Errorf("summary mode wrong:\n%s", sink.buf.String())
	}
}

func TestScanDependencies_WorktreeFailureFallsBackToFull(t *testing.T) {
	headJSON := `{"Results":[{"Vulnerabilities":[]}]}`
	trivy := &fakeTrivy{writePerCall: []string{headJSON, ""}}
	// Git refuses both worktree add attempts.
	sink := &appSummaryBuf{}

	var stderr bytes.Buffer

	fsys := testfs.NewReal(t)

	err := appsecurity.ScanDependencies(context.Background(), trivy, &fakeGit{}, sink, io.Discard, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appsecurity.ScanDependenciesInput{
		ScanMode:      security.ScanModeDiff,
		BaseRef:       "main",
		SARIFFile:     fsys.Path("trivy-dependency-results.sarif"),
		GitLabDepFile: fsys.Path("gl-dependency-scanning-report.json"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(stderr.String(), "Could not create worktree") {
		t.Errorf("missing worktree warning:\n%s", stderr.String())
	}

	if !strings.Contains(sink.buf.String(), "| Mode | full (worktree fallback) |") {
		t.Errorf("summary mode wrong:\n%s", sink.buf.String())
	}
}

func TestScanDependencies_FullModeUsesAllHeadIDs(t *testing.T) {
	headJSON := `{"Results":[{"Vulnerabilities":[{"VulnerabilityID":"CVE-1"}]}]}`
	trivy := &fakeTrivy{writePerCall: []string{headJSON, ""}}
	sink := &appSummaryBuf{}
	fsys := testfs.NewReal(t)

	err := appsecurity.ScanDependencies(context.Background(), trivy, &fakeGit{}, sink, io.Discard, io.Discard, output.Annotator{}, appsecurity.ScanDependenciesInput{
		ScanMode:      security.ScanModeFull,
		SARIFFile:     fsys.Path("trivy-dependency-results.sarif"),
		GitLabDepFile: fsys.Path("gl-dependency-scanning-report.json"),
	})
	if err == nil || !strings.Contains(err.Error(), "1 new vulnerability") {
		t.Errorf("expected single-vuln error, got: %v", err)
	}
}

func TestScanDependencies_CleanResult(t *testing.T) {
	headJSON := `{"Results":[]}` //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	trivy := &fakeTrivy{writePerCall: []string{headJSON, ""}}
	sink := &appSummaryBuf{}

	var out bytes.Buffer

	fsys := testfs.NewReal(t)

	err := appsecurity.ScanDependencies(context.Background(), trivy, &fakeGit{}, sink, &out, io.Discard, output.Annotator{}, appsecurity.ScanDependenciesInput{
		ScanMode:      security.ScanModeFull,
		SARIFFile:     fsys.Path("trivy-dependency-results.sarif"),
		GitLabDepFile: fsys.Path("gl-dependency-scanning-report.json"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(sink.buf.String(), "> No new vulnerabilities found.") {
		t.Errorf("missing clean line:\n%s", sink.buf.String())
	}

	for _, want := range []string{"Severity threshold: critical", "Scan mode: full", "Scan path: ."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("out missing %q:\n%s", want, out.String())
		}
	}
}

func TestScanDependencies_UnknownSeverityWarnsAndDefaultsToCritical(t *testing.T) {
	headJSON := `{"Results":[]}`
	trivy := &fakeTrivy{writePerCall: []string{headJSON, ""}}
	sink := &appSummaryBuf{}

	var stderr bytes.Buffer

	fsys := testfs.NewReal(t)

	err := appsecurity.ScanDependencies(context.Background(), trivy, &fakeGit{}, sink, io.Discard, &stderr, output.NewAnnotator(&stderr, output.FormatGitHub), appsecurity.ScanDependenciesInput{
		FailOnSeverity: "unknown",
		ScanMode:       security.ScanModeFull,
		SARIFFile:      fsys.Path("trivy-dependency-results.sarif"),
		GitLabDepFile:  fsys.Path("gl-dependency-scanning-report.json"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(stderr.String(), "::warning::Unknown severity") {
		t.Errorf("missing warning:\n%s", stderr.String())
	}

	if !strings.Contains(sink.buf.String(), "| Severity filter | CRITICAL |") {
		t.Errorf("summary should default to CRITICAL:\n%s", sink.buf.String())
	}
}

func TestScanDependencies_CustomScanPathShown(t *testing.T) {
	headJSON := `{"Results":[]}`
	trivy := &fakeTrivy{writePerCall: []string{headJSON, ""}}
	sink := &appSummaryBuf{}

	var out bytes.Buffer

	fsys := testfs.NewReal(t)
	custom := fsys.MkdirAll("custom-project")

	err := appsecurity.ScanDependencies(context.Background(), trivy, &fakeGit{}, sink, &out, io.Discard, output.Annotator{}, appsecurity.ScanDependenciesInput{
		ScanMode:      security.ScanModeFull,
		ScanPath:      custom,
		SARIFFile:     fsys.Path("trivy-dependency-results.sarif"),
		GitLabDepFile: fsys.Path("gl-dependency-scanning-report.json"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "Scan path: "+custom) {
		t.Errorf("out = %s", out.String())
	}
}
