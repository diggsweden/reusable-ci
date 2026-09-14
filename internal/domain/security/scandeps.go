// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// ScanMode is the dependency-scan comparison mode requested by the
// workflow. "diff" compares HEAD vulnerabilities against the PR base
// ref; "full" reports every HEAD finding.
type ScanMode string

// Recognised ScanMode values.
const (
	ScanModeDiff ScanMode = "diff"
	ScanModeFull ScanMode = "full"
)

// DepSeverity is the user-facing fail-on threshold for the dependency
// scanner. Distinct from OpengrepSeverity (which is the static-analysis
// scanner's input vocabulary) because the levels themselves differ
// ("moderate" here, "medium" there) and the two should not alias.
//
// Single source of truth: the CLI flag's default Value, the app-layer
// cmp.Or fallback, and the switch in DepSeverity.TrivyFilter all reference
// these constants.
type DepSeverity string

// Recognised DepSeverity values.
const (
	DepSeverityLow      DepSeverity = "low"
	DepSeverityModerate DepSeverity = "moderate"
	DepSeverityHigh     DepSeverity = "high"
	// DepSeverityCritical is the default fail-on threshold — matches
	// the bash trivy default.
	DepSeverityCritical DepSeverity = "critical"
)

// Default file paths for the Trivy scan outputs. Single source of
// truth: the CLI flag defaults and the app-layer empty-string
// fallbacks reference these constants.
//
// The dependency- and container-scan filenames are kept distinct so a
// repo running both scanners in the same workspace doesn't clobber
// one report with the other.
const (
	// Dependency-scan outputs.
	DefaultTrivySARIFFile     = "trivy-dependency-results.sarif"
	DefaultTrivyGitLabDepFile = "gl-dependency-scanning-report.json"

	// Container-scan outputs.
	DefaultTrivyContainerJSONFile   = "trivy-results.json"
	DefaultTrivyContainerSARIFFile  = "trivy-results.sarif"
	DefaultTrivyGitLabContainerFile = "gl-container-scanning-report.json"
)

// ParseDepSeverity normalizes a user-facing threshold string (case- and
// whitespace-insensitive) into a DepSeverity. Unrecognized input is kept
// (lowercased) so callers can distinguish it via IsKnown.
//
// "medium" is accepted as a spelling of "moderate" because it is what
// trivy itself calls that band, and the sibling
// NormalizeOpengrepFailSeverity already accepts several spellings per
// band for the same reason. Anything still unrecognised stays as written
// so the caller can name it in the refusal.
func ParseDepSeverity(raw string) DepSeverity {
	normalised := DepSeverity(strings.ToLower(strings.TrimSpace(raw)))
	// OpengrepSeverityMedium is the same word; reusing it keeps one
	// spelling of "medium" in the package rather than a third literal.
	if normalised == DepSeverity(OpengrepSeverityMedium) {
		return DepSeverityModerate
	}

	return normalised
}

// TrivyFilter returns the cumulative Trivy --severity filter string for
// this threshold. Lower thresholds widen the filter to include higher
// classes.
//
// An unknown severity still falls back to "CRITICAL", but callers must
// not rely on that: the fallback is the NARROWEST filter, so reaching it
// silently stops HIGH and MEDIUM findings from being reported at all.
// Callers reject unknown input via IsKnown before they get here.
func (s DepSeverity) TrivyFilter() string {
	switch s {
	case DepSeverityCritical:
		return "CRITICAL" //nolint:goconst // generic severity literal; extracting would not aid readability.
	case DepSeverityHigh:
		return "CRITICAL,HIGH"
	case DepSeverityModerate:
		return "CRITICAL,HIGH,MEDIUM"
	case DepSeverityLow:
		return "CRITICAL,HIGH,MEDIUM,LOW"
	default:
		return "CRITICAL"
	}
}

// IsKnown reports whether s is one of the accepted threshold values.
func (s DepSeverity) IsKnown() bool {
	switch s {
	case DepSeverityCritical, DepSeverityHigh, DepSeverityModerate, DepSeverityLow:
		return true
	default:
		return false
	}
}

// ExtractTrivyVulnIDs returns the unique sorted list of
// VulnerabilityID values inside a Trivy JSON report. Sort order is
// lexicographic, matching `sort -u`.
//
// The bash uses jq when available and a grep/cut fallback otherwise.
// The Go version always returns the same set; we walk the decoded
// JSON rather than substring matching.
func ExtractTrivyVulnIDs(body []byte) ([]string, error) {
	if len(body) == 0 {
		return nil, nil
	}

	doc, err := ParseTrivyReport(body)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}

	for _, r := range doc.Results {
		for _, v := range r.Vulnerabilities {
			if v.VulnerabilityID == "" {
				continue
			}

			seen[v.VulnerabilityID] = struct{}{}
		}
	}

	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}

	slices.Sort(out)

	return out, nil
}

