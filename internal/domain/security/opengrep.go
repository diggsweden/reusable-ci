// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// OpengrepSeverity is the canonical severity threshold used by
// run-opengrep.sh. The bash accepts a wider set of aliases (none/off/never
// → none, low/info → low, medium/moderate/warning → medium,
// high/critical/error → high) and maps them to this fixed quartet.
type OpengrepSeverity string

// Canonical OpengrepSeverity values.
const (
	OpengrepSeverityNone   OpengrepSeverity = "none"
	OpengrepSeverityLow    OpengrepSeverity = "low"
	OpengrepSeverityMedium OpengrepSeverity = "medium"
	OpengrepSeverityHigh   OpengrepSeverity = "high"
)

// Wire-format scan-result strings. Mirror the canonical
// summary.Result vocabulary but kept as package-locals because the
// domain/security package cannot depend on domain/summary.
const (
	scanResultSuccess = "success"
	scanResultFailure = "failure"
)

// NormalizeOpengrepFailSeverity maps the user-facing severity input
// (case-insensitive, with aliases) onto the canonical four.
func NormalizeOpengrepFailSeverity(in string) (OpengrepSeverity, error) {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "none", "off", "never":
		return OpengrepSeverityNone, nil
	case "low", "info":
		return OpengrepSeverityLow, nil
	case "medium", "moderate", "warning":
		return OpengrepSeverityMedium, nil
	case "high", "critical", "error":
		return OpengrepSeverityHigh, nil
	default:
		return "", fmt.Errorf("unsupported OPENGREP_FAIL_ON_SEVERITY value: %s: %w", in, errs.ErrValidation)
	}
}

// OpengrepHasFindingsMeetingThreshold reports whether the scan should
// fail the workflow given the configured threshold:
//
//   - none   → never fails
//   - low    → fails on any finding
//   - medium → fails on any ERROR or WARNING
//   - high   → fails on any ERROR
func OpengrepHasFindingsMeetingThreshold(threshold OpengrepSeverity, findingsTotal, errorTotal, warningTotal int) bool {
	switch threshold {
	case OpengrepSeverityNone:
		return false
	case OpengrepSeverityLow:
		return findingsTotal > 0
	case OpengrepSeverityMedium:
		return errorTotal > 0 || warningTotal > 0
	case OpengrepSeverityHigh:
		return errorTotal > 0
	default:
		return false
	}
}

// CountOccurrences returns the number of non-overlapping instances of
// needle in body. Used to count opengrep JSON output markers like
// `"severity":"ERROR"` — the bash uses `grep -o ... | wc -l` which
// counts matches not lines, hence we want non-overlapping occurrences.
func CountOccurrences(body, needle string) int {
	if needle == "" {
		return 0
	}

	return strings.Count(body, needle)
}

// OpengrepCounts is what the bash extracts from the JSON output file
// using grep -o counts.
type OpengrepCounts struct {
	FindingsTotal int // `"check_id":` occurrences
	ErrorTotal    int // `"severity":"ERROR"`
	WarningTotal  int // `"severity":"WARNING"`
	InfoTotal     int // `"severity":"INFO"`
}

// CountOpengrepFindings parses the JSON body looking for the four
// occurrence markers the bash relies on. The bash uses substring
// counting rather than JSON parsing for performance and to avoid pulling
// in jq; the Go port does the same so the result is byte-for-byte
// identical.
func CountOpengrepFindings(body string) OpengrepCounts {
	return OpengrepCounts{
		FindingsTotal: CountOccurrences(body, `"check_id":`),
		ErrorTotal:    CountOccurrences(body, `"severity":"ERROR"`),
		WarningTotal:  CountOccurrences(body, `"severity":"WARNING"`),
		InfoTotal:     CountOccurrences(body, `"severity":"INFO"`),
	}
}

