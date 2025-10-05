// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
)

// ScanContainerInput drives ScanContainer.
type ScanContainerInput struct {
	// ImageRef is the image to scan (registry/name@digest or registry/name:tag).
	// Required.
	ImageRef string
	// JSONFile is the destination for the raw trivy JSON output.
	// Default: trivy-results.json.
	JSONFile string
	// SARIFFile is the destination for the SARIF report derived in-process.
	// Default: trivy-results.sarif. Skipped when the parsed JSON
	// scan returned zero findings.
	SARIFFile string
	// GitLabReportFile is the destination for the GitLab
	// container-scanning report derived via the in-process transform.
	// Default: gl-container-scanning-report.json.
	GitLabReportFile string
	// TrivyVersion is embedded into the GitLab report metadata.
	TrivyVersion string
	// Severity is the trivy --severity filter AND the fail-on threshold.
	// Any finding at this severity or above fails the scan. Default:
	// "CRITICAL,HIGH". To skip the gate entirely, set the workflow's
	// `enable-scan: false`; narrowing the filter (e.g. "CRITICAL") is the
	// way to relax the threshold without disabling the scan.
	Severity string
}

// ScanContainer orchestrates a container scan in four steps:
//  1. `trivy image --format json` → private report (trivy does the scan)
//  2. Parse JSON, count findings at/above Severity → fail when non-zero
//  3. in-process TrivyToSARIF       → SARIFFile (skipped with no findings)
//  4. in-process TrivyToGitLabContainer → GitLabReportFile
//
// Trivy itself is invoked with `--exit-code 0` so the failure decision
// stays inside the Go code (matches the scandeps pattern: trivy reports,
// we gate). SARIF + GitLab transforms still run after a failing gate so
// the Code Scanning tab + GitLab report carry the same findings the
// pipeline rejected on — making the failure visible in the same places
// where adopters look for vulnerability detail.
// Configured output files represent this invocation: previous files are retired
// after preflight; fresh validated reports are installed individually.
//
// TRIVY_PLATFORM (when set in the env) is inherited by the trivy
// subprocess automatically; the multi-arch container build pins per-leg
// platforms that way.
//
//nolint:cyclop // validate the severity vocabulary before the scan/report phases.
func ScanContainer(
	ctx context.Context,
	trivy TrivyOps,
	out, stderr io.Writer,
	annot output.Annotator,
	in ScanContainerInput,
) error {
	if in.ImageRef == "" {
		return fmt.Errorf("image-ref is required: %w", errs.ErrUsage)
	}

	jsonPath := cmp.Or(in.JSONFile, security.DefaultTrivyContainerJSONFile)
	sarifPath := cmp.Or(in.SARIFFile, security.DefaultTrivyContainerSARIFFile)
	gitlabPath := cmp.Or(in.GitLabReportFile, security.DefaultTrivyGitLabContainerFile)
	severity := cmp.Or(in.Severity, "CRITICAL,HIGH")

	parts := strings.Split(severity, ",")
	for index, part := range parts {
		part = strings.ToUpper(strings.TrimSpace(part))
		switch part {
		case "UNKNOWN", "LOW", "MEDIUM", "HIGH", "CRITICAL":
			parts[index] = part
		default:
			return fmt.Errorf("unsupported container scan severity %q: %w", part, errs.ErrUsage)
		}
	}

	severity = strings.Join(parts, ",")

	workDir, err := prepareScanReports(jsonPath, sarifPath, gitlabPath)
	if err != nil {
		return err
	}

	defer func() { _ = os.RemoveAll(workDir) }()

	rawReport := filepath.Join(workDir, "raw.json")

	_, _ = fmt.Fprintf(out, "🔍 Container vulnerability scan\n")
	_, _ = fmt.Fprintf(out, "   Image: %s\n", in.ImageRef)
	_, _ = fmt.Fprintf(out, "   Severity filter: %s\n\n", severity)

	if scanErr := runTrivyContainerScan(ctx, trivy, out, stderr, rawReport, severity, in.ImageRef); scanErr != nil {
		return scanErr
	}

	ids, err := loadContainerVulnIDs(rawReport)
	if err != nil {
		return err
	}

	if err := publishScanReport(rawReport, jsonPath, true); err != nil {
		return err
	}
	// Only a parsed report with no findings is clean; empty bytes are not a scan.
	if len(ids) == 0 {
		_, _ = fmt.Fprintf(out, "%s No vulnerabilities found at severity %s or above\n", clicolor.Check(out), severity)

		return nil
	}

	// Always derive SARIF + GitLab reports — even when the gate fails,
	// the artifacts must reach Code Scanning so the failure is visible
	// in the same places adopters use for vulnerability triage.
	deriveContainerReports(out, annot, TransformInput{
		InputPath:    rawReport,
		OutputPath:   filepath.Join(workDir, "report.sarif"),
		TrivyVersion: in.TrivyVersion,
		ImageRef:     in.ImageRef,
	}, filepath.Join(workDir, "gitlab.json"), sarifPath, gitlabPath)

	noun := "vulnerabilities"
	if len(ids) == 1 {
		noun = "vulnerability"
	}

	msg := fmt.Sprintf("Found %d container %s at severity %s or above", len(ids), noun, severity)
	annot.Errorf("%s", msg)
	// Domain rule failure (scan threshold exceeded) — wrap so the CLI
	// exits with ExitCodeValidation (1), matching the scandeps verdict.
	return fmt.Errorf("%s: %w", msg, errs.ErrValidation)
}

