// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
)

const sampleTrivy = `{
  "Results": [
    {
      "Target": "package-lock.json",
      "Class": "lang-pkgs",
      "Vulnerabilities": [
        {
          "VulnerabilityID": "CVE-2024-0001",
          "PkgName": "lodash",
          "InstalledVersion": "4.17.20",
          "FixedVersion": "4.17.21",
          "Title": "Prototype pollution in lodash",
          "Description": "lodash mishandles property merging.",
          "Severity": "HIGH",
          "PrimaryURL": "https://example.com/CVE-2024-0001",
          "References": ["https://example.com/ref1", "https://example.com/ref2"]
        },
        {
          "VulnerabilityID": "CVE-2024-0002",
          "PkgName": "axios",
          "InstalledVersion": "0.21.0",
          "Title": "ReDoS in axios",
          "Severity": "MEDIUM",
          "PrimaryURL": "https://example.com/CVE-2024-0002"
        }
      ]
    }
  ]
}`

func parseTrivy(t *testing.T, in string) *security.TrivyReport {
	t.Helper()

	var r security.TrivyReport
	if err := json.Unmarshal([]byte(in), &r); err != nil {
		t.Fatalf("parse trivy: %v", err)
	}

	return &r
}

func TestTrivyToGitLabDep_BasicShape(t *testing.T) {
	t.Parallel()
	report := parseTrivy(t, sampleTrivy)
	gl := security.TrivyToGitLabDep(report, security.Options{
		TrivyVersion: "0.50.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Now:          time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	})

	if gl.Version != security.GitLabSchemaVersion {
		t.Errorf("version = %q, want %q", gl.Version, security.GitLabSchemaVersion)
	}

	if gl.Scan.Type != "dependency_scanning" {
		t.Errorf("scan.type = %q, want %q", gl.Scan.Type, "dependency_scanning")
	}

	if gl.Scan.Scanner.ID != "trivy" {
		t.Errorf("scanner.id = %q, want %q", gl.Scan.Scanner.ID, "trivy")
	}

	if gl.Scan.Scanner.Version != "0.50.0" {
		t.Errorf("scanner.version = %q, want %q", gl.Scan.Scanner.Version, "0.50.0")
	}

	if got := len(gl.Vulnerabilities); got != 2 {
		t.Errorf("vulnerabilities count = %d, want 2", got)
	}
}

func TestTrivyToGitLabDep_SeverityMapping(t *testing.T) {
	t.Parallel()
	report := parseTrivy(t, sampleTrivy)
	gl := security.TrivyToGitLabDep(report, security.Options{})

	severities := make([]string, 0, len(gl.Vulnerabilities))
	for _, v := range gl.Vulnerabilities {
		severities = append(severities, v.Severity)
	}

	want := []string{"High", "Medium"} //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	for i, s := range severities {
		if s != want[i] {
			t.Errorf("severities[%d] = %q, want %q", i, s, want[i])
		}
	}
}

func TestTrivyToGitLabDep_SolutionFromFixedVersion(t *testing.T) {
	t.Parallel()
	report := parseTrivy(t, sampleTrivy)
	gl := security.TrivyToGitLabDep(report, security.Options{})

	// First finding has FixedVersion=4.17.21
	if got := gl.Vulnerabilities[0].Solution; got != "Upgrade to 4.17.21" {
		t.Errorf("solution = %q, want %q", got, "Upgrade to 4.17.21")
	}
	// Second finding has no FixedVersion
	if got := gl.Vulnerabilities[1].Solution; got != "" {
		t.Errorf("solution = %q, want empty", got)
	}
}

func TestTrivyToGitLabDep_Location(t *testing.T) {
	t.Parallel()
	report := parseTrivy(t, sampleTrivy)
	gl := security.TrivyToGitLabDep(report, security.Options{})

	loc := gl.Vulnerabilities[0].Location
	if loc.File != "package-lock.json" {
		t.Errorf("location.file = %q, want %q", loc.File, "package-lock.json")
	}

	if loc.Image != "" || loc.OperatingSystem != "" {
		t.Errorf("dep location should not have image/os: %+v", loc)
	}

	if loc.Dependency.Package.Name != "lodash" {
		t.Errorf("package.name = %q, want %q", loc.Dependency.Package.Name, "lodash")
	}
}

func TestTrivyToGitLabDep_LinksDeduplicated(t *testing.T) {
	t.Parallel()
	report := parseTrivy(t, `{
		"Results": [{"Target": "x", "Vulnerabilities": [{
			"VulnerabilityID": "CVE-1",
			"PrimaryURL": "https://a/",
			"References": ["https://a/", "https://b/", "https://b/", "https://c/"]
		}]}]
	}`)
	gl := security.TrivyToGitLabDep(report, security.Options{})

	urls := make([]string, 0, len(gl.Vulnerabilities[0].Links))
	for _, l := range gl.Vulnerabilities[0].Links {
		urls = append(urls, l.URL)
	}

	if got := strings.Join(urls, ","); got != "https://a/,https://b/,https://c/" {
		t.Errorf("links = %q, want sorted+deduped", got)
	}
}

func TestTrivyToGitLabDep_DeterministicUUIDs(t *testing.T) {
	t.Parallel()
	report := parseTrivy(t, sampleTrivy)
	a := security.TrivyToGitLabDep(report, security.Options{}) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	b := security.TrivyToGitLabDep(report, security.Options{})
	for i := range a.Vulnerabilities {
		if a.Vulnerabilities[i].ID != b.Vulnerabilities[i].ID {
			t.Errorf("UUID for vuln %d not deterministic: %q vs %q",
				i, a.Vulnerabilities[i].ID, b.Vulnerabilities[i].ID)
		}
	}
}

func TestTrivyToGitLabContainer_LocationHasImageAndOS(t *testing.T) {
	t.Parallel()
	report := parseTrivy(t, `{
		"ArtifactName": "ghcr.io/owner/img",
		"Metadata": {"OS": {"Family": "alpine", "Name": "3.19"}},
		"Results": [{"Target": "alpine 3.19 (alpine 3.19)", "Vulnerabilities": [{
			"VulnerabilityID": "CVE-2024-0003",
			"PkgName": "musl",
			"InstalledVersion": "1.2.4",
			"FixedVersion": "1.2.5",
			"Severity": "CRITICAL"
		}]}]
	}`)
	gl := security.TrivyToGitLabContainer(report, security.Options{
		ImageRef:     "ghcr.io/owner/img@sha256:abc",
		TrivyVersion: "0.50.0",
	})

	if gl.Scan.Type != "container_scanning" {
		t.Errorf("scan.type = %q", gl.Scan.Type)
	}

	loc := gl.Vulnerabilities[0].Location
	if loc.Image != "ghcr.io/owner/img@sha256:abc" {
		t.Errorf("location.image = %q", loc.Image)
	}

	if loc.OperatingSystem != "alpine 3.19" {
		t.Errorf("operating_system = %q", loc.OperatingSystem)
	}

	if got := gl.Vulnerabilities[0].Solution; got != "Upgrade musl to 1.2.5" {
		t.Errorf("solution = %q", got)
	}
}

func TestTrivyToGitLabContainer_FallsBackToArtifactName(t *testing.T) {
	t.Parallel()
	report := parseTrivy(t, `{
		"ArtifactName": "fallback-image",
		"Results": [{"Target": "x", "Vulnerabilities": [{
			"VulnerabilityID": "CVE-X", "PkgName": "p", "InstalledVersion": "1", "Severity": "low"
		}]}]
	}`)
	// No ImageRef in opts → falls back to ArtifactName.
	gl := security.TrivyToGitLabContainer(report, security.Options{})
	if got := gl.Vulnerabilities[0].Location.Image; got != "fallback-image" {
		t.Errorf("location.image = %q, want %q", got, "fallback-image")
	}
}

func TestTrivyToGitLabContainer_EmptyResults(t *testing.T) {
	t.Parallel()
	report := parseTrivy(t, `{"Results": []}`)

	gl := security.TrivyToGitLabContainer(report, security.Options{ImageRef: "img"})
	if gl.Vulnerabilities == nil {
		t.Errorf("vulnerabilities should be empty array, not nil (JSON shape)")
	}

	if len(gl.Vulnerabilities) != 0 {
		t.Errorf("got %d vulns, want 0", len(gl.Vulnerabilities))
	}
}

func TestNormalizeSeverity(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"CRITICAL":      "Critical", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"critical":      "Critical", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"High":          "High",
		"medium":        "Medium", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"LOW":           "Low",    //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"INFO":          "Info",
		"INFORMATIONAL": "Info",
		"unknown":       "Unknown", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"":              "Unknown",
		"weird-thing":   "Unknown",
	}
	for in, want := range tests {
		t.Run(in+"->"+want, func(t *testing.T) {
			t.Parallel()

			if got := security.NormalizeSeverity(in); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestDeterministicUUID_Format(t *testing.T) {
	t.Parallel()

	id := security.DeterministicUUID("CVE-2024-0001|lodash|4.17.20|package-lock.json")

	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Fatalf("UUID has %d parts, want 5: %q", len(parts), id)
	}

	wantLens := []int{8, 4, 4, 4, 12}
	for i, p := range parts {
		if len(p) != wantLens[i] {
			t.Errorf("part %d len = %d, want %d (%q)", i, len(p), wantLens[i], p)
		}
	}
	// Determinism
	id2 := security.DeterministicUUID("CVE-2024-0001|lodash|4.17.20|package-lock.json")
	if id != id2 {
		t.Errorf("UUID not deterministic: %q vs %q", id, id2)
	}
	// Different seed → different UUID
	id3 := security.DeterministicUUID("CVE-2024-0001|lodash|4.17.20|other-target")
	if id == id3 {
		t.Errorf("different seeds produced the same UUID: %q", id)
	}
}

func TestDefaultsApplyWhenOptionsZero(t *testing.T) {
	t.Parallel()
	report := parseTrivy(t, sampleTrivy)
	gl := security.TrivyToGitLabDep(report, security.Options{})

	if gl.Scan.Scanner.Version != "unknown" {
		t.Errorf("default version = %q, want %q", gl.Scan.Scanner.Version, "unknown")
	}

	if gl.Scan.StartTime == "" || gl.Scan.EndTime == "" {
		t.Errorf("start/end time empty when Options.Now is zero (should default to now)")
	}
}