// OpengrepPlatformContext drives the code-scanning labels in the
// summary block. The bash takes these from $CI_PLATFORM /
// $HAS_CODE_SCANNING_TOKEN / $CODE_SCANNING_TOKEN.
type OpengrepPlatformContext struct {
	Platform             provider.Platform
	HasCodeScanningToken bool
	RunURL               string
}

// OpengrepCodeScanningLabel returns the value rendered in the
// "Security / Code Scanning" row.
func OpengrepCodeScanningLabel(ctx OpengrepPlatformContext) string {
	switch ctx.Platform {
	case provider.PlatformGitHub:
		if ctx.HasCodeScanningToken {
			return "SARIF generated, upload configured"
		}

		return "SARIF generated, upload not configured"
	case provider.PlatformGitLab:
		return "GitLab SAST artifact generated"
	default:
		return "Portable artifacts only"
	}
}

// OpengrepCodeScanningNote returns the longer-form footnote rendered
// after the table.
func OpengrepCodeScanningNote(ctx OpengrepPlatformContext) string {
	switch ctx.Platform {
	case provider.PlatformGitHub:
		if ctx.HasCodeScanningToken {
			return "SARIF will be uploaded to Security / Code Scanning after the scan step completes."
		}

		return "SARIF is still generated and saved as a workflow artifact. Configure CODE_SCANNING_TOKEN to publish results in Security / Code Scanning."
	case provider.PlatformGitLab:
		return "A GitLab SAST report is generated alongside the portable artifacts."
	default:
		return "Portable artifacts are generated without platform-native upload."
	}
}

// OpengrepWorkflowRunLabel returns either a markdown link to the run
// URL or "n/a" when the URL is unset.
func OpengrepWorkflowRunLabel(runURL string) string {
	if runURL == "" {
		return "n/a"
	}

	return fmt.Sprintf("[View workflow run](%s)", runURL)
}

// OpengrepArtifactsLabel is the static set of artifact file names
// rendered in the summary table.
func OpengrepArtifactsLabel() string {
	return "`opengrep-results.sarif`, `opengrep-results.json`, `opengrep-results.txt`, `opengrep-results.gitlab-sast.json`"
}

// OpengrepSummaryInput drives RenderOpengrepSummary.
type OpengrepSummaryInput struct {
	Config           string
	TargetPath       string
	FailOnSeverity   OpengrepSeverity
	Counts           OpengrepCounts
	ThresholdFailure bool
	TextExcerpt      string // optional — at most 120 lines from the text-output file
	Platform         OpengrepPlatformContext
}