// VulnFinding is one vulnerability in one package of one scanned target
// (a lockfile, a go.mod, a jar). A dependency diff compares findings, not
// bare vulnerability IDs: a pull request that brings a CVE into another
// package or another lockfile has added exposure even when the base branch
// already carries that CVE somewhere else. The installed version is left
// out, so upgrading a package to a release still affected by the same CVE
// is not reported as new.
type VulnFinding struct {
	Target  string
	Package string
	ID      string
}

// ExtractTrivyFindings returns the unique findings in a Trivy JSON report,
// sorted by target, package and ID. Entries without a vulnerability ID are
// skipped, as ExtractTrivyVulnIDs skips them.
func ExtractTrivyFindings(body []byte) ([]VulnFinding, error) {
	if len(body) == 0 {
		return nil, nil
	}

	doc, err := ParseTrivyReport(body)
	if err != nil {
		return nil, err
	}

	var out []VulnFinding

	for _, result := range doc.Results {
		for _, vuln := range result.Vulnerabilities {
			if vuln.VulnerabilityID == "" {
				continue
			}

			out = append(out, VulnFinding{Target: result.Target, Package: vuln.PkgName, ID: vuln.VulnerabilityID})
		}
	}

	slices.SortFunc(out, compareFindings)

	return slices.Compact(out), nil
}

func compareFindings(a, b VulnFinding) int {
	return cmp.Or(cmp.Compare(a.Target, b.Target), cmp.Compare(a.Package, b.Package), cmp.Compare(a.ID, b.ID))
}

// DiffNewFindings returns the findings in head that base does not have, in
// head's order. The result never aliases head.
func DiffNewFindings(base, head []VulnFinding) []VulnFinding {
	out := make([]VulnFinding, 0)

	for _, finding := range head {
		if !slices.Contains(base, finding) {
			out = append(out, finding)
		}
	}

	return out
}

// VulnRow is one row of the "New Vulnerabilities" table — the subset
// of fields the bash extracts via jq.
type VulnRow struct {
	ID        string
	Severity  string
	Package   string
	Installed string
	Fixed     string
}

// FilterVulnRows returns a table row for every vulnerability in body that
// matches one of keep. A package installed at two versions in one target
// yields a row for each.
func FilterVulnRows(body []byte, keep []VulnFinding) ([]VulnRow, error) {
	if len(keep) == 0 || len(body) == 0 {
		return nil, nil
	}

	doc, err := ParseTrivyReport(body)
	if err != nil {
		return nil, err
	}

	var out []VulnRow

	for _, result := range doc.Results {
		for _, vuln := range result.Vulnerabilities {
			if !slices.Contains(keep, VulnFinding{Target: result.Target, Package: vuln.PkgName, ID: vuln.VulnerabilityID}) {
				continue
			}

			fixed := vuln.FixedVersion
			if fixed == "" {
				fixed = "—"
			}

			out = append(out, VulnRow{
				ID:        vuln.VulnerabilityID,
				Severity:  vuln.Severity,
				Package:   vuln.PkgName,
				Installed: vuln.InstalledVersion,
				Fixed:     fixed,
			})
		}
	}

	return out, nil
}

// ScanDepsSummaryInput drives RenderScanDepsSummary.
type ScanDepsSummaryInput struct {
	Mode           string // "diff" | "full" | "full (worktree fallback)" | …
	FailOnSeverity string
	SeverityFilter string
	NewCount       int
	NewRows        []VulnRow // empty when row details unavailable
}

// RenderScanDepsSummary returns the markdown summary block written by.
func RenderScanDepsSummary(in ScanDepsSummaryInput) string {
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "## Dependency Vulnerability Scan\n\n")
	_, _ = fmt.Fprintf(&b, "| Setting | Value |\n")
	_, _ = fmt.Fprintf(&b, "|---------|-------|\n")
	_, _ = fmt.Fprintf(&b, "| Scanner | Trivy |\n")
	_, _ = fmt.Fprintf(&b, "| Mode | %s |\n", in.Mode)
	_, _ = fmt.Fprintf(&b, "| Fail threshold | %s |\n", in.FailOnSeverity)
	_, _ = fmt.Fprintf(&b, "| Severity filter | %s |\n", in.SeverityFilter)
	_, _ = fmt.Fprintf(&b, "| New vulnerabilities | **%d** |\n\n", in.NewCount)

	if in.NewCount > 0 {
		_, _ = fmt.Fprintf(&b, "### New Vulnerabilities\n\n")
		_, _ = fmt.Fprintf(&b, "| ID | Severity | Package | Installed | Fixed |\n")
		_, _ = fmt.Fprintf(&b, "|----|----------|---------|-----------|-------|\n")

		if len(in.NewRows) > 0 {
			for _, r := range in.NewRows {
				_, _ = fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
					r.ID, r.Severity, r.Package, r.Installed, r.Fixed)
			}
		} else {
			// Fallback row format the bash uses when the report JSON is
			// unavailable for detail enrichment.
			_, _ = fmt.Fprintf(&b, "| (no details available) | — | — | — | — |\n")
		}

		_, _ = fmt.Fprintf(&b, "\n")
	} else if in.NewCount == 0 {
		_, _ = fmt.Fprintf(&b, "> No new vulnerabilities found.\n\n")
	}

	return b.String()
}
