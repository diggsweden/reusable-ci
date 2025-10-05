// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	appsecurity "github.com/diggsweden/reusable-ci/v3/internal/app/security"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type fakeTrivy struct {
	calls [][]string
	// writePerCall: per-call body to write to the file pointed at by
	// "--output" arg. If too few entries, writes nothing for that call.
	writePerCall []string
	err          error
}

func (f *fakeTrivy) RunInherit(_ context.Context, _, _ io.Writer, args ...string) (int, error) {
	idx := len(f.calls)

	f.calls = append(f.calls, append([]string{}, args...))
	if f.err != nil {
		return -1, f.err
	}

	// A call past the end of writePerCall, or with an empty entry, writes
	// nothing on purpose: that is how a fixture models a tool that produced no
	// report.
	if idx < len(f.writePerCall) && f.writePerCall[idx] != "" {
		writeFixtureReport(scanOutput(args), f.writePerCall[idx], args)
	}

	return 0, nil
}

// writeFixtureReport writes a fake tool's report and panics if it cannot.
//
// The fakes used to return a write failure as the tool's own error, so a broken
// fixture reached the product as "trivy failed" -- and a test asserting how the
// product handles a failing scan could pass because the fixture broke. A panic
// fails the test at once, naming the fixture rather than the code under test,
// without threading a *testing.T through every struct literal. A report
// requested for a call that names no output path is the same kind of fixture
// mistake and fails the same way.
func writeFixtureReport(path, body string, args []string) {
	if path == "" {
		panic(fmt.Sprintf("fake tool: a report was scripted for a call with no output path: %q", args))
	}

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		panic(fmt.Sprintf("fake tool: cannot write the scripted report to %s: %v", path, err))
	}
}

type fakeGit struct {
	allow map[string]bool // worktree-add ref → succeed?
	root  string
	calls [][]string
}

