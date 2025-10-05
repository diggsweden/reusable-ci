// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"bytes"
	"maps"
	"slices"
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

// TestTrivyToSARIF_SerialisesTheExpectedDocumentShape checks that the document
// carries the fields a SARIF consumer requires, and nothing more than that.
//
// It was called MapsSeveritiesToLevels, which is a claim it cannot support:
// every assertion below is a substring search over the serialised bytes, so
// swapping CRITICAL to note and LOW to error leaves all three levels present
// and the test green. A reader scanning for "is the severity mapping tested?"
// would have stopped here and found an answer that was not true.
//
// The mapping itself is asserted per result, structurally, in
// TestTrivyToSARIF_SeverityLevelIsPerResult. This one is named for what it
// does: the schema version, the tool identity, the rule IDs and the image
// reference all reach the output.
func TestTrivyToSARIF_SerialisesTheExpectedDocumentShape(t *testing.T) {
	t.Parallel()
	writeAndAssert(t, sample(), security.Options{
		ImageRef:     "ghcr.io/example/img@sha256:deadbeef",
		TrivyVersion: "0.69.3",
	}, []string{
		`"$schema"`,
		`"version": "2.1.0"`,
		`"name": "Trivy"`,
		`"version": "0.69.3"`,
		// The levels are listed as strings that must be present, not as a
		// mapping: which finding gets which level is not observable here.
		`"ruleId": "CVE-2025-0001"`,
		`"ruleId": "CVE-2025-0002"`,
		`"ruleId": "CVE-2025-0003"`,
		`"level": "error"`,
		`"level": "warning"`,
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
	if !maps.Equal(got, want) {
		t.Errorf("levels = %v, want %v", got, want)
	}
}

func TestTrivyToSARIF_RuleSetIsDeterministicallyOrdered(t *testing.T) {
	t.Parallel()

	doc := security.TrivyToSARIF(sample(), security.Options{})
	if len(doc.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(doc.Runs))
	}

	got := make([]string, 0, len(doc.Runs[0].Tool.Driver.Rules))

	for _, rule := range doc.Runs[0].Tool.Driver.Rules {
		id := ""
		if rule.ID != nil {
			id = *rule.ID
		}

		got = append(got, id)
	}

	// One rule per unique CVE, and CVE ids sort lexicographically.
	want := []string{"CVE-2025-0001", "CVE-2025-0002", "CVE-2025-0003"}
	if !slices.Equal(got, want) {
		t.Errorf("rule ids = %v, want %v", got, want)
	}
}

// TestTrivyToSARIF_ResultsAreSortedByRuleIDThenMessage pins the order the
// doc comment promises: "(CVE id, then message) so retries produce
// byte-identical output". Feeding the same report twice cannot show that,
// because this code path has no map iteration to be unstable — deleting
// the sort entirely leaves such a test passing. Only input whose order
// differs from the wanted output can see the sort at all.
//
// Trivy groups findings per target and does not order them, so the same
// scan re-run can hand us the same CVEs in a different sequence; without
// the sort the SARIF bytes churn and every retry looks like a new report.
func TestTrivyToSARIF_ResultsAreSortedByRuleIDThenMessage(t *testing.T) {
	t.Parallel()

	// Deliberately scrambled: descending ids, and the two CVE-2025-0004
	// findings arrive with the later message first so the tiebreak has
	// something to do.
	report := &security.TrivyReport{
		Results: []security.TrivyResult{
			{Vulnerabilities: []security.TrivyVulnerability{
				{VulnerabilityID: "CVE-2025-0004", PkgName: "zeta", Severity: "LOW", Title: "second"},
				{VulnerabilityID: "CVE-2025-0009", PkgName: "b", Severity: "LOW", Title: "t"},
			}},
			{Vulnerabilities: []security.TrivyVulnerability{
				{VulnerabilityID: "CVE-2025-0004", PkgName: "alpha", Severity: "LOW", Title: "first"},
				{VulnerabilityID: "CVE-2025-0001", PkgName: "a", Severity: "LOW", Title: "t"},
			}},
		},
	}

	doc := security.TrivyToSARIF(report, security.Options{})

	type finding struct{ id, msg string }

	got := make([]finding, 0, len(doc.Runs[0].Results))

	for _, res := range doc.Runs[0].Results {
		got = append(got, finding{id: strOrEmpty(res.RuleID), msg: strOrEmpty(res.Message.Text)})
	}

	want := []finding{
		{id: "CVE-2025-0001", msg: "t — a"},
		{id: "CVE-2025-0004", msg: "first — alpha"},
		{id: "CVE-2025-0004", msg: "second — zeta"},
		{id: "CVE-2025-0009", msg: "t — b"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("result order = %v, want %v", got, want)
	}
}

// TestTrivyToSARIF_IsByteStableAcrossRuns is the property the ordering
// exists to deliver: a re-run of the same scan must produce the same file,
// so a checksummed report does not churn.
func TestTrivyToSARIF_IsByteStableAcrossRuns(t *testing.T) {
	t.Parallel()

	var first, second bytes.Buffer
	if err := security.TrivyToSARIF(sample(), security.Options{}).PrettyWrite(&first); err != nil {
		t.Fatal(err)
	}

	if err := security.TrivyToSARIF(sample(), security.Options{}).PrettyWrite(&second); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Error("identical input produced non-identical SARIF")
	}
}

func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}

	return *p
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
