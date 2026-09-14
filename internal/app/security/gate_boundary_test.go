// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	appsecurity "github.com/diggsweden/reusable-ci/v3/internal/app/security"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	domainsecurity "github.com/diggsweden/reusable-ci/v3/internal/domain/security"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type scanCall struct {
	args        []string
	out, stderr io.Writer
}
type scanRecorder struct {
	calls []scanCall
	run   func(context.Context, []string) (int, error)
}

func (f *scanRecorder) RunInherit(ctx context.Context, out, stderr io.Writer, args ...string) (int, error) {
	f.calls = append(f.calls, scanCall{args: slices.Clone(args), out: out, stderr: stderr})

	return f.run(ctx, args)
}
func scanOutput(args []string) string {
	for i, arg := range args {
		if arg == "--output" && i+1 < len(args) {
			return args[i+1]
		}
	}

	return ""
}

func TestOpenGrepGateBoundary_StructuralReports(t *testing.T) {
	for _, body := range []string{
		`{"results":[{"check_id":"r","extra":{"severity":"ERROR"}}]}`,
		"{\n  \"results\": [{\"check_id\": \"r\", \"extra\": {\"severity\": \"ERROR\"}}]\n}",
		`{"results":[{"check_id":"r","extra":{"severity":"WARNING"}}]}`,
		`{"results":[]}`, `null`, `not JSON`, `{"results":{}}`, `{"results":[null]}`, `{"results":[],"errors":[{"message":"scan failed"}]}`,
	} {
		fsys := testfs.NewReal(t)
		fsys.Chdir()

		sink := fakeoutputsink.New(t)
		summary := &appSummaryBuf{}

		var out bytes.Buffer

		err := appsecurity.RunOpengrep(t.Context(), &fakeOpengrep{writeJSON: body}, sink, summary, &out, &out, output.Annotator{}, appsecurity.RunOpengrepInput{FailOnSeverity: "high"})
		switch {
		case strings.Contains(body, `"ERROR"`):
			if !errors.Is(err, errs.ErrValidation) || sink.Single("opengrep-findings-error") != "1" || sink.Single("opengrep-findings-total") != "1" {
				t.Fatalf("body=%s err=%v outputs=%v", body, err, sink.AllScalar())
			}
		case body == `{"results":[]}` || strings.Contains(body, `"WARNING"`):
			if err != nil || sink.Single("opengrep-result") != "success" {
				t.Fatalf("body=%s err=%v", body, err)
			}
		default:
			if !errors.Is(err, errs.ErrMalformedInput) || len(sink.Keys()) != 0 || summary.buf.Len() != 0 || strings.Contains(out.String(), "completed successfully") {
				t.Fatalf("invalid report err=%v outputs=%v summary=%s", err, sink.Keys(), &summary.buf)
			}
		}
	}
}

const boundaryTrivyJSON = `{"ArtifactName":"fixture-image","Results":[{"Target":"first","Vulnerabilities":[{"VulnerabilityID":"CVE-A","PkgName":"alpha","InstalledVersion":"1.0","FixedVersion":"1.1","Severity":"HIGH","Title":"Alpha issue"}]},{"Target":"second","Vulnerabilities":[{"VulnerabilityID":"CVE-B","PkgName":"beta","InstalledVersion":"2.0","FixedVersion":"2.1","Severity":"CRITICAL","Title":"Beta issue"}]}]}`