func (g *fakeGit) Run(_ context.Context, args ...string) (string, error) {
	g.calls = append(g.calls, append([]string{}, args...))

	if len(args) == 2 && args[0] == "rev-parse" && args[1] == "--show-toplevel" {
		if g.root != "" {
			return g.root, nil
		}

		return os.Getwd()
	}

	if len(args) >= 4 && args[0] == "worktree" && args[1] == "add" {
		ref := args[len(args)-1]
		if g.allow[ref] {
			return "", nil
		}

		return "", errors.New("worktree add failed") //nolint:err113 // test mock error
	}

	// Worktree removal is the deliberate no-op: cleanup of a worktree the fake
	// never created.
	if len(args) >= 2 && args[0] == "worktree" && args[1] == "remove" {
		return "", nil
	}

	// Anything else used to succeed silently, so the product could start
	// calling git in a way no test described and every test still passed.
	return "", fmt.Errorf("fake git: unexpected call %q", args) //nolint:err113 // test double refusal.
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
	// A blocked scan is a domain-rule failure, not a broken tool.
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(err.Error(), "1 new vulnerability") {
		t.Errorf("error = %v, want the singular count", err)
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
	sink := &appSummaryBuf{}

	var stderr bytes.Buffer

	fsys := testfs.NewReal(t)

	// A fakeGit with no allowed refs refuses every worktree add, which is
	// what the fallback under test reacts to.
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
	// Full mode has no base to diff against, so every HEAD finding counts as
	// new -- the same ErrValidation verdict as diff mode reaches.
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	if !strings.Contains(err.Error(), "1 new vulnerability") {
		t.Errorf("err = %v, want the singular count", err)
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

// TestScanDependencies_UnknownSeverityIsRefused covers the gate's own
// configuration. An unrecognised threshold used to warn and fall back to
// CRITICAL -- the NARROWEST filter -- and because that filter is passed
// to trivy as --severity, HIGH and MEDIUM findings were then neither
// blocking nor reported: absent from the SARIF and the GitLab report
// too. A mistyped gate silently became the least protective one.
//
// The two rows are the spellings a caller actually reaches for.
// "CRITICAL,HIGH" is the grammar the sibling `security scan container`
// documents for the identically-named flag; "error" is the grammar of
// `security scan opengrep`. Both are now refused by name.
func TestScanDependencies_UnknownSeverityIsRefused(t *testing.T) {
	for _, value := range []string{"unknown", "CRITICAL,HIGH", "error"} {
		t.Run(value, func(t *testing.T) {
			trivy := &fakeTrivy{writePerCall: []string{`{"Results":[]}`, ""}}
			sink := &appSummaryBuf{}

			var stderr bytes.Buffer

			fsys := testfs.NewReal(t)

			err := appsecurity.ScanDependencies(context.Background(), trivy, &fakeGit{}, sink, io.Discard, &stderr,
				output.NewAnnotator(&stderr, output.FormatGitHub), appsecurity.ScanDependenciesInput{
					FailOnSeverity: value,
					ScanMode:       security.ScanModeFull,
					SARIFFile:      fsys.Path("trivy-dependency-results.sarif"),
					GitLabDepFile:  fsys.Path("gl-dependency-scanning-report.json"),
				})
			if !errors.Is(err, errs.ErrUsage) {
				t.Fatalf("err = %v, want ErrUsage", err)
			}

			// Refused before trivy ran: a scan under the wrong filter
			// would write reports missing the findings it was meant to
			// catch.
			if len(trivy.calls) != 0 {
				t.Errorf("trivy ran %d times under an unrecognised threshold", len(trivy.calls))
			}
		})
	}
}

// TestScanDependencies_MediumIsAcceptedAsModerate covers the spelling
// trivy itself uses for that band. Refusing it outright would turn the
// most likely typo into a hard failure for a value that is unambiguous.
func TestScanDependencies_MediumIsAcceptedAsModerate(t *testing.T) {
	trivy := &fakeTrivy{writePerCall: []string{`{"Results":[]}`, ""}}
	sink := &appSummaryBuf{}

	var stderr bytes.Buffer

	fsys := testfs.NewReal(t)

	err := appsecurity.ScanDependencies(context.Background(), trivy, &fakeGit{}, sink, io.Discard, &stderr,
		output.NewAnnotator(&stderr, output.FormatGitHub), appsecurity.ScanDependenciesInput{
			FailOnSeverity: "medium",
			ScanMode:       security.ScanModeFull,
			SARIFFile:      fsys.Path("trivy-dependency-results.sarif"),
			GitLabDepFile:  fsys.Path("gl-dependency-scanning-report.json"),
		})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(sink.buf.String(), "| Severity filter | CRITICAL,HIGH,MEDIUM |") {
		t.Errorf("medium did not widen the filter to MEDIUM:\n%s", sink.buf.String())
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

// TestFakes_FailLoudlyOnFixtureMistakes pins the fakes' own contract, since
// every scan test depends on it. A scripted report that cannot be written, or
// is scripted for a call with no output path, must stop the test rather than
// reach the product as a tool failure; a git call no test described must be
// refused rather than succeed.
func TestFakes_FailLoudlyOnFixtureMistakes(t *testing.T) {
	t.Parallel()

	mustPanic := func(t *testing.T, name string, fn func()) {
		t.Helper()

		defer func() {
			if recover() == nil {
				t.Errorf("%s: the fake carried on instead of failing the test", name)
			}
		}()

		fn()
	}

	unwritable := filepath.Join(t.TempDir(), "missing-dir", "report.json")

	mustPanic(t, "unwritable report", func() {
		_, _ = (&fakeTrivy{writePerCall: []string{"{}"}}).RunInherit(context.Background(), io.Discard, io.Discard, "--output", unwritable)
	})

	mustPanic(t, "report scripted without an output path", func() {
		_, _ = (&recordingTrivy{writeJSON: []byte("{}")}).RunInherit(context.Background(), io.Discard, io.Discard, "image", "x")
	})

	mustPanic(t, "opengrep report scripted without --json-output", func() {
		_, _ = (&fakeOpengrep{writeJSON: "{}"}).RunInherit(context.Background(), io.Discard, io.Discard, "scan")
	})

	git := &fakeGit{}
	if _, err := git.Run(context.Background(), "push", "origin", "main"); err == nil {
		t.Error("an unexpected git call succeeded")
	}

	if want := [][]string{{"push", "origin", "main"}}; len(git.calls) != 1 || !slices.Equal(git.calls[0], want[0]) {
		t.Errorf("git calls = %q, want %q", git.calls, want)
	}

	// A call past the scripted list is the deliberate no-write case.
	if code, err := (&fakeTrivy{}).RunInherit(context.Background(), io.Discard, io.Discard, "--output", unwritable); code != 0 || err != nil {
		t.Errorf("an unscripted call = (%d, %v), want a silent success", code, err)
	}
}

// TestScanDependencies_DiffGatesOnNewFindingsInCallOrder drives one diff scan
// end to end. The base carries CVE-1 in lodash; the head keeps that, adds the
// same CVE in lodash-es, and adds CVE-9 in a second lockfile under a package
// name carrying a table delimiter. Both additions are new: the gate compared
// bare IDs and let the lodash-es one through. The remote base ref is refused
// and the local one used; every git and Trivy call is compared in order,
// including worktree removal and the SARIF conversion; the tool's stderr goes
// to the stderr writer; and the summary rows are exact, with the package name
// escaped rather than splitting the row.
func TestScanDependencies_DiffGatesOnNewFindingsInCallOrder(t *testing.T) {
	fsys := testfs.NewReal(t)
	fsys.Chdir()

	const (
		base = `{"Results":[{"Target":"a/package-lock.json","Vulnerabilities":[
{"VulnerabilityID":"CVE-1","Severity":"CRITICAL","PkgName":"lodash","InstalledVersion":"4.17.0","FixedVersion":"4.17.21"}]}]}`
		head = `{"Results":[{"Target":"a/package-lock.json","Vulnerabilities":[
{"VulnerabilityID":"CVE-1","Severity":"CRITICAL","PkgName":"lodash","InstalledVersion":"4.17.1","FixedVersion":"4.17.21"},
{"VulnerabilityID":"CVE-1","Severity":"CRITICAL","PkgName":"lodash-es","InstalledVersion":"4.17.0","FixedVersion":"4.17.21"}]},
{"Target":"b/package-lock.json","Vulnerabilities":[
{"VulnerabilityID":"CVE-9","Severity":"CRITICAL","PkgName":"ax|ios","InstalledVersion":"0.1.0"}]}]}`
	)

	scanner := &scanRecorder{}
	scanner.run = func(_ context.Context, args []string) (int, error) {
		body := map[int]string{0: head, 1: base, 2: `{"version":"2.1.0","runs":[]}`}[len(scanner.calls)-1]

		return 0, os.WriteFile(scanOutput(args), []byte(body), 0o600)
	}

	git := &fakeGit{allow: map[string]bool{"main": true}}
	summary := &appSummaryBuf{}

	var w, stderr bytes.Buffer

	err := appsecurity.ScanDependencies(context.Background(), scanner, git, summary, &w, &stderr, output.Annotator{}, appsecurity.ScanDependenciesInput{
		ScanMode:      security.ScanModeDiff,
		BaseRef:       "main",
		SARIFFile:     fsys.Path("deps.sarif"),
		GitLabDepFile: fsys.Path("deps.json"),
	})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "Found 2 new vulnerabilities") {
		t.Errorf("err = %v, want the gate to count the two new findings", err)
	}

	if len(scanner.calls) != 3 {
		t.Fatalf("trivy calls = %d, want HEAD, base and convert", len(scanner.calls))
	}

	workDir := filepath.Dir(scanOutput(scanner.calls[0].args))
	worktree := filepath.Join(workDir, "base-worktree")
	headJSON := filepath.Join(workDir, "head-vulns.json")

	wantTrivy := [][]string{
		{"fs", "--format", "json", "--severity", "CRITICAL", "--scanners", "vuln", "--output", headJSON, fsys.Root},
		{"fs", "--format", "json", "--severity", "CRITICAL", "--scanners", "vuln", "--output", filepath.Join(workDir, "base-vulns.json"), worktree},
		{"convert", "--format", "sarif", "--output", filepath.Join(workDir, "report.sarif"), headJSON},
	}
	for i, want := range wantTrivy {
		if !slices.Equal(scanner.calls[i].args, want) {
			t.Errorf("trivy call %d =\n%q\nwant\n%q", i, scanner.calls[i].args, want)
		}

		if scanner.calls[i].out != &w || scanner.calls[i].stderr != &stderr {
			t.Errorf("trivy call %d did not receive the caller's writers", i)
		}
	}

	wantGit := [][]string{
		{"rev-parse", "--show-toplevel"},
		{"worktree", "add", "-q", worktree, "origin/main"},
		{"worktree", "add", "-q", worktree, "main"},
		{"worktree", "remove", "-f", worktree},
	}
	if !slices.EqualFunc(git.calls, wantGit, slices.Equal) {
		t.Errorf("git calls =\n%q\nwant\n%q", git.calls, wantGit)
	}

	_, table, found := strings.Cut(summary.buf.String(), "### New Vulnerabilities")
	if !found {
		t.Fatalf("summary has no new-vulnerabilities table:\n%s", summary.buf.String())
	}

	table = "### New Vulnerabilities" + table

	wantTable := "### New Vulnerabilities\n\n" +
		"| ID | Severity | Package | Installed | Fixed |\n" +
		"|----|----------|---------|-----------|-------|\n" +
		"| CVE-1 | CRITICAL | lodash-es | 4.17.0 | 4.17.21 |\n" +
		"| CVE-9 | CRITICAL | ax&#124;ios | 0.1.0 | — |\n\n"
	if table != wantTable {
		t.Errorf("summary table =\n%s\nwant\n%s", table, wantTable)
	}

	if !strings.Contains(summary.buf.String(), "| New vulnerabilities | **2** |") {
		t.Errorf("summary count wrong:\n%s", summary.buf.String())
	}
}

// TestScanDependencies_FullModesReportEveryFinding covers the modes with no
// base to compare: requested full, and diff without a base ref. Every head
// finding is new, including one ID in two packages, and the rows say so.
func TestScanDependencies_FullModesReportEveryFinding(t *testing.T) {
	const head = `{"Results":[{"Target":"go.mod","Vulnerabilities":[
{"VulnerabilityID":"CVE-1","Severity":"CRITICAL","PkgName":"alpha","InstalledVersion":"1.0","FixedVersion":"1.1"},
{"VulnerabilityID":"CVE-1","Severity":"CRITICAL","PkgName":"beta","InstalledVersion":"2.0","FixedVersion":"2.1"}]}]}`

	for name, in := range map[string]appsecurity.ScanDependenciesInput{
		"full":              {ScanMode: security.ScanModeFull},
		"diff without base": {ScanMode: security.ScanModeDiff},
	} {
		t.Run(name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			trivy := &fakeTrivy{writePerCall: []string{head, ""}}
			summary := &appSummaryBuf{}

			in.SARIFFile, in.GitLabDepFile = fsys.Path("deps.sarif"), fsys.Path("deps.json")

			err := appsecurity.ScanDependencies(context.Background(), trivy, &fakeGit{}, summary, io.Discard, io.Discard, output.Annotator{}, in)
			if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "Found 2 new vulnerabilities") {
				t.Errorf("err = %v, want both findings counted", err)
			}

			for _, row := range []string{"| CVE-1 | CRITICAL | alpha | 1.0 | 1.1 |\n", "| CVE-1 | CRITICAL | beta | 2.0 | 2.1 |\n", "| New vulnerabilities | **2** |"} {
				if !strings.Contains(summary.buf.String(), row) {
					t.Errorf("summary missing %q:\n%s", row, summary.buf.String())
				}
			}
		})
	}
}