// runTrivyContainerScan invokes `trivy image --format json` against
// the image ref and writes the JSON output to jsonPath.
// `--exit-code 0` keeps the scan-gate decision inside the Go code.
func runTrivyContainerScan(ctx context.Context, trivy TrivyOps, out, stderr io.Writer, jsonPath, severity, imageRef string) error {
	code, err := trivy.RunInherit(ctx, out, stderr,
		"image",
		"--format", "json",
		"--output", jsonPath,
		"--severity", severity,
		"--vuln-type", "os,library",
		"--exit-code", "0",
		imageRef,
	)
	if err != nil {
		return fmt.Errorf("trivy image: %w", err)
	}

	if code != 0 {
		return fmt.Errorf("trivy image exited with status %d: %w", code, errs.ErrDependencyUnavailable)
	}

	return nil
}

// loadContainerVulnIDs reads a fresh, bounded, regular Trivy report.
func loadContainerVulnIDs(jsonPath string) ([]string, error) {
	body, err := readJSONScanReport(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", jsonPath, err)
	}

	ids, err := security.ExtractTrivyVulnIDs(body)
	if err != nil {
		return nil, fmt.Errorf("extract container vuln ids: %w", err)
	}

	return ids, nil
}

// deriveContainerReports derives the SARIF + GitLab container-scanning
// reports from the trivy JSON. Both transforms are best-effort: if
// either fails, the gate still fires below but the artifacts may be
// incomplete. annot.Warningf surfaces the failure.
func deriveContainerReports(out io.Writer, annot output.Annotator, in TransformInput, gitlabPath, sarifDestination, gitlabDestination string) {
	_, _ = fmt.Fprintln(out, "Converting JSON to SARIF...")

	if err := TrivyToSARIF(in); err != nil {
		annot.Warningf("convert to SARIF failed: %v", err)
	} else if err := publishScanReport(in.OutputPath, sarifDestination, true); err != nil {
		annot.Warningf("publish SARIF failed: %v", err)
	}

	_, _ = fmt.Fprintln(out, "Generating GitLab container-scanning report...")

	gitlabIn := in
	gitlabIn.OutputPath = gitlabPath

	if _, err := TrivyToGitLabContainer(gitlabIn); err != nil {
		annot.Warningf("TrivyToGitLabContainer: %v", err)
	} else if err := publishScanReport(gitlabPath, gitlabDestination, true); err != nil {
		annot.Warningf("publish GitLab container report failed: %v", err)
	}
}
