// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
)

// TestParseDepSeverity_TrivyFilterWidensByBandAndDefaultsToCritical: each band
// includes everything above it, and anything unrecognised narrows to CRITICAL
// rather than opening the gate.
//
// Named for MapTrivyFailSeverity until now, which has not existed for some
// time -- so the only thing a grep for the real subject found was the
// implementation, never its test.
func TestParseDepSeverity_TrivyFilterWidensByBandAndDefaultsToCritical(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"critical": "CRITICAL",                 //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"HIGH":     "CRITICAL,HIGH",            //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"Moderate": "CRITICAL,HIGH,MEDIUM",     //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"low":      "CRITICAL,HIGH,MEDIUM,LOW", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"weird":    "CRITICAL",                 // default

		// "medium" is what trivy itself calls this band, and it is the
		// spelling a caller most often reaches for; it now resolves to
		// "moderate" rather than falling through to the narrow default.
		"medium": "CRITICAL,HIGH,MEDIUM",

		// "CRITICAL,HIGH" is the grammar the sibling
		// `security scan container --fail-on-severity` documents for the
		// very same flag name. It is still not a valid value here, and
		// the app layer now REFUSES it rather than narrowing the gate to
		// CRITICAL -- see TestScanDependencies_UnknownSeverityIsRefused.
		// TrivyFilter's own fallback is unchanged and unreachable from
		// that path.
		"CRITICAL,HIGH": "CRITICAL",
	}
	for in, want := range cases {
		if got := security.ParseDepSeverity(in).TrivyFilter(); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

// TestParseDepSeverity_IsKnownAcceptsBandsAndRejectsLists: surrounding space
// and mixed case are accepted, a comma-separated list is not -- that grammar
// belongs to the container scanner's flag of the same name, and silently
// accepting it here would narrow the gate instead of failing.
func TestParseDepSeverity_IsKnownAcceptsBandsAndRejectsLists(t *testing.T) {
	t.Parallel()

	for _, level := range []string{"critical", " HIGH ", "Moderate", "low", "medium", " Medium "} {
		if !security.ParseDepSeverity(level).IsKnown() {
			t.Errorf("%q should be known", level)
		}
	}

	for _, level := range []string{"", "unknown", "CRITICAL,HIGH", "error"} { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		if security.ParseDepSeverity(level).IsKnown() {
			t.Errorf("%q should not be known", level)
		}
	}
}

// TestExtractTrivyVulnIDs_DedupesAndSorts: the same CVE reported by two
// results is one id, and the order is stable so a diff against a base scan
// compares like with like.
func TestExtractTrivyVulnIDs_DedupesAndSorts(t *testing.T) {
	t.Parallel()

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
	if !slices.Equal(got, want) {
		t.Errorf("ids = %v, want %v", got, want)
	}
}

func TestExtractTrivyVulnIDs_EmptyBody(t *testing.T) {
	t.Parallel()

	got, err := security.ExtractTrivyVulnIDs(nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

// TestDiffNewFindings_ComparesPackageAndTargetNotJustTheID covers the gate
// itself. It compared bare IDs, so a pull request adding a second package
// affected by a CVE the base already had elsewhere -- in another package, or
// in another lockfile of the same repository -- reported nothing new and
// passed. Pre-existing findings still are not the author's to fix, and an
// upgrade that stays affected by the same CVE is not new either.
func TestDiffNewFindings_ComparesPackageAndTargetNotJustTheID(t *testing.T) {
	t.Parallel()

	base := []byte(`{"Results":[
{"Target":"service-a/package-lock.json","Vulnerabilities":[
  {"VulnerabilityID":"CVE-1","PkgName":"lodash","InstalledVersion":"4.17.0"},
  {"VulnerabilityID":"CVE-2","PkgName":"minimist","InstalledVersion":"1.0.0"}]}]}`)
	head := []byte(`{"Results":[
{"Target":"service-a/package-lock.json","Vulnerabilities":[
  {"VulnerabilityID":"CVE-1","PkgName":"lodash","InstalledVersion":"4.17.1"},
  {"VulnerabilityID":"CVE-1","PkgName":"lodash-es","InstalledVersion":"4.17.0"},
  {"VulnerabilityID":"CVE-2","PkgName":"minimist","InstalledVersion":"1.0.0"}]},
{"Target":"service-b/package-lock.json","Vulnerabilities":[
  {"VulnerabilityID":"CVE-1","PkgName":"lodash","InstalledVersion":"4.17.0"},
  {"VulnerabilityID":"CVE-3","PkgName":"axios","InstalledVersion":"0.1.0"},
  {"VulnerabilityID":"","PkgName":"unnamed","InstalledVersion":"0.0.1"}]}]}`)

	baseFindings, err := security.ExtractTrivyFindings(base)
	if err != nil {
		t.Fatal(err)
	}

	headFindings, err := security.ExtractTrivyFindings(head)
	if err != nil {
		t.Fatal(err)
	}

	want := []security.VulnFinding{
		{Target: "service-a/package-lock.json", Package: "lodash-es", ID: "CVE-1"},
		{Target: "service-b/package-lock.json", Package: "axios", ID: "CVE-3"},
		{Target: "service-b/package-lock.json", Package: "lodash", ID: "CVE-1"},
	}
	if got := security.DiffNewFindings(baseFindings, headFindings); !slices.Equal(got, want) {
		t.Errorf("new findings =\n%+v\nwant\n%+v", got, want)
	}

	if got := security.DiffNewFindings(nil, headFindings); !slices.Equal(got, headFindings) {
		t.Errorf("with no base, new findings = %+v, want every head finding %+v", got, headFindings)
	}
}

// TestFilterVulnRows_KeepsRowsForTheRequestedFindings: the summary shows a
// row for each requested finding, including every installed version of it,
// and nothing for the same ID in a package that was not requested. A missing
// fixed version renders as an em-dash rather than an empty cell.
func TestFilterVulnRows_KeepsRowsForTheRequestedFindings(t *testing.T) {
	t.Parallel()

	body := []byte(`{"Results":[{"Target":"go.mod","Vulnerabilities":[
  {"VulnerabilityID":"CVE-1","Severity":"HIGH","PkgName":"foo","InstalledVersion":"1.0","FixedVersion":"1.1"},
  {"VulnerabilityID":"CVE-1","Severity":"HIGH","PkgName":"foo","InstalledVersion":"1.2","FixedVersion":"1.3"},
  {"VulnerabilityID":"CVE-1","Severity":"HIGH","PkgName":"other","InstalledVersion":"9.0"},
  {"VulnerabilityID":"CVE-2","Severity":"CRITICAL","PkgName":"bar","InstalledVersion":"2.0"}]}]}`)

	got, err := security.FilterVulnRows(body, []security.VulnFinding{
		{Target: "go.mod", Package: "foo", ID: "CVE-1"},
		{Target: "go.mod", Package: "bar", ID: "CVE-2"},
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []security.VulnRow{
		{ID: "CVE-1", Severity: "HIGH", Package: "foo", Installed: "1.0", Fixed: "1.1"},
		{ID: "CVE-1", Severity: "HIGH", Package: "foo", Installed: "1.2", Fixed: "1.3"},
		{ID: "CVE-2", Severity: "CRITICAL", Package: "bar", Installed: "2.0", Fixed: "—"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("rows =\n%+v\nwant\n%+v", got, want)
	}
}

func TestRenderScanDepsSummary_WithFindings(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
