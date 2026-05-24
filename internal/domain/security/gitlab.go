// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security

// GitLabSchemaVersion is the version both transforms emit. The
// security-report-schemas registry identifies dependency_scanning and
// container_scanning by `scan.type`; the version field is the same
// across them.
const GitLabSchemaVersion = "15.2.1"

// GitLabReport is the top-level schema-v15 object GitLab CI ingests via
// `artifacts:reports:dependency_scanning` or `artifacts:reports:container_scanning`.
type GitLabReport struct {
	Version         string                `json:"version"`
	Scan            GitLabScan            `json:"scan"`
	Vulnerabilities []GitLabVulnerability `json:"vulnerabilities"`
}

// GitLabScan describes the scanner that produced the report.
type GitLabScan struct {
	Scanner   GitLabScanner `json:"scanner"`
	Analyzer  GitLabScanner `json:"analyzer"`
	Type      string        `json:"type"`       // "dependency_scanning" | "container_scanning"
	StartTime string        `json:"start_time"` // ISO 8601 without zone, per schema
	EndTime   string        `json:"end_time"`
	Status    string        `json:"status"` // always "success" — non-success is a CI-level concern
}

// GitLabScanner identifies the tool. Trivy is both scanner and analyzer
// for our flow.
type GitLabScanner struct {
	ID      string              `json:"id"`
	Name    string              `json:"name"`
	Version string              `json:"version"`
	Vendor  GitLabScannerVendor `json:"vendor"`
}

// GitLabScannerVendor is the vendor block of a GitLabScanner entry.
type GitLabScannerVendor struct {
	Name string `json:"name"`
}

// GitLabVulnerability is one finding in the scan.
type GitLabVulnerability struct {
	ID          string             `json:"id"` // deterministic UUID per (vuln,package,version,target)
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Severity    string             `json:"severity"` // "Critical" | "High" | "Medium" | "Low" | "Info" | "Unknown"
	Solution    string             `json:"solution"`
	Identifiers []GitLabIdentifier `json:"identifiers"`
	Links       []GitLabLink       `json:"links"`
	Location    GitLabLocation     `json:"location"`
}

// GitLabIdentifier is one (type, value) pair in a vulnerability's identifiers list.
type GitLabIdentifier struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
	URL   string `json:"url"`
}

// GitLabLink is one URL reference attached to a vulnerability.
type GitLabLink struct {
	URL string `json:"url"`
}

// GitLabLocation differs between dep and container reports — the dep
// schema uses `file`, the container schema uses `image` +
// `operating_system`. Both share the dependency.{package,version} sub-shape.
type GitLabLocation struct {
	File            string           `json:"file,omitempty"`
	Image           string           `json:"image,omitempty"`
	OperatingSystem string           `json:"operating_system,omitempty"`
	Dependency      GitLabDependency `json:"dependency"`
}

// GitLabDependency describes the vulnerable dependency at a location.
type GitLabDependency struct {
	Package GitLabPackage `json:"package"`
	Version string        `json:"version"`
}

// GitLabPackage is the package portion of a GitLabDependency.
type GitLabPackage struct {
	Name string `json:"name"`
}