// RenderOpengrepSummary returns the markdown body of the
// successful-scan summary block. The bash chooses between three
// summary-line variants (blocked / below-threshold / clean) based on
// counts; we encode the same decision tree.
func RenderOpengrepSummary(in OpengrepSummaryInput) (string, string) {
	scanResult := scanResultSuccess

	var summaryLine string

	switch {
	case in.ThresholdFailure:
		scanResult = scanResultFailure
		summaryLine = fmt.Sprintf("Blocked by findings meeting threshold `%s`.", in.FailOnSeverity)
	case in.Counts.FindingsTotal > 0:
		summaryLine = fmt.Sprintf("Completed with findings below threshold `%s`.", in.FailOnSeverity)
	default:
		summaryLine = "Passed with `0` findings."
	}

	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "## OpenGrep SAST\n\n")
	_, _ = fmt.Fprintf(&b, "%s\n\n", summaryLine)
	_, _ = fmt.Fprintf(&b, "| Property | Value |\n")
	_, _ = fmt.Fprintf(&b, "|----------|-------|\n")
	_, _ = fmt.Fprintf(&b, "| Rules | %s |\n", in.Config)
	_, _ = fmt.Fprintf(&b, "| Target | %s |\n", in.TargetPath)
	_, _ = fmt.Fprintf(&b, "| Findings | %d |\n", in.Counts.FindingsTotal)
	_, _ = fmt.Fprintf(&b, "| Fail Threshold | %s |\n", in.FailOnSeverity)
	_, _ = fmt.Fprintf(&b, "| Security / Code Scanning | %s |\n", OpengrepCodeScanningLabel(in.Platform))
	_, _ = fmt.Fprintf(&b, "| Workflow Run | %s |\n", OpengrepWorkflowRunLabel(in.Platform.RunURL))
	_, _ = fmt.Fprintf(&b, "| Artifacts | %s |\n", OpengrepArtifactsLabel())
	_, _ = fmt.Fprintf(&b, "| Scan Result | %s |\n", scanResult)
	_, _ = fmt.Fprintf(&b, "\n%s\n", OpengrepCodeScanningNote(in.Platform))

	if in.Counts.FindingsTotal > 0 {
		_, _ = fmt.Fprintf(&b, "\n| Severity | Count |\n")
		_, _ = fmt.Fprintf(&b, "|----------|-------|\n")
		_, _ = fmt.Fprintf(&b, "| ERROR | %d |\n", in.Counts.ErrorTotal)
		_, _ = fmt.Fprintf(&b, "| WARNING | %d |\n", in.Counts.WarningTotal)
		_, _ = fmt.Fprintf(&b, "| INFO | %d |\n", in.Counts.InfoTotal)
	}

	if in.Counts.FindingsTotal > 0 && in.TextExcerpt != "" {
		_, _ = fmt.Fprintf(&b, "\n<details>\n<summary>OpenGrep findings excerpt</summary>\n\n<pre>\n")
		_, _ = fmt.Fprint(&b, in.TextExcerpt)
		_, _ = fmt.Fprintf(&b, "\n</pre>\n</details>\n")
	}

	return b.String(), scanResult
}

// OpengrepFailureSummaryInput drives RenderOpengrepFailureSummary —
// the variant the bash writes when the scan itself exited non-zero.
type OpengrepFailureSummaryInput struct {
	Config     string
	TargetPath string
	ExitCode   int
	Platform   OpengrepPlatformContext
}

// RenderOpengrepFailureSummary returns the markdown body of the
// scan-failed summary (no findings table because no JSON was emitted).
func RenderOpengrepFailureSummary(in OpengrepFailureSummaryInput) string {
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "## OpenGrep SAST\n\n")
	_, _ = fmt.Fprintf(&b, "OpenGrep exited with status %d before a complete result set was produced.\n\n", in.ExitCode)
	_, _ = fmt.Fprintf(&b, "| Property | Value |\n")
	_, _ = fmt.Fprintf(&b, "|----------|-------|\n")
	_, _ = fmt.Fprintf(&b, "| Rules | %s |\n", in.Config)
	_, _ = fmt.Fprintf(&b, "| Target | %s |\n", in.TargetPath)
	_, _ = fmt.Fprintf(&b, "| Security / Code Scanning | %s |\n", OpengrepCodeScanningLabel(in.Platform))
	_, _ = fmt.Fprintf(&b, "| Workflow Run | %s |\n", OpengrepWorkflowRunLabel(in.Platform.RunURL))
	_, _ = fmt.Fprintf(&b, "| Artifacts | %s |\n", OpengrepArtifactsLabel())
	_, _ = fmt.Fprintf(&b, "| Scan Result | failure |\n")

	return b.String()
}

// ParseConfigList splits a comma-separated config string and trims
// whitespace from each entry. Empty entries are dropped. Mirrors the
// bash `build_config_args` logic.
//
// Returns an error when no non-empty configs remain.
func ParseConfigList(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")

	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" {
			continue
		}

		out = append(out, t)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("at least one OpenGrep config is required: %w", errs.ErrUsage)
	}

	return out, nil
}

// HeadN returns the first n newline-terminated entries of s. Used to
// approximate `sed -n '1,120p'` for the text-excerpt embed.
func HeadN(s string, n int) string { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if n <= 0 {
		return ""
	}

	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}

	return strings.Join(lines, "\n")
}