func TestContainerReportBoundary_CompleteFindingAssociations(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	in := appsecurity.ScanContainerInput{ImageRef: "registry.example/owner/image:tag", JSONFile: fsys.Path("raw.json"), SARIFFile: fsys.Path("report.sarif"), GitLabReportFile: fsys.Path("report.json"), TrivyVersion: "fixture-version"}
	scanner := &scanRecorder{run: func(_ context.Context, args []string) (int, error) {
		return 0, os.WriteFile(scanOutput(args), []byte(boundaryTrivyJSON), 0o600)
	}}

	err := appsecurity.ScanContainer(t.Context(), scanner, io.Discard, io.Discard, output.Annotator{}, in)
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "2 container vulnerabilities") {
		t.Fatalf("gate=%v", err)
	}

	var sarif struct {
		Runs []struct {
			Results []struct {
				RuleID  string `json:"ruleId"`
				Level   string `json:"level"`
				Message struct {
					Text string `json:"text"`
				} `json:"message"`
				Locations []struct {
					Physical struct {
						Artifact struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(fsys.ReadFile("report.sarif"), &sarif); err != nil {
		t.Fatal(err)
	}

	if len(sarif.Runs) != 1 || len(sarif.Runs[0].Results) != 2 {
		t.Fatalf("SARIF lost findings: %+v", sarif)
	}

	for i, want := range []struct{ id, message string }{{"CVE-A", "Alpha issue \u2014 alpha@1.0 (fix: 1.1)"}, {"CVE-B", "Beta issue \u2014 beta@2.0 (fix: 2.1)"}} {
		got := sarif.Runs[0].Results[i]
		if got.RuleID != want.id || got.Message.Text != want.message || got.Level != "error" || len(got.Locations) != 1 || got.Locations[0].Physical.Artifact.URI != in.ImageRef {
			t.Fatalf("SARIF association=%+v", got)
		}
	}

	var gitlab domainsecurity.GitLabReport
	if err := json.Unmarshal(fsys.ReadFile("report.json"), &gitlab); err != nil {
		t.Fatal(err)
	}

	if len(gitlab.Vulnerabilities) != 2 {
		t.Fatalf("GitLab lost findings: %+v", gitlab)
	}

	for i, want := range []struct{ id, pkg, version, severity string }{{"CVE-A", "alpha", "1.0", "High"}, {"CVE-B", "beta", "2.0", "Critical"}} {
		got := gitlab.Vulnerabilities[i]
		if len(got.Identifiers) == 0 || got.Identifiers[0].Value != want.id || got.Location.Dependency == nil || got.Location.Dependency.Package.Name != want.pkg || got.Location.Dependency.Version != want.version || got.Severity != want.severity || got.Location.Image != in.ImageRef {
			t.Fatalf("GitLab association=%+v", got)
		}
	}
}

func TestContainerReportBoundary_StatusShapeAndAliases(t *testing.T) { //nolint:gocognit // one owned fixture independently exercises process, data and file-identity failures.
	t.Parallel()

	for _, mode := range []string{"status", "null", "malformed", "alias", "hardlink"} {
		fsys := testfs.NewReal(t)
		raw := fsys.WriteFile("raw.json", []byte("prior raw"))

		report := fsys.Path("report.sarif")
		if mode == "alias" {
			report = fsys.Path("./raw.json")
		}

		if mode == "hardlink" {
			if err := os.Link(raw, report); err != nil {
				t.Fatal(err)
			}
		}

		scanner := &scanRecorder{run: func(_ context.Context, args []string) (int, error) {
			body := `{"Results":[]}`
			if mode == "null" {
				body = "null"
			}

			if mode == "malformed" {
				body = "{"
			}

			if err := os.WriteFile(scanOutput(args), []byte(body), 0o600); err != nil {
				return 0, err
			}

			if mode == "status" {
				return 7, nil
			}

			return 0, nil
		}}

		var out bytes.Buffer

		err := appsecurity.ScanContainer(t.Context(), scanner, &out, &out, output.Annotator{}, appsecurity.ScanContainerInput{ImageRef: "image", JSONFile: raw, SARIFFile: report, GitLabReportFile: fsys.Path("gitlab.json")})
		if err == nil {
			t.Fatalf("mode=%s passed", mode)
		}

		if mode == "status" && !errors.Is(err, errs.ErrDependencyUnavailable) {
			t.Fatalf("status err=%v", err)
		}

		if mode == "null" || mode == "malformed" {
			if !errors.Is(err, errs.ErrMalformedInput) {
				t.Fatalf("shape err=%v", err)
			}
		}

		if mode == "alias" || mode == "hardlink" {
			if len(scanner.calls) != 0 || out.Len() != 0 || string(fsys.ReadFile("raw.json")) != "prior raw" {
				t.Fatalf("alias refusal had effects: %v", err)
			}
		}
	}
}

type comparisonGit struct {
	root             string
	calls            [][]string
	cleanup          error
	cancelledCleanup bool
}

func (g *comparisonGit) Run(ctx context.Context, args ...string) (string, error) {
	g.calls = append(g.calls, slices.Clone(args))
	if args[0] == "rev-parse" {
		return g.root, nil
	}

	if args[0] == "worktree" && args[1] == "remove" {
		g.cancelledCleanup = ctx.Err() != nil

		return "", g.cleanup
	}

	return "", nil
}

var errComparisonBoundary = errors.New("owned cleanup failure")

func TestDependencyBoundary_SymmetricScopeStatusAndCleanup(t *testing.T) { //nolint:gocognit,gocyclo // one scope-aware fake distinguishes sibling suppression and every base-scan/cleanup boundary.
	fsys := testfs.NewReal(t)
	fsys.MkdirAll("component")
	fsys.Chdir()

	for _, mode := range []string{"relative", "absolute", "outside", "head status", "base status", "base malformed", "cleanup", "both", "cancel"} {
		ctx, cancel := context.WithCancel(t.Context())

		git := &comparisonGit{root: fsys.Root}
		if mode == "cleanup" || mode == "both" {
			git.cleanup = errComparisonBoundary
		}

		var scanner *scanRecorder

		scanner = &scanRecorder{run: func(_ context.Context, args []string) (int, error) {
			index := len(scanner.calls)

			if args[0] == "convert" {
				return 9, nil
			}

			body := boundaryTrivyJSON
			if index == 2 {
				body = `{"Results":[]}`
				if filepath.Base(args[len(args)-1]) != "component" {
					body = boundaryTrivyJSON
				}

				if mode == "base malformed" || mode == "both" {
					body = "null"
				}

				if mode == "cancel" {
					cancel()

					return 0, context.Canceled
				}
			}

			if err := os.WriteFile(scanOutput(args), []byte(body), 0o600); err != nil {
				return 0, err
			}

			if mode == "head status" && index == 1 || mode == "base status" && index == 2 {
				return 7, nil
			}

			return 0, nil
		}}

		path := "component"
		if mode == "absolute" {
			path = fsys.Path("component")
		}

		if mode == "outside" {
			path = filepath.Dir(fsys.Root)
		}

		var out, diagnostics bytes.Buffer

		summary := &appSummaryBuf{}
		err := appsecurity.ScanDependencies(ctx, scanner, git, summary, &out, &out, output.NewAnnotator(&diagnostics, output.FormatText), appsecurity.ScanDependenciesInput{ScanPath: path, ScanMode: domainsecurity.ScanModeDiff, BaseRef: "main", SARIFFile: fsys.Path("dep.sarif"), GitLabDepFile: fsys.Path("dep.json")})

		cancel()

		if mode == "relative" || mode == "absolute" { //nolint:nestif // successful diff controls assert argv, reports and complete finding sets together.
			if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "2 new vulnerabilities") || !strings.Contains(diagnostics.String(), "status 9") {
				t.Fatalf("mode=%s err=%v diagnostics=%s", mode, err, &diagnostics)
			}

			for _, id := range []string{"CVE-A", "CVE-B"} {
				if !strings.Contains(summary.buf.String(), id) {
					t.Fatalf("missing %s: %s", id, &summary.buf)
				}
			}

			if len(scanner.calls) != 3 || scanner.calls[0].args[len(scanner.calls[0].args)-1] != fsys.Path("component") {
				t.Fatalf("HEAD calls=%v", scanner.calls)
			}

			basePath := scanner.calls[1].args[len(scanner.calls[1].args)-1]
			if filepath.Base(basePath) != "component" || filepath.Base(filepath.Dir(basePath)) != "base-worktree" {
				t.Fatalf("base scope=%s", basePath)
			}

			workDir := filepath.Dir(scanOutput(scanner.calls[0].args))
			for index, target := range []string{fsys.Path("component"), filepath.Join(workDir, "base-worktree", "component")} {
				file := "head-vulns.json"
				if index == 1 {
					file = "base-vulns.json"
				}

				want := []string{"fs", "--format", "json", "--severity", "CRITICAL", "--scanners", "vuln", "--output", filepath.Join(workDir, file), target}
				if !slices.Equal(scanner.calls[index].args, want) {
					t.Fatalf("scan argv=%v want=%v", scanner.calls[index].args, want)
				}
			}
		} else {
			if err == nil || summary.buf.Len() != 0 {
				t.Fatalf("mode=%s err=%v summary=%s", mode, err, &summary.buf)
			}

			if mode == "outside" {
				if len(scanner.calls) != 0 || out.Len() != 0 {
					t.Fatal("outside scope had effects")
				}

				continue
			}

			if strings.Contains(mode, "status") && !errors.Is(err, errs.ErrDependencyUnavailable) {
				t.Fatalf("status err=%v", err)
			}

			if (mode == "cleanup" || mode == "both") && !errors.Is(err, errComparisonBoundary) {
				t.Fatalf("cleanup err=%v", err)
			}

			if mode == "both" && !errors.Is(err, errs.ErrMalformedInput) {
				t.Fatalf("primary failure lost: %v", err)
			}
		}

		if mode != "head status" {
			last := git.calls[len(git.calls)-1]

			workDir := filepath.Dir(scanOutput(scanner.calls[0].args))
			if !reflect.DeepEqual(last, []string{"worktree", "remove", "-f", filepath.Join(workDir, "base-worktree")}) || git.cancelledCleanup {
				t.Fatalf("cleanup=%v cancelled=%v", last, git.cancelledCleanup)
			}
		}
	}
}

func TestDependencyBoundary_AllHeadFindingRows(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	summary := &appSummaryBuf{}
	scanner := &fakeTrivy{writePerCall: []string{boundaryTrivyJSON}}

	err := appsecurity.ScanDependencies(t.Context(), scanner, &fakeGit{}, summary, io.Discard, io.Discard, output.Annotator{}, appsecurity.ScanDependenciesInput{ScanMode: domainsecurity.ScanModeFull, SARIFFile: fsys.Path("dep.sarif"), GitLabDepFile: fsys.Path("dep.json")})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "2 new vulnerabilities") {
		t.Fatalf("gate=%v", err)
	}

	for _, row := range []string{"| CVE-A | HIGH | alpha | 1.0 | 1.1 |", "| CVE-B | CRITICAL | beta | 2.0 | 2.1 |"} {
		if !strings.Contains(summary.buf.String(), row) {
			t.Fatalf("missing %s in %s", row, &summary.buf)
		}
	}
}

// TestContainerReportBoundary_OneFailedReportLeavesItsSibling makes one
// derived report impossible to install, in each direction. The two
// derivations are documented as independent best-effort steps: the other
// report still reaches its destination with every finding, the failure is
// annotated rather than silent, the raw report is published, and the verdict
// is still the gate's.
func TestContainerReportBoundary_OneFailedReportLeavesItsSibling(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("permission bits do not restrict root")
	}

	for _, blocked := range []string{"sarif", "gitlab"} {
		t.Run(blocked, func(t *testing.T) {
			t.Parallel()

			fsys := testfs.NewReal(t)
			readOnly := fsys.MkdirAll("read-only")

			in := appsecurity.ScanContainerInput{
				ImageRef: "registry.example/owner/image:tag", JSONFile: fsys.Path("raw.json"),
				SARIFFile: fsys.Path("report.sarif"), GitLabReportFile: fsys.Path("report.json"),
			}
			if blocked == "sarif" {
				in.SARIFFile = filepath.Join(readOnly, "report.sarif")
			} else {
				in.GitLabReportFile = filepath.Join(readOnly, "report.json")
			}

			if err := os.Chmod(readOnly, 0o500); err != nil { //nolint:gosec // a read-only directory keeps its search bit.
				t.Fatal(err)
			}

			t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) }) //nolint:gosec // a directory needs its search bit; restores access so the temp dir can be removed.

			scanner := &scanRecorder{run: func(_ context.Context, args []string) (int, error) {
				return 0, os.WriteFile(scanOutput(args), []byte(boundaryTrivyJSON), 0o600)
			}}

			var diagnostics bytes.Buffer

			err := appsecurity.ScanContainer(t.Context(), scanner, io.Discard, io.Discard, output.NewAnnotator(&diagnostics, output.FormatGitHub), in)
			if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "Found 2 container vulnerabilities") {
				t.Errorf("err = %v, want the gate's verdict", err)
			}

			if !json.Valid(fsys.ReadFile("raw.json")) {
				t.Error("the raw report was not published")
			}

			assertSiblingReport(t, fsys, blocked, diagnostics.String())

			if entries, readErr := os.ReadDir(readOnly); readErr != nil || len(entries) != 0 {
				t.Errorf("blocked directory holds %v (err %v), want nothing", entries, readErr)
			}
		})
	}
}

// assertSiblingReport checks the report that was not blocked carries both
// findings and the blocked one's failure was annotated.
func assertSiblingReport(t *testing.T, fsys *testfs.Real, blocked, diagnostics string) {
	t.Helper()

	if blocked == "sarif" {
		if !strings.Contains(diagnostics, "::warning::publish SARIF failed") {
			t.Errorf("diagnostics = %q, want the SARIF failure annotated", diagnostics)
		}

		var gitlab domainsecurity.GitLabReport
		if err := json.Unmarshal(fsys.ReadFile("report.json"), &gitlab); err != nil || len(gitlab.Vulnerabilities) != 2 {
			t.Errorf("GitLab report = %+v (err %v), want both findings", gitlab, err)
		}

		return
	}

	if !strings.Contains(diagnostics, "::warning::publish GitLab container report failed") {
		t.Errorf("diagnostics = %q, want the GitLab failure annotated", diagnostics)
	}

	var sarif struct {
		Runs []struct {
			Results []json.RawMessage `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(fsys.ReadFile("report.sarif"), &sarif); err != nil || len(sarif.Runs) != 1 || len(sarif.Runs[0].Results) != 2 {
		t.Errorf("SARIF = %+v (err %v), want both findings", sarif, err)
	}
}
