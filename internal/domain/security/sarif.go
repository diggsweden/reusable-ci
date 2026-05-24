// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security

import (
	"sort"
	"strings"

	"github.com/owenrumney/go-sarif/v3/pkg/report/v210/sarif"
)

// TrivyToSARIF builds a SARIF v2.1.0 report from a Trivy JSON report.
// Replaces the `trivy convert --format sarif` subprocess with an
// in-process transform.
//
// Mapping (matches what trivy's own SARIF emitter does for the fields
// GitHub Code Scanning relies on):
//
//   - Tool: driver.name="Trivy", driver.version=opts.TrivyVersion
//
//   - Rules: one ReportingDescriptor per unique CVE id, sorted for
//     deterministic byte output
//
//   - Results: one per vulnerability finding, with:
//     ruleId       = CVE id
//     level        = severity → SARIF level mapping (see trivySeverityLevel)
//     message.text = "<Title> — <PkgName>@<InstalledVersion>" (+fix advice)
//     location     = artifactLocation.uri = ImageRef ("registry/name@digest")
//
//   - Results sort order is stable (CVE id, then message) so retries
//     produce byte-identical output.
func TrivyToSARIF(report *TrivyReport, opts Options) *sarif.Report {
	if report == nil {
		report = &TrivyReport{}
	}

	rules := newRuleSet()
	results := make([]*sarif.Result, 0, 64)

	for i := range report.Results {
		res := &report.Results[i]
		for j := range res.Vulnerabilities {
			v := &res.Vulnerabilities[j]
			rules.add(v)
			results = append(results, buildSARIFResult(v, opts.ImageRef))
		}
	}

	sortSARIFResults(results)

	driver := sarif.NewToolComponent().
		WithName("Trivy").
		WithInformationURI("https://github.com/aquasecurity/trivy").
		WithRules(rules.sorted())
	if v := strings.TrimSpace(opts.TrivyVersion); v != "" {
		driver = driver.WithVersion(v)
	}

	run := sarif.NewRun().
		WithTool(sarif.NewTool().WithDriver(driver)).
		WithResults(results)
	out := sarif.NewReport()
	out.AddRun(run)

	return out
}

// trivySeverityLevel maps Trivy's severity string onto SARIF's level
// enum (note|warning|error|none). The mapping matches trivy's own
// SARIF emitter so Code Scanning's severity colouring stays unchanged.
func trivySeverityLevel(severity string) string {
	switch strings.ToUpper(severity) {
	case "CRITICAL", "HIGH": //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		return "error"
	case "MEDIUM":
		return "warning"
	case "LOW":
		return "note"
	default:
		return "none"
	}
}

// buildSARIFResult constructs one SARIF Result from a Trivy
// vulnerability finding. imageRef populates the result's physical
// location so Code Scanning anchors the finding to the right image.
func buildSARIFResult(v *TrivyVulnerability, imageRef string) *sarif.Result { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	loc := sarif.NewLocation().WithPhysicalLocation(
		sarif.NewPhysicalLocation().
			WithArtifactLocation(sarif.NewSimpleArtifactLocation(physicalLocationURI(imageRef, v))).
			WithRegion(sarif.NewRegion().WithStartLine(1)),
	)

	return sarif.NewResult().
		WithRuleID(v.VulnerabilityID).
		WithLevel(trivySeverityLevel(v.Severity)).
		WithMessage(sarif.NewMessage().WithText(vulnerabilityMessage(v))).
		WithLocations([]*sarif.Location{loc})
}

// vulnerabilityMessage composes a human-readable Code Scanning message:
// "<Title> — <pkg>@<version> (fix: <fixed>)" with sane fall-backs when
// Title is missing.
func vulnerabilityMessage(v *TrivyVulnerability) string { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	title := strings.TrimSpace(v.Title)
	if title == "" {
		title = v.VulnerabilityID
	}

	b.WriteString(title)

	if v.PkgName != "" {
		b.WriteString(" — ")
		b.WriteString(v.PkgName)

		if v.InstalledVersion != "" {
			b.WriteString("@")
			b.WriteString(v.InstalledVersion)
		}
	}

	if v.FixedVersion != "" {
		b.WriteString(" (fix: ")
		b.WriteString(v.FixedVersion)
		b.WriteString(")")
	}

	return b.String()
}

// physicalLocationURI picks the URI for a finding's location. Container
// scans get the image ref so Code Scanning's "this finding is in image
// X" grouping works; if no image ref is set we fall back to the package
// name (still a stable identifier per finding).
func physicalLocationURI(imageRef string, v *TrivyVulnerability) string { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if imageRef != "" {
		return imageRef
	}

	if v.PkgName != "" {
		return v.PkgName
	}

	return "unknown" //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
}

// ruleSet accumulates unique CVE rule descriptors keyed by id.
type ruleSet struct {
	byID map[string]*sarif.ReportingDescriptor
}

func newRuleSet() *ruleSet {
	return &ruleSet{byID: make(map[string]*sarif.ReportingDescriptor, 16)}
}

func (s *ruleSet) add(v *TrivyVulnerability) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if v.VulnerabilityID == "" {
		return
	}

	if _, ok := s.byID[v.VulnerabilityID]; ok {
		return
	}

	title := strings.TrimSpace(v.Title)
	if title == "" {
		title = v.VulnerabilityID
	}

	rule := sarif.NewReportingDescriptor().
		WithID(v.VulnerabilityID).
		WithName(v.VulnerabilityID).
		WithShortDescription(sarif.NewMultiformatMessageString().WithText(title))
	if desc := strings.TrimSpace(v.Description); desc != "" {
		rule = rule.WithFullDescription(sarif.NewMultiformatMessageString().WithText(desc))
	}

	if v.PrimaryURL != "" {
		rule = rule.WithHelpURI(v.PrimaryURL)
	}

	s.byID[v.VulnerabilityID] = rule
}

// sorted returns the descriptors in stable id order so retries produce
// byte-identical SARIF.
func (s *ruleSet) sorted() []*sarif.ReportingDescriptor {
	ids := make([]string, 0, len(s.byID))
	for id := range s.byID {
		ids = append(ids, id)
	}

	sort.Strings(ids)

	out := make([]*sarif.ReportingDescriptor, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.byID[id])
	}

	return out
}

// sortSARIFResults orders by ruleId, then by message text — deterministic
// across runs.
func sortSARIFResults(results []*sarif.Result) {
	sort.SliceStable(results, func(i, j int) bool { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		ri := strFromPtr(results[i].RuleID)

		rj := strFromPtr(results[j].RuleID)
		if ri != rj {
			return ri < rj
		}

		mi := messageText(results[i])
		mj := messageText(results[j])

		return mi < mj
	})
}

func messageText(r *sarif.Result) string {
	if r == nil || r.Message.Text == nil {
		return ""
	}

	return *r.Message.Text
}

func strFromPtr(p *string) string {
	if p == nil {
		return ""
	}

	return *p
}
