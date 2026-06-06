// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Defaults / constants surfaced to callers that want to override them.
const (
	scanTypeDep       = "dependency_scanning"
	scanTypeContainer = "container_scanning"
	scannerID         = "trivy"
	scannerName       = "Trivy"
	scannerVendor     = "Aqua Security"
	defaultStatus     = "success"
)

// Options carries the inputs the transform can't infer from the Trivy JSON
// alone. All fields are optional — TrivyToGitLabDep / Container will fill
// sensible defaults when zero values are passed.
type Options struct {
	// TrivyVersion is what scanner.version / analyzer.version report.
	// Empty defaults to "unknown".
	TrivyVersion string

	// Now is the timestamp emitted as scan.start_time and scan.end_time.
	// Zero value defaults to time.Now().UTC(). Tests pass a fixed time
	// for deterministic golden output.
	Now time.Time

	// ImageRef is the fully qualified image reference (e.g.
	// ghcr.io/owner/img@sha256:…) used by the container transform's
	// location.image. Falls back to TrivyReport.ArtifactName when empty.
	// Ignored by the dep transform.
	ImageRef string
}

// TrivyToGitLabDep transforms Trivy JSON into the GitLab
// dependency-scanning schema. Pure: no I/O. Each finding gets a
// deterministic UUID derived from (VulnerabilityID|PkgName|InstalledVersion|Target).
func TrivyToGitLabDep(report *TrivyReport, opts Options) *GitLabReport {
	out := buildBase(scanTypeDep, opts)

	for _, r := range report.Results {
		for _, v := range r.Vulnerabilities {
			out.Vulnerabilities = append(out.Vulnerabilities, buildDepVuln(r, v))
		}
	}

	if out.Vulnerabilities == nil {
		out.Vulnerabilities = []GitLabVulnerability{}
	}

	return out
}

// TrivyToGitLabContainer transforms Trivy JSON into the GitLab
// container-scanning schema. Includes operating_system + image in
// location. Each finding gets a deterministic UUID derived from
// (VulnerabilityID|PkgName|InstalledVersion|ImageRef).
func TrivyToGitLabContainer(report *TrivyReport, opts Options) *GitLabReport {
	out := buildBase(scanTypeContainer, opts)

	imageRef := opts.ImageRef
	if imageRef == "" {
		imageRef = report.ArtifactName
	}

	osDesc := report.OSDescription()
	for _, r := range report.Results {
		for _, v := range r.Vulnerabilities {
			out.Vulnerabilities = append(out.Vulnerabilities, buildContainerVuln(v, imageRef, osDesc))
		}
	}

	if out.Vulnerabilities == nil {
		out.Vulnerabilities = []GitLabVulnerability{}
	}

	return out
}

func buildBase(scanType string, opts Options) *GitLabReport {
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	stamp := now.Format("2006-01-02T15:04:05")

	version := opts.TrivyVersion
	if version == "" {
		version = "unknown" //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}

	scanner := GitLabScanner{
		ID: scannerID, Name: scannerName, Version: version,
		Vendor: GitLabScannerVendor{Name: scannerVendor},
	}

	return &GitLabReport{
		Version: GitLabSchemaVersion,
		Scan: GitLabScan{
			Scanner: scanner, Analyzer: scanner,
			Type: scanType, StartTime: stamp, EndTime: stamp, Status: defaultStatus,
		},
	}
}

func buildDepVuln(result TrivyResult, v TrivyVulnerability) GitLabVulnerability { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	uuid := DeterministicUUID(fmt.Sprintf("%s|%s|%s|%s",
		v.VulnerabilityID, v.PkgName, v.InstalledVersion, result.Target))

	return GitLabVulnerability{
		ID:          uuid,
		Name:        nameOf(v),
		Description: v.Description,
		Severity:    NormalizeSeverity(v.Severity),
		Solution:    depSolution(v),
		Identifiers: identifiersFor(v),
		Links:       linksFor(v),
		Location: GitLabLocation{
			File: result.Target,
			Dependency: GitLabDependency{
				Package: GitLabPackage{Name: v.PkgName},
				Version: v.InstalledVersion,
			},
		},
	}
}

func buildContainerVuln(v TrivyVulnerability, image, osDesc string) GitLabVulnerability { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	uuid := DeterministicUUID(fmt.Sprintf("%s|%s|%s|%s",
		v.VulnerabilityID, v.PkgName, v.InstalledVersion, image))

	return GitLabVulnerability{
		ID:          uuid,
		Name:        nameOf(v),
		Description: v.Description,
		Severity:    NormalizeSeverity(v.Severity),
		Solution:    containerSolution(v),
		Identifiers: identifiersFor(v),
		Links:       linksFor(v),
		Location: GitLabLocation{
			Image:           image,
			OperatingSystem: osDesc,
			Dependency: GitLabDependency{
				Package: GitLabPackage{Name: v.PkgName},
				Version: v.InstalledVersion,
			},
		},
	}
}

func nameOf(v TrivyVulnerability) string {
	switch {
	case v.Title != "":
		return v.Title
	case v.VulnerabilityID != "":
		return v.VulnerabilityID
	default:
		return "Unknown" //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}
}

func depSolution(v TrivyVulnerability) string {
	if v.FixedVersion == "" {
		return ""
	}

	return "Upgrade to " + v.FixedVersion
}

func containerSolution(v TrivyVulnerability) string { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if v.FixedVersion == "" {
		return ""
	}

	pkg := v.PkgName
	if pkg == "" {
		pkg = "package"
	}

	return "Upgrade " + pkg + " to " + v.FixedVersion
}

func identifiersFor(v TrivyVulnerability) []GitLabIdentifier {
	return []GitLabIdentifier{{
		Type: "cve", Name: v.VulnerabilityID, Value: v.VulnerabilityID, URL: v.PrimaryURL,
	}}
}

func linksFor(v TrivyVulnerability) []GitLabLink { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	seen := make(map[string]struct{})

	var ordered []string

	add := func(s string) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if s == "" {
			return
		}

		if _, dup := seen[s]; dup {
			return
		}

		seen[s] = struct{}{}
		ordered = append(ordered, s)
	}
	add(v.PrimaryURL)

	for _, ref := range v.References {
		add(ref)
	}
	// Match the bash transform's `unique` semantics: deterministic order
	// for tests. The bash uses jq's `unique` which sorts.
	sort.Strings(ordered)

	out := make([]GitLabLink, len(ordered))
	for i, u := range ordered {
		out[i] = GitLabLink{URL: u}
	}

	return out
}

// NormalizeSeverity maps Trivy's varying-case severity to GitLab's
// Title-case enum. Unknown values fall through to "Unknown".
func NormalizeSeverity(in string) string {
	switch strings.ToLower(in) {
	case "critical":
		return "Critical"
	case "high":
		return "High"
	case "medium":
		return "Medium"
	case "low":
		return "Low"
	case "info", "informational":
		return "Info"
	case "":
		return "Unknown"
	default:
		// Includes "unknown" and any unexpected token.
		return "Unknown"
	}
}

// DeterministicUUID returns a UUID-shaped string derived from sha256(input).
// Stable across runs for the same seed; matches deterministic_uuid
// helper byte-for-byte.
func DeterministicUUID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	hex := hex.EncodeToString(sum[:])

	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex[0:8], hex[8:12], hex[12:16], hex[16:20], hex[20:32])
}
