// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// ScanMode is the dependency-scan comparison mode requested by the
// workflow. "diff" compares HEAD vulnerabilities against the PR base
// ref; "full" reports every HEAD finding.
type ScanMode string

const (
	ScanModeDiff ScanMode = "diff"
	ScanModeFull ScanMode = "full"
)

// Default file paths for the Trivy dependency-scan outputs. Single
// source of truth: both the CLI flag's --sarif-file / --gitlab-dep-file
// defaults and the use case's empty-fallback reference these constants.
const (
	DefaultTrivySARIFFile     = "trivy-dependency-results.sarif"
	DefaultTrivyGitLabDepFile = "gl-dependency-scanning-report.json"
)

// MapTrivyFailSeverity returns the cumulative Trivy --severity filter
// string for a user-facing severity threshold. Lower thresholds widen
// the filter to include higher-severity classes.
//
// Unknown inputs map to "CRITICAL" (the bash default) — the caller is
// expected to also log a warning in that path.
func MapTrivyFailSeverity(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "critical":
		return "CRITICAL"
	case "high":
		return "CRITICAL,HIGH"
	case "moderate":
		return "CRITICAL,HIGH,MEDIUM"
	case "low":
		return "CRITICAL,HIGH,MEDIUM,LOW"
	default:
		return "CRITICAL"
	}
}

// IsKnownTrivyFailSeverity reports whether level is one of the accepted
// user-facing threshold values.
func IsKnownTrivyFailSeverity(level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "critical", "high", "moderate", "low":
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
	type trivyResult struct {
		Vulnerabilities []struct {
			VulnerabilityID string `json:"VulnerabilityID"`
		} `json:"Vulnerabilities"`
	}
	type trivyReport struct {
		Results []trivyResult `json:"Results"`
	}
	var doc trivyReport
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse trivy JSON: %w", err)
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

// DiffNewIDs returns the IDs present in head but absent from base.
// Both inputs must already be sorted. Mirrors `comm -13 base head`.
//
// Returns a freshly-allocated slice (never aliases head).
func DiffNewIDs(base, head []string) []string {
	baseSet := make(map[string]struct{}, len(base))
	for _, id := range base {
		baseSet[id] = struct{}{}
	}
	out := make([]string, 0)
	for _, id := range head {
		if _, ok := baseSet[id]; ok {
			continue
		}
		out = append(out, id)
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

// FilterVulnRowsByID returns the VulnRow set in body whose
// VulnerabilityID is in keep. The bash uses `jq --slurpfile ids ... |
// index($id)` for the same purpose.
func FilterVulnRowsByID(body []byte, keep []string) ([]VulnRow, error) {
	if len(keep) == 0 || len(body) == 0 {
		return nil, nil
	}
	type trivyVuln struct {
		VulnerabilityID  string `json:"VulnerabilityID"`
		Severity         string `json:"Severity"`
		PkgName          string `json:"PkgName"`
		InstalledVersion string `json:"InstalledVersion"`
		FixedVersion     string `json:"FixedVersion"`
	}
	type trivyResult struct {
		Vulnerabilities []trivyVuln `json:"Vulnerabilities"`
	}
	type trivyReport struct {
		Results []trivyResult `json:"Results"`
	}
	var doc trivyReport
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse trivy JSON: %w", err)
	}
	keepSet := make(map[string]struct{}, len(keep))
	for _, id := range keep {
		keepSet[id] = struct{}{}
	}
	var out []VulnRow
	for _, r := range doc.Results {
		for _, v := range r.Vulnerabilities {
			if _, ok := keepSet[v.VulnerabilityID]; !ok {
				continue
			}
			fixed := v.FixedVersion
			if fixed == "" {
				fixed = "—"
			}
			out = append(out, VulnRow{
				ID:        v.VulnerabilityID,
				Severity:  v.Severity,
				Package:   v.PkgName,
				Installed: v.InstalledVersion,
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

// RenderScanDepsSummary returns the markdown summary block written by
// scripts/security/scan-dependencies.sh, byte-for-byte.
func RenderScanDepsSummary(in ScanDepsSummaryInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Dependency Vulnerability Scan\n\n")
	fmt.Fprintf(&b, "| Setting | Value |\n")
	fmt.Fprintf(&b, "|---------|-------|\n")
	fmt.Fprintf(&b, "| Scanner | Trivy |\n")
	fmt.Fprintf(&b, "| Mode | %s |\n", in.Mode)
	fmt.Fprintf(&b, "| Fail threshold | %s |\n", in.FailOnSeverity)
	fmt.Fprintf(&b, "| Severity filter | %s |\n", in.SeverityFilter)
	fmt.Fprintf(&b, "| New vulnerabilities | **%d** |\n\n", in.NewCount)

	if in.NewCount > 0 {
		fmt.Fprintf(&b, "### New Vulnerabilities\n\n")
		fmt.Fprintf(&b, "| ID | Severity | Package | Installed | Fixed |\n")
		fmt.Fprintf(&b, "|----|----------|---------|-----------|-------|\n")
		if len(in.NewRows) > 0 {
			for _, r := range in.NewRows {
				fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
					r.ID, r.Severity, r.Package, r.Installed, r.Fixed)
			}
		} else {
			// Fallback row format the bash uses when the report JSON is
			// unavailable for detail enrichment.
			fmt.Fprintf(&b, "| (no details available) | — | — | — | — |\n")
		}
		fmt.Fprintf(&b, "\n")
	} else if in.NewCount == 0 {
		fmt.Fprintf(&b, "> No new vulnerabilities found.\n\n")
	}
	return b.String()
}
