// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security_test

import (
	"bytes"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/security"
)

func sample() *security.TrivyReport {
	return &security.TrivyReport{
		ArtifactName: "ghcr.io/example/img",
		Results: []security.TrivyResult{{
			Target: "alpine",
			Vulnerabilities: []security.TrivyVulnerability{
				{
					VulnerabilityID:  "CVE-2025-0002",
					PkgName:          "libcurl",
					InstalledVersion: "8.0.1",
					FixedVersion:     "8.0.2",
					Severity:         "MEDIUM",
					Title:            "curl heap overflow",
					Description:      "Heap overflow in curl",
					PrimaryURL:       "https://nvd.nist.gov/vuln/detail/CVE-2025-0002",
				},
				{
					VulnerabilityID:  "CVE-2025-0001",
					PkgName:          "openssl",
					InstalledVersion: "3.0.0",
					FixedVersion:     "3.0.1",
					Severity:         "CRITICAL", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
					Title:            "OpenSSL RCE",
				},
				{
					VulnerabilityID:  "CVE-2025-0003",
					PkgName:          "zlib",
					InstalledVersion: "1.2.13",
					Severity:         "LOW", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				},
			},
		}},
	}
}

func writeAndAssert(t *testing.T, report *security.TrivyReport, opts security.Options, contains []string) {
	t.Helper()

	doc := security.TrivyToSARIF(report, opts)

	var buf bytes.Buffer
	if err := doc.PrettyWrite(&buf); err != nil {
		t.Fatalf("PrettyWrite: %v", err)
	}

	body := buf.String()
	for _, want := range contains {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Errorf("SARIF missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestTrivyToSARIF_MapsSeveritiesToLevels(t *testing.T) {
	t.Parallel()
	writeAndAssert(t, sample(), security.Options{
		ImageRef:     "ghcr.io/example/img@sha256:deadbeef",
		TrivyVersion: "0.69.3",
	}, []string{
		`"$schema"`,
		`"version": "2.1.0"`,
		`"name": "Trivy"`,
		`"version": "0.69.3"`,
		`"ruleId": "CVE-2025-0001"`, // CRITICAL → error
		`"level": "error"`,
		`"ruleId": "CVE-2025-0002"`, // MEDIUM → warning
		`"level": "warning"`,
		`"ruleId": "CVE-2025-0003"`, // LOW → note
		`"level": "note"`,
		`"ghcr.io/example/img@sha256:deadbeef"`,
	})
}

func TestTrivyToSARIF_RuleSetIsDeterministicallyOrdered(t *testing.T) {
	t.Parallel()

	doc := security.TrivyToSARIF(sample(), security.Options{})
	if len(doc.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(doc.Runs))
	}

	rules := doc.Runs[0].Tool.Driver.Rules
	if len(rules) != 3 {
		t.Fatalf("rule count = %d, want 3 (one per unique CVE)", len(rules))
	}
	// CVE ids sort lexicographically.
	want := []string{"CVE-2025-0001", "CVE-2025-0002", "CVE-2025-0003"}

	for i, r := range rules { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		got := ""
		if r.ID != nil {
			got = *r.ID
		}

		if got != want[i] {
			t.Errorf("rules[%d].ID = %q, want %q", i, got, want[i])
		}
	}
}

func TestTrivyToSARIF_ResultsAreDeterministicallyOrdered(t *testing.T) {
	t.Parallel()

	first := security.TrivyToSARIF(sample(), security.Options{})
	second := security.TrivyToSARIF(sample(), security.Options{})

	var b1, b2 bytes.Buffer
	if err := first.PrettyWrite(&b1); err != nil {
		t.Fatal(err)
	}

	if err := second.PrettyWrite(&b2); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(b1.Bytes(), b2.Bytes()) {
		t.Errorf("identical input produced non-identical SARIF — sort is non-deterministic")
	}
}

func TestTrivyToSARIF_MessageIncludesFixWhenAvailable(t *testing.T) {
	t.Parallel()
	writeAndAssert(t, sample(), security.Options{}, []string{
		"openssl@3.0.0",
		"(fix: 3.0.1)",
		"OpenSSL RCE",
	})
}

func TestTrivyToSARIF_FallsBackToVulnIDWhenTitleMissing(t *testing.T) {
	t.Parallel()

	report := &security.TrivyReport{
		Results: []security.TrivyResult{{
			Vulnerabilities: []security.TrivyVulnerability{
				{VulnerabilityID: "CVE-X-1", PkgName: "p", Severity: "HIGH"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			},
		}},
	}
	writeAndAssert(t, report, security.Options{}, []string{`"CVE-X-1"`})
}

func TestTrivyToSARIF_EmptyReportProducesValidEmptySARIF(t *testing.T) {
	t.Parallel()

	doc := security.TrivyToSARIF(nil, security.Options{})
	if len(doc.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(doc.Runs))
	}

	if len(doc.Runs[0].Results) != 0 {
		t.Errorf("expected zero results in empty SARIF, got %d", len(doc.Runs[0].Results))
	}
}

func TestTrivyToSARIF_PhysicalLocationFallsBackToPkgNameWithoutImageRef(t *testing.T) {
	t.Parallel()

	report := &security.TrivyReport{
		Results: []security.TrivyResult{{
			Vulnerabilities: []security.TrivyVulnerability{
				{VulnerabilityID: "CVE-X-2", PkgName: "lib-foo", Severity: "LOW"},
			},
		}},
	}
	writeAndAssert(t, report, security.Options{}, []string{`"uri": "lib-foo"`})
}
