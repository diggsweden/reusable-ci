// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/security"
)

func TestMapTrivyFailSeverity(t *testing.T) {
	cases := map[string]string{
		"critical": "CRITICAL", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"HIGH":     "CRITICAL,HIGH", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"Moderate": "CRITICAL,HIGH,MEDIUM", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"low":      "CRITICAL,HIGH,MEDIUM,LOW", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"weird":    "CRITICAL", // default
	}
	for in, want := range cases {
		if got := security.MapTrivyFailSeverity(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestIsKnownTrivyFailSeverity(t *testing.T) {
	t.Parallel()

	for _, level := range []string{"critical", " HIGH ", "Moderate", "low"} {
		if !security.IsKnownTrivyFailSeverity(level) {
			t.Errorf("%q should be known", level)
		}
	}

	for _, level := range []string{"", "medium", "unknown"} { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		if security.IsKnownTrivyFailSeverity(level) {
			t.Errorf("%q should not be known", level)
		}
	}
}

func TestExtractTrivyVulnIDs(t *testing.T) {
	body := []byte(`{
  "Results": [
    {"Vulnerabilities": [
      {"VulnerabilityID":"CVE-2024-2"},
      {"VulnerabilityID":"CVE-2024-1"},
      {"VulnerabilityID":"CVE-2024-1"}
    ]},
    {"Vulnerabilities": [
      {"VulnerabilityID":"CVE-2024-3"}
    ]}
  ]
}`)

	got, err := security.ExtractTrivyVulnIDs(body)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"CVE-2024-1", "CVE-2024-2", "CVE-2024-3"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	for i := range got {
		if got[i] != want[i] {
			t.Errorf("at %d: %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExtractTrivyVulnIDs_EmptyBody(t *testing.T) {
	got, err := security.ExtractTrivyVulnIDs(nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestDiffNewIDs(t *testing.T) {
	base := []string{"CVE-1", "CVE-2", "CVE-3"} //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	head := []string{"CVE-2", "CVE-3", "CVE-4", "CVE-5"}
	got := security.DiffNewIDs(base, head)

	want := []string{"CVE-4", "CVE-5"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDiffNewIDs_EmptyBaseReturnsAllHead(t *testing.T) {
	got := security.DiffNewIDs(nil, []string{"CVE-1"})
	if len(got) != 1 || got[0] != "CVE-1" {
		t.Errorf("got %v", got)
	}
}

func TestFilterVulnRowsByID(t *testing.T) {
	body := []byte(`{
  "Results": [{
    "Vulnerabilities": [
      {"VulnerabilityID":"CVE-1","Severity":"HIGH","PkgName":"foo","InstalledVersion":"1.0","FixedVersion":"1.1"},
      {"VulnerabilityID":"CVE-2","Severity":"CRITICAL","PkgName":"bar","InstalledVersion":"2.0"},
      {"VulnerabilityID":"CVE-3","Severity":"LOW","PkgName":"baz","InstalledVersion":"3.0","FixedVersion":"3.1"}
    ]
  }]
}`)

	got, err := security.FilterVulnRowsByID(body, []string{"CVE-1", "CVE-2"})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}

	if got[0].ID != "CVE-1" || got[0].Fixed != "1.1" {
		t.Errorf("row 0 = %+v", got[0])
	}

	if got[1].ID != "CVE-2" || got[1].Fixed != "—" {
		t.Errorf("row 1 = %+v (FixedVersion empty should render as em-dash)", got[1])
	}
}

func TestRenderScanDepsSummary_WithFindings(t *testing.T) {
	got := security.RenderScanDepsSummary(security.ScanDepsSummaryInput{
		Mode:           "diff",
		FailOnSeverity: "high",
		SeverityFilter: "CRITICAL,HIGH",
		NewCount:       1,
		NewRows: []security.VulnRow{
			{ID: "CVE-1", Severity: "HIGH", Package: "foo", Installed: "1.0", Fixed: "1.1"},
		},
	})
	for _, want := range []string{
		"## Dependency Vulnerability Scan",
		"| Scanner | Trivy |",
		"| Mode | diff |",
		"| Fail threshold | high |",
		"| Severity filter | CRITICAL,HIGH |",
		"| New vulnerabilities | **1** |",
		"### New Vulnerabilities",
		"| CVE-1 | HIGH | foo | 1.0 | 1.1 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderScanDepsSummary_ClearReport(t *testing.T) {
	got := security.RenderScanDepsSummary(security.ScanDepsSummaryInput{
		Mode:           "full",
		FailOnSeverity: "critical",
		SeverityFilter: "CRITICAL",
		NewCount:       0,
	})
	if !strings.Contains(got, "> No new vulnerabilities found.") {
		t.Errorf("missing clean-report line:\n%s", got)
	}

	if strings.Contains(got, "### New Vulnerabilities") {
		t.Errorf("did not expect details table:\n%s", got)
	}
}
