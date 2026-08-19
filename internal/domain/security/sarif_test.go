// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/owenrumney/go-sarif/v3/pkg/report/v210/sarif"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
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

// TestTrivyToSARIF_SeverityLevelIsPerResult asserts which level each
// finding gets, rather than that the levels appear somewhere in the
// document. The substring checks above cannot see a swap -- mapping
// CRITICAL to note and LOW to error still leaves all three strings
// present -- and the level is what decides whether Code Scanning fails
// a branch protection check.
//
// It also covers the two severity classes no fixture reached: HIGH,
// which shares the error level with CRITICAL, and an unrecognised value,
// which must degrade to "none" rather than to a severity it did not earn.
func TestTrivyToSARIF_SeverityLevelIsPerResult(t *testing.T) {
	t.Parallel()

	report := &security.TrivyReport{
		ArtifactName: "ghcr.io/example/img",
		Results: []security.TrivyResult{{
			Target: "alpine",
			Vulnerabilities: []security.TrivyVulnerability{
				{VulnerabilityID: "CVE-CRIT", PkgName: "a", Severity: "CRITICAL"},
				{VulnerabilityID: "CVE-HIGH", PkgName: "b", Severity: "HIGH"},
				{VulnerabilityID: "CVE-MED", PkgName: "c", Severity: "MEDIUM"},
				{VulnerabilityID: "CVE-LOW", PkgName: "d", Severity: "LOW"},
				{VulnerabilityID: "CVE-UNKNOWN", PkgName: "e", Severity: "UNKNOWN"},
				{VulnerabilityID: "CVE-EMPTY", PkgName: "f", Severity: ""},
				// Trivy has emitted lowercase severities; the mapping
				// upper-cases before matching.
				{VulnerabilityID: "CVE-LOWERCASE", PkgName: "g", Severity: "critical"},
			},
		}},
	}

	doc := security.TrivyToSARIF(report, security.Options{})
	if len(doc.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(doc.Runs))
	}

	got := map[string]string{}

	for _, r := range doc.Runs[0].Results { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		id := ""
		if r.RuleID != nil {
			id = *r.RuleID
		}

		got[id] = r.Level
	}

	want := map[string]string{
		"CVE-CRIT":      "error",
		"CVE-HIGH":      "error",
		"CVE-MED":       "warning",
		"CVE-LOW":       "note",
		"CVE-UNKNOWN":   "none",
		"CVE-EMPTY":     "none",
		"CVE-LOWERCASE": "error",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("levels = %v, want %v", got, want)
	}
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

// resultByRuleID returns the message text and location URI of the result
// carrying ruleID, so tests can assert what a finding says rather than
// that a substring exists somewhere in the document.
func resultByRuleID(t *testing.T, doc *sarif.Report, ruleID string) (string, string) {
	t.Helper()

	for _, r := range doc.Runs[0].Results { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if r.RuleID == nil || *r.RuleID != ruleID {
			continue
		}

		msg := ""
		if r.Message.Text != nil {
			msg = *r.Message.Text
		}

		uri := ""
		if len(r.Locations) > 0 &&
			r.Locations[0].PhysicalLocation != nil &&
			r.Locations[0].PhysicalLocation.ArtifactLocation != nil &&
			r.Locations[0].PhysicalLocation.ArtifactLocation.URI != nil {
			uri = *r.Locations[0].PhysicalLocation.ArtifactLocation.URI
		}

		return msg, uri
	}

	t.Fatalf("no result for rule %q", ruleID)

	return "", ""
}

// TestTrivyToSARIF_MessageComposition pins the whole message for each
// shape it can take. The previous checks looked for "openssl@3.0.0" and
// "(fix: 3.0.1)" as substrings of the document, which cannot show they
// belong to the same finding, nor in what order the parts appear.
func TestTrivyToSARIF_MessageComposition(t *testing.T) {
	t.Parallel()

	report := &security.TrivyReport{
		Results: []security.TrivyResult{{
			Vulnerabilities: []security.TrivyVulnerability{
				{VulnerabilityID: "CVE-FULL", PkgName: "openssl", InstalledVersion: "3.0.0", FixedVersion: "3.0.1", Severity: "HIGH", Title: "OpenSSL RCE"},
				{VulnerabilityID: "CVE-NOFIX", PkgName: "zlib", InstalledVersion: "1.2.13", Severity: "LOW", Title: "zlib issue"},
				{VulnerabilityID: "CVE-NOVER", PkgName: "musl", Severity: "LOW", Title: "musl issue"},
				{VulnerabilityID: "CVE-NOPKG", Severity: "LOW", Title: "loose finding"},
				// No title: the id stands in for it. The document always
				// carries the id as ruleId, so asserting the message text
				// is the only way to see this fallback at all.
				{VulnerabilityID: "CVE-NOTITLE", PkgName: "p", InstalledVersion: "1.0", Severity: "HIGH"},
				{VulnerabilityID: "CVE-BLANKTITLE", PkgName: "q", Severity: "HIGH", Title: "   "},
			},
		}},
	}

	doc := security.TrivyToSARIF(report, security.Options{})

	for _, tc := range []struct{ ruleID, want string }{
		{"CVE-FULL", "OpenSSL RCE — openssl@3.0.0 (fix: 3.0.1)"},
		{"CVE-NOFIX", "zlib issue — zlib@1.2.13"},
		{"CVE-NOVER", "musl issue — musl"},
		{"CVE-NOPKG", "loose finding"},
		{"CVE-NOTITLE", "CVE-NOTITLE — p@1.0"},
		{"CVE-BLANKTITLE", "CVE-BLANKTITLE — q"},
	} {
		if got, _ := resultByRuleID(t, doc, tc.ruleID); got != tc.want {
			t.Errorf("%s message = %q, want %q", tc.ruleID, got, tc.want)
		}
	}
}

// TestTrivyToSARIF_PhysicalLocationURI covers all three branches of the
// location fallback. The image ref is what lets Code Scanning group
// findings by image, so it has to win when present.
func TestTrivyToSARIF_PhysicalLocationURI(t *testing.T) {
	t.Parallel()

	report := &security.TrivyReport{
		Results: []security.TrivyResult{{
			Vulnerabilities: []security.TrivyVulnerability{
				{VulnerabilityID: "CVE-PKG", PkgName: "lib-foo", Severity: "LOW"},
				{VulnerabilityID: "CVE-BARE", Severity: "LOW"},
			},
		}},
	}

	withoutImage := security.TrivyToSARIF(report, security.Options{})

	if _, got := resultByRuleID(t, withoutImage, "CVE-PKG"); got != "lib-foo" {
		t.Errorf("uri = %q, want the package name", got)
	}

	if _, got := resultByRuleID(t, withoutImage, "CVE-BARE"); got != "unknown" {
		t.Errorf("uri = %q, want unknown", got)
	}

	// An image ref outranks the package name for every finding.
	withImage := security.TrivyToSARIF(report, security.Options{ImageRef: "ghcr.io/example/img@sha256:deadbeef"})
	for _, id := range []string{"CVE-PKG", "CVE-BARE"} {
		if _, got := resultByRuleID(t, withImage, id); got != "ghcr.io/example/img@sha256:deadbeef" {
			t.Errorf("%s uri = %q, want the image ref", id, got)
		}
	}
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
