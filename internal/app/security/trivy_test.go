// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	appsecurity "github.com/diggsweden/reusable-ci/internal/app/security"
	domainsecurity "github.com/diggsweden/reusable-ci/internal/domain/security"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

const sampleTrivyJSON = `{
  "Results": [{"Target": "package-lock.json", "Class": "lang-pkgs", "Vulnerabilities": [
    {"VulnerabilityID": "CVE-X", "PkgName": "p", "InstalledVersion": "1", "FixedVersion": "2", "Severity": "high"}
  ]}]
}`

func TestTrivyToGitLabDep_RoundTrip(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	in := fsys.WriteFile("trivy.json", []byte(sampleTrivyJSON))
	out := fsys.Path("gl.json")

	count, err := appsecurity.TrivyToGitLabDep(appsecurity.TransformInput{
		InputPath: in, OutputPath: out, TrivyVersion: "0.50.0",
	})
	if err != nil {
		t.Fatalf("TrivyToGitLabDep: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}

	var report domainsecurity.GitLabReport
	data := fsys.ReadFile("gl.json")
	require.NoError(t, json.Unmarshal(data, &report))

	if report.Scan.Type != "dependency_scanning" {
		t.Errorf("scan.type = %q, want dependency_scanning", report.Scan.Type)
	}
	vuln := report.Vulnerabilities[0]
	if vuln.Name != "CVE-X" || vuln.Severity != "High" || vuln.Location.File != "package-lock.json" {
		t.Errorf("vulnerability mapping = %+v", vuln)
	}
	if vuln.Location.Dependency.Package.Name != "p" || vuln.Location.Dependency.Version != "1" {
		t.Errorf("dependency mapping = %+v", vuln.Location.Dependency)
	}
}

func TestTrivyToGitLabContainer_RoundTrip(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)
	in := fsys.WriteFile("trivy.json", []byte(`{
		"ArtifactName": "img",
		"Metadata": {"OS": {"Family": "alpine", "Name": "3.19"}},
		"Results": [{"Target": "x", "Vulnerabilities": [{
			"VulnerabilityID": "CVE-Y", "PkgName": "musl", "InstalledVersion": "1.2.4",
			"FixedVersion": "1.2.5", "Severity": "CRITICAL"
		}]}]
	}`))
	out := fsys.Path("gl.json")

	count, err := appsecurity.TrivyToGitLabContainer(appsecurity.TransformInput{
		InputPath: in, OutputPath: out, ImageRef: "ghcr.io/owner/img@sha256:abc",
	})
	if err != nil {
		t.Fatalf("TrivyToGitLabContainer: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}

	var report domainsecurity.GitLabReport
	data := fsys.ReadFile("gl.json")
	require.NoError(t, json.Unmarshal(data, &report))
	if report.Scan.Type != "container_scanning" {
		t.Errorf("scan.type = %q, want container_scanning", report.Scan.Type)
	}
	vuln := report.Vulnerabilities[0]
	if vuln.Name != "CVE-Y" || vuln.Severity != "Critical" {
		t.Errorf("vulnerability mapping = %+v", vuln)
	}
	if vuln.Location.Image != "ghcr.io/owner/img@sha256:abc" || vuln.Location.OperatingSystem != "alpine 3.19" {
		t.Errorf("container location = %+v", vuln.Location)
	}
	if vuln.Location.Dependency.Package.Name != "musl" || vuln.Location.Dependency.Version != "1.2.4" {
		t.Errorf("dependency mapping = %+v", vuln.Location.Dependency)
	}
}

func TestTrivyToGitLab_BadInputErrors(t *testing.T) {
	t.Parallel()
	_, err := appsecurity.TrivyToGitLabDep(appsecurity.TransformInput{
		InputPath: "/does/not/exist", OutputPath: testfs.NewReal(t).Path("out.json"),
	})
	if err == nil {
		t.Errorf("expected error on missing input")
	}
}
