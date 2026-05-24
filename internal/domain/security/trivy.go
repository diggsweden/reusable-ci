// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package security holds pure security-report transforms.
//
// Trivy emits its native JSON. GitLab CI ingests dependency- and
// container-scanning reports in a different schema (security-report-schemas
// v15.x). The transforms in this package are JSON-in / JSON-out, no I/O,
// no subprocess.
package security

// TrivyReport is the subset of Trivy's JSON output we care about.
// Fields not used by the transforms are omitted; encoding/json ignores
// extra keys silently.
type TrivyReport struct {
	ArtifactName string         `json:"ArtifactName,omitempty"`
	Metadata     *TrivyMetadata `json:"Metadata,omitempty"`
	Results      []TrivyResult  `json:"Results,omitempty"`
}

// TrivyMetadata carries OS / image fingerprint context for container scans.
type TrivyMetadata struct {
	OS *TrivyOS `json:"OS,omitempty"`
}

// TrivyOS is the OS family/name pair we surface as operating_system.
type TrivyOS struct {
	Family string `json:"Family,omitempty"`
	Name   string `json:"Name,omitempty"`
}

// TrivyResult is one scan target (a package-lock.json, a layer, etc.).
type TrivyResult struct {
	Target          string               `json:"Target,omitempty"`
	Class           string               `json:"Class,omitempty"`
	Vulnerabilities []TrivyVulnerability `json:"Vulnerabilities,omitempty"`
}

// TrivyVulnerability is one finding.
type TrivyVulnerability struct {
	VulnerabilityID  string   `json:"VulnerabilityID,omitempty"`
	PkgName          string   `json:"PkgName,omitempty"`
	InstalledVersion string   `json:"InstalledVersion,omitempty"`
	FixedVersion     string   `json:"FixedVersion,omitempty"`
	Title            string   `json:"Title,omitempty"`
	Description      string   `json:"Description,omitempty"`
	Severity         string   `json:"Severity,omitempty"`
	PrimaryURL       string   `json:"PrimaryURL,omitempty"`
	References       []string `json:"References,omitempty"`
}

// OSDescription returns "<Family> <Name>" trimmed, or "" when no OS info.
func (r *TrivyReport) OSDescription() string {
	if r == nil || r.Metadata == nil || r.Metadata.OS == nil {
		return ""
	}

	os := r.Metadata.OS
	switch {
	case os.Family != "" && os.Name != "":
		return os.Family + " " + os.Name
	case os.Family != "":
		return os.Family
	default:
		return os.Name
	}
}
