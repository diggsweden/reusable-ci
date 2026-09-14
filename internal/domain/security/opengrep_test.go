// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
)

func TestNormalizeOpengrepFailSeverity_MapsKnownSpellingsIgnoringCaseAndSpace(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want security.OpengrepSeverity
	}{
		{"none", security.OpengrepSeverityNone},
		{"OFF", security.OpengrepSeverityNone},
		{"Never", security.OpengrepSeverityNone},
		{"low", security.OpengrepSeverityLow}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"INFO", security.OpengrepSeverityLow},
		{"medium", security.OpengrepSeverityMedium},   //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"Moderate", security.OpengrepSeverityMedium}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"Warning", security.OpengrepSeverityMedium},
		{"high", security.OpengrepSeverityHigh},
		{"CRITICAL", security.OpengrepSeverityHigh}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{"error", security.OpengrepSeverityHigh},
		{"  high  ", security.OpengrepSeverityHigh},
	}
	for _, c := range cases { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		got, err := security.NormalizeOpengrepFailSeverity(c.in)
		if err != nil {
			t.Errorf("%q: unexpected error %v", c.in, err)
		}

		if got != c.want {
			t.Errorf("%q → %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeOpengrepFailSeverity_RejectsUnknownSpelling(t *testing.T) {
	t.Parallel()

	_, err := security.NormalizeOpengrepFailSeverity("severe")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	// The rejected spelling has to appear, or the operator cannot tell
	// which of several severity flags they got wrong.
	if !strings.Contains(err.Error(), "severe") {
		t.Errorf("err = %v, want it to quote the rejected value", err)
	}
}

func TestOpengrepHasFindingsMeetingThreshold_GatesAtOrAboveTheThreshold(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		threshold security.OpengrepSeverity
		findings  int
		errors    int
		warnings  int
		want      bool
	}{
		{"none never fails", security.OpengrepSeverityNone, 100, 100, 100, false},
		{"low fails on any finding", security.OpengrepSeverityLow, 1, 0, 0, true},
		{"low passes on zero", security.OpengrepSeverityLow, 0, 0, 0, false},
		{"medium fails on error", security.OpengrepSeverityMedium, 5, 1, 0, true},
		{"medium fails on warning", security.OpengrepSeverityMedium, 5, 0, 1, true},
		{"medium passes when only info", security.OpengrepSeverityMedium, 5, 0, 0, false},
		{"high fails on error only", security.OpengrepSeverityHigh, 10, 1, 5, true},
		{"high passes on warning only", security.OpengrepSeverityHigh, 10, 0, 5, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := security.OpengrepHasFindingsMeetingThreshold(c.threshold, c.findings, c.errors, c.warnings)
			if got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestCountOpengrepFindings_TalliesEachSeverity(t *testing.T) {
	t.Parallel()

	body := `{"results":[
{"check_id":"a","extra":{"severity":"ERROR"}},
{"check_id":"b","extra":{"severity":"WARNING"}},
{"check_id":"c","extra":{"severity":"INFO"}},
{"check_id":"d","extra":{"severity":"ERROR"}}
]}`

	want := security.OpengrepCounts{FindingsTotal: 4, ErrorTotal: 2, WarningTotal: 1, InfoTotal: 1}
	if got, err := security.CountOpengrepFindings(body); err != nil || got != want {
		t.Errorf("counts = %+v, want %+v", got, want)
	}
}

func TestRenderOpengrepSummary_BlockedByThreshold(t *testing.T) {
	t.Parallel()

	md, result := security.RenderOpengrepSummary(security.OpengrepSummaryInput{
		Config:           "p/default", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		TargetPath:       ".",
		FailOnSeverity:   security.OpengrepSeverityHigh,
		Counts:           security.OpengrepCounts{FindingsTotal: 3, ErrorTotal: 2, WarningTotal: 1},
		ThresholdFailure: true,
		Platform:         security.OpengrepPlatformContext{Platform: provider.ForgeGitHub, HasCodeScanningToken: true, RunURL: "https://example.com/run"},
	})
	if result != "failure" {
		t.Errorf("result = %q", result)
	}

	for _, want := range []string{
		"## OpenGrep SAST",
		"Blocked by findings meeting threshold `high`.",
		"| Findings | 3 |",
		"| Fail Threshold | high |",
		"| Security / Code Scanning | SARIF generated, upload configured |",
		"| Workflow Run | [View workflow run](https://example.com/run) |",
		"| Scan Result | failure |",
		"| ERROR | 2 |",
		"| WARNING | 1 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
}

func TestRenderOpengrepSummary_PassedWithFindingsBelowThreshold(t *testing.T) {
	t.Parallel()

	md, result := security.RenderOpengrepSummary(security.OpengrepSummaryInput{
		FailOnSeverity:   security.OpengrepSeverityHigh,
		Counts:           security.OpengrepCounts{FindingsTotal: 2, WarningTotal: 1, InfoTotal: 1},
		ThresholdFailure: false,
	})
	if result != "success" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		t.Errorf("result = %q", result)
	}

	if !strings.Contains(md, "Completed with findings below threshold `high`.") {
		t.Errorf("missing below-threshold line:\n%s", md)
	}
}

func TestRenderOpengrepSummary_CleanPass(t *testing.T) {
	t.Parallel()

	md, result := security.RenderOpengrepSummary(security.OpengrepSummaryInput{
		FailOnSeverity: security.OpengrepSeverityHigh,
		Counts:         security.OpengrepCounts{},
	})
	if result != "success" {
		t.Errorf("result = %q", result)
	}

	if !strings.Contains(md, "Passed with `0` findings.") {
		t.Errorf("missing clean-pass line:\n%s", md)
	}
	// Severity sub-table must be omitted when no findings.
	if strings.Contains(md, "| Severity | Count |") {
		t.Errorf("did not expect severity sub-table:\n%s", md)
	}
}

func TestRenderOpengrepSummary_GitHubWithoutCodeScanningToken(t *testing.T) {
	t.Parallel()

	md, result := security.RenderOpengrepSummary(security.OpengrepSummaryInput{
		Config:         "p/default",
		TargetPath:     ".",
		FailOnSeverity: security.OpengrepSeverityHigh,
		Counts:         security.OpengrepCounts{},
		Platform: security.OpengrepPlatformContext{
			Platform:             provider.ForgeGitHub,
			HasCodeScanningToken: false,
			RunURL:               "https://example.com/run",
		},
	})
	if result != "success" {
		t.Errorf("result = %q", result)
	}

	for _, want := range []string{
		"Passed with `0` findings.",
		"SARIF generated, upload not configured",
		"[View workflow run](https://example.com/run)",
		"Configure CODE_SCANNING_TOKEN to publish results in Security / Code Scanning.",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
}

func TestRenderOpengrepSummary_EmbedsExcerpt(t *testing.T) {
	t.Parallel()

	md, _ := security.RenderOpengrepSummary(security.OpengrepSummaryInput{
		Counts:      security.OpengrepCounts{FindingsTotal: 1, ErrorTotal: 1},
		TextExcerpt: "rule X matched at foo.go:42",
	})
	if !strings.Contains(md, "<details>") || !strings.Contains(md, "rule X matched at foo.go:42") {
		t.Errorf("expected embedded excerpt:\n%s", md)
	}
}

func TestRenderOpengrepFailureSummary_NamesTheExitCodeAndScanResult(t *testing.T) {
	t.Parallel()

	md := security.RenderOpengrepFailureSummary(security.OpengrepFailureSummaryInput{
		Config:     "p/default",
		TargetPath: ".",
		ExitCode:   2,
		Platform:   security.OpengrepPlatformContext{Platform: provider.ForgeGitLab},
	})
	for _, want := range []string{
		"## OpenGrep SAST",
		"OpenGrep exited with status 2 before a complete result set was produced.",
		"| Rules | p/default |",
		"| Security / Code Scanning | GitLab SAST artifact generated |",
		"| Scan Result | failure |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
}

func TestRenderOpengrepSummary_Forgejo(t *testing.T) {
	t.Parallel()

	md, result := security.RenderOpengrepSummary(security.OpengrepSummaryInput{
		Config:         "p/default",
		TargetPath:     ".",
		FailOnSeverity: security.OpengrepSeverityHigh,
		Counts:         security.OpengrepCounts{},
		Platform: security.OpengrepPlatformContext{
			Platform: provider.ForgeForgejo,
			RunURL:   "https://codeberg.org/owner/repo/actions/runs/1",
		},
	})
	if result != "success" {
		t.Errorf("result = %q", result)
	}

	for _, want := range []string{
		"SARIF artifact generated (no Code Scanning ingestion)",
		"Forgejo has no Code Scanning ingestion; the SARIF report is saved as a workflow artifact for external tooling.",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
}

func TestParseConfigList_TrimsAndDropsEmptyEntries(t *testing.T) {
	t.Parallel()

	got, err := security.ParseConfigList(" p/default ,  custom-rules.yaml,  ")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"p/default", "custom-rules.yaml"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseConfigList_RejectsEmpty(t *testing.T) {
	t.Parallel()

	// Both spellings of "no configs" are the caller's mistake, not a
	// rule failure: a scan with no rules would silently pass everything.
	for _, raw := range []string{"", ",,  ,"} {
		t.Run("raw="+raw, func(t *testing.T) {
			t.Parallel()

			if _, err := security.ParseConfigList(raw); !errors.Is(err, errs.ErrUsage) {
				t.Errorf("ParseConfigList(%q): err = %v, want ErrUsage", raw, err)
			}
		})
	}
}

func TestHeadN_ReturnsAtMostNLines(t *testing.T) {
	t.Parallel()

	in := "1\n2\n3\n4\n5"
	if got := security.HeadN(in, 3); got != "1\n2\n3" {
		t.Errorf("HeadN(3) = %q", got)
	}

	if got := security.HeadN(in, 100); got != in {
		t.Errorf("HeadN(100) should return whole input, got %q", got)
	}

	if got := security.HeadN(in, 0); got != "" {
		t.Errorf("HeadN(0) = %q, want empty", got)
	}
}
