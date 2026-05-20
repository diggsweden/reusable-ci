// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	scanTypeSAST       = "sast"
	defaultSASTScanner = "sast"
)

// SARIFToGitLabSAST converts a parsed SARIF document (the generic map shape
// json.Unmarshal produces) into the GitLab SAST report schema. Pure: no I/O.
//
// It is the SAST sibling of TrivyToGitLabDep / TrivyToGitLabContainer, used so a
// generic-SARIF producer (nanolinter, which bundles opengrep + other checks)
// can populate the GitLab merge-request Security tab via
// `artifacts:reports:sast` — parity with GitHub Code Scanning. The scanner
// identity is read from the SARIF tool.driver; each finding gets a
// deterministic UUID from (ruleId|file|startLine|message); opts.Now stamps the
// scan times (tests pass a fixed time for golden output).
func SARIFToGitLabSAST(doc map[string]any, opts Options) *GitLabReport {
	scannerID, scannerName, scannerVersion := sarifTool(doc)

	out := buildSASTBase(GitLabScanner{
		ID: scannerID, Name: scannerName, Version: scannerVersion,
		Vendor: GitLabScannerVendor{Name: scannerName},
	}, opts.Now)

	for _, run := range sarifRuns(doc) {
		results, _ := run["results"].([]any)
		for _, raw := range results {
			if result, ok := raw.(map[string]any); ok {
				out.Vulnerabilities = append(out.Vulnerabilities, buildSASTVuln(scannerID, result))
			}
		}
	}

	if out.Vulnerabilities == nil {
		out.Vulnerabilities = []GitLabVulnerability{}
	}

	return out
}

func buildSASTBase(scanner GitLabScanner, now time.Time) *GitLabReport {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	if scanner.Version == "" {
		scanner.Version = "unknown" //nolint:goconst // generic identifier shared with the Trivy transforms.
	}

	stamp := now.Format("2006-01-02T15:04:05")

	return &GitLabReport{
		Version: GitLabSchemaVersion,
		Scan: GitLabScan{
			Scanner: scanner, Analyzer: scanner,
			Type: scanTypeSAST, StartTime: stamp, EndTime: stamp, Status: defaultStatus,
		},
	}
}

func buildSASTVuln(scannerID string, result map[string]any) GitLabVulnerability {
	ruleID, _ := result["ruleId"].(string)
	msg := sarifMessage(result)
	uri, startLine := extractURIAndStartLine(result)
	endLine := extractEndLine(result)

	name := ruleID
	if name == "" {
		name = firstLine(msg)
	}

	if name == "" {
		name = "Finding"
	}

	idValue := ruleID
	if idValue == "" {
		idValue = name
	}

	return GitLabVulnerability{
		ID:          DeterministicUUID(fmt.Sprintf("%s|%s|%d|%s", ruleID, uri, startLine, msg)),
		Name:        name,
		Description: msg,
		Severity:    sarifSeverity(result),
		Identifiers: []GitLabIdentifier{{Type: scannerID + "_rule", Name: idValue, Value: idValue}},
		Links:       []GitLabLink{},
		Location:    GitLabLocation{File: uri, StartLine: startLine, EndLine: endLine},
	}
}

// sarifTool reads the producing scanner's id/name/version from the first run's
// tool.driver. Falls back to a generic "SAST" scanner when the SARIF omits it.
func sarifTool(doc map[string]any) (string, string, string) {
	driver := sarifDriver(doc)

	name, _ := driver["name"].(string)
	if name == "" {
		name = "SAST"
	}

	version, _ := driver["semanticVersion"].(string)
	if version == "" {
		version, _ = driver["version"].(string)
	}

	return sanitizeScannerID(name), name, version
}

// sarifDriver returns the first run's tool.driver map, or nil. Reading a field
// from a nil map yields the zero value, so callers need no extra nil checks.
func sarifDriver(doc map[string]any) map[string]any {
	runs := sarifRuns(doc)
	if len(runs) == 0 {
		return nil
	}

	tool, ok := runs[0]["tool"].(map[string]any)
	if !ok {
		return nil
	}

	driver, _ := tool["driver"].(map[string]any)

	return driver
}

func sanitizeScannerID(name string) string {
	id := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "-")
	if id == "" {
		return defaultSASTScanner
	}

	return id
}

func sarifRuns(doc map[string]any) []map[string]any {
	raw, ok := doc["runs"].([]any)
	if !ok {
		return nil
	}

	out := make([]map[string]any, 0, len(raw))

	for _, item := range raw {
		if run, ok := item.(map[string]any); ok {
			out = append(out, run)
		}
	}

	return out
}

func sarifMessage(result map[string]any) string {
	if msg, ok := result["message"].(map[string]any); ok {
		if text, ok := msg["text"].(string); ok {
			return text
		}
	}

	return ""
}

// sarifSeverity prefers the `properties.security-severity` CVSS score (the
// GitHub Code Scanning convention many tools emit) and otherwise maps the SARIF
// level, so the GitLab Security tab shows a meaningful severity.
func sarifSeverity(result map[string]any) string {
	if props, ok := result["properties"].(map[string]any); ok {
		if score, ok := cvssScore(props["security-severity"]); ok {
			return cvssBand(score)
		}
	}

	switch lvl, _ := result["level"].(string); lvl {
	case sarifLevelError:
		return SeverityHigh
	case sarifLevelWarning:
		return SeverityMedium
	case sarifLevelNote, sarifLevelNone:
		return SeverityInfo
	default:
		return SeverityUnknown
	}
}

func cvssScore(raw any) (float64, bool) {
	switch value := raw.(type) {
	case string:
		score, err := strconv.ParseFloat(strings.TrimSpace(value), 64)

		return score, err == nil
	case float64:
		return value, true
	default:
		return 0, false
	}
}

func cvssBand(score float64) string {
	switch {
	case score >= 9.0:
		return SeverityCritical
	case score >= 7.0:
		return SeverityHigh
	case score >= 4.0:
		return SeverityMedium
	case score > 0.0:
		return SeverityLow
	default:
		return SeverityInfo
	}
}

func extractEndLine(result map[string]any) int {
	region := sarifRegion(result)
	if region == nil {
		return 0
	}

	switch value := region["endLine"].(type) {
	case float64:
		return int(value)
	case int:
		return value
	default:
		return 0
	}
}

func sarifRegion(result map[string]any) map[string]any {
	locations, ok := result["locations"].([]any)
	if !ok || len(locations) == 0 {
		return nil
	}

	loc, ok := locations[0].(map[string]any)
	if !ok {
		return nil
	}

	phys, ok := loc["physicalLocation"].(map[string]any)
	if !ok {
		return nil
	}

	region, _ := phys["region"].(map[string]any)

	return region
}

func firstLine(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return text[:i]
	}

	return text
}
