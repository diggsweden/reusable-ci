// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/domain/security"
)

func TestNormalizeOpengrepFailSeverity(t *testing.T) {
	cases := []struct {
		in   string
		want security.OpengrepSeverity
	}{
		{"none", security.OpengrepSeverityNone},
		{"OFF", security.OpengrepSeverityNone},
		{"Never", security.OpengrepSeverityNone},
		{"low", security.OpengrepSeverityLow},
		{"INFO", security.OpengrepSeverityLow},
		{"medium", security.OpengrepSeverityMedium},
		{"Moderate", security.OpengrepSeverityMedium},
		{"Warning", security.OpengrepSeverityMedium},
		{"high", security.OpengrepSeverityHigh},
		{"CRITICAL", security.OpengrepSeverityHigh},
		{"error", security.OpengrepSeverityHigh},
		{"  high  ", security.OpengrepSeverityHigh},
	}
	for _, c := range cases {
		got, err := security.NormalizeOpengrepFailSeverity(c.in)
		if err != nil {
			t.Errorf("%q: unexpected error %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("%q → %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeOpengrepFailSeverity_Unknown(t *testing.T) {
	_, err := security.NormalizeOpengrepFailSeverity("severe")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOpengrepHasFindingsMeetingThreshold(t *testing.T) {
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
			got := security.OpengrepHasFindingsMeetingThreshold(c.threshold, c.findings, c.errors, c.warnings)
			if got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestCountOpengrepFindings(t *testing.T) {
	body := `{"results":[
{"check_id":"a","severity":"ERROR"},
{"check_id":"b","severity":"WARNING"},
{"check_id":"c","severity":"INFO"},
{"check_id":"d","severity":"ERROR"}
]}`
	c := security.CountOpengrepFindings(body)
	if c.FindingsTotal != 4 || c.ErrorTotal != 2 || c.WarningTotal != 1 || c.InfoTotal != 1 {
		t.Errorf("counts = %+v", c)
	}
}

func TestRenderOpengrepSummary_BlockedByThreshold(t *testing.T) {
	md, result := security.RenderOpengrepSummary(security.OpengrepSummaryInput{
		Config:           "p/default",
		TargetPath:       ".",
		FailOnSeverity:   security.OpengrepSeverityHigh,
		Counts:           security.OpengrepCounts{FindingsTotal: 3, ErrorTotal: 2, WarningTotal: 1},
		ThresholdFailure: true,
		Platform:         security.OpengrepPlatformContext{Platform: provider.PlatformGitHub, HasCodeScanningToken: true, RunURL: "https://example.com/run"},
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
	md, result := security.RenderOpengrepSummary(security.OpengrepSummaryInput{
		FailOnSeverity:   security.OpengrepSeverityHigh,
		Counts:           security.OpengrepCounts{FindingsTotal: 2, WarningTotal: 1, InfoTotal: 1},
		ThresholdFailure: false,
	})
	if result != "success" {
		t.Errorf("result = %q", result)
	}
	if !strings.Contains(md, "Completed with findings below threshold `high`.") {
		t.Errorf("missing below-threshold line:\n%s", md)
	}
}

func TestRenderOpengrepSummary_CleanPass(t *testing.T) {
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
	md, result := security.RenderOpengrepSummary(security.OpengrepSummaryInput{
		Config:         "p/default",
		TargetPath:     ".",
		FailOnSeverity: security.OpengrepSeverityHigh,
		Counts:         security.OpengrepCounts{},
		Platform: security.OpengrepPlatformContext{
			Platform:             provider.PlatformGitHub,
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
	md, _ := security.RenderOpengrepSummary(security.OpengrepSummaryInput{
		Counts:      security.OpengrepCounts{FindingsTotal: 1, ErrorTotal: 1},
		TextExcerpt: "rule X matched at foo.go:42",
	})
	if !strings.Contains(md, "<details>") || !strings.Contains(md, "rule X matched at foo.go:42") {
		t.Errorf("expected embedded excerpt:\n%s", md)
	}
}

func TestRenderOpengrepFailureSummary(t *testing.T) {
	md := security.RenderOpengrepFailureSummary(security.OpengrepFailureSummaryInput{
		Config:     "p/default",
		TargetPath: ".",
		ExitCode:   2,
		Platform:   security.OpengrepPlatformContext{Platform: provider.PlatformGitLab},
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

func TestParseConfigList(t *testing.T) {
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
	if _, err := security.ParseConfigList(""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := security.ParseConfigList(",,  ,"); err == nil {
		t.Fatal("expected error on whitespace-only entries")
	}
}

func TestHeadN(t *testing.T) {
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
