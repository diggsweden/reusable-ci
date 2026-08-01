// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"

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
	// SARIFFile is the destination for the SARIF report derived via
	// `trivy convert`. Default: trivy-results.sarif. Skipped when the JSON
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
//  1. `trivy image --format json` → JSONFile  (subprocess — trivy does the scan)
//  2. Parse JSON, count findings at/above Severity → fail when non-zero
//  3. in-process TrivyToSARIF       → SARIFFile (skipped when JSON is empty)
//  4. in-process TrivyToGitLabContainer → GitLabReportFile
//
// Trivy itself is invoked with `--exit-code 0` so the failure decision
// stays inside the Go code (matches the scandeps pattern: trivy reports,
// we gate). SARIF + GitLab transforms still run after a failing gate so
// the Code Scanning tab + GitLab report carry the same findings the
// pipeline rejected on — making the failure visible in the same places
// where adopters look for vulnerability detail.
//
// TRIVY_PLATFORM (when set in the env) is inherited by the trivy
// subprocess automatically; the multi-arch container build pins per-leg
// platforms that way.
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

	_, _ = fmt.Fprintf(out, "🔍 Container vulnerability scan\n")
	_, _ = fmt.Fprintf(out, "   Image: %s\n", in.ImageRef)
	_, _ = fmt.Fprintf(out, "   Severity filter: %s\n\n", severity)

	if err := runTrivyContainerScan(ctx, trivy, out, stderr, jsonPath, severity, in.ImageRef); err != nil {
		return err
	}

	ids, empty, err := loadContainerVulnIDs(jsonPath)
	if err != nil {
		return err
	}

	// Empty JSON means "trivy ran but found nothing at threshold". The
	// pipeline passes; no SARIF / GitLab artefacts to upload.
	if empty || len(ids) == 0 {
		_, _ = fmt.Fprintf(out, "%s No vulnerabilities found at severity %s or above\n", clicolor.Check(out), severity)

		return nil
	}

	// Always derive SARIF + GitLab reports — even when the gate fails,
	// the artefacts must reach Code Scanning so the failure is visible
	// in the same places adopters use for vulnerability triage.
	deriveContainerReports(out, annot, TransformInput{
		InputPath:    jsonPath,
		OutputPath:   sarifPath,
		TrivyVersion: in.TrivyVersion,
		ImageRef:     in.ImageRef,
	}, gitlabPath)

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
	if _, err := trivy.RunInherit(ctx, out, stderr,
		"image",
		"--format", "json",
		"--output", jsonPath,
		"--severity", severity,
		"--vuln-type", "os,library",
		"--exit-code", "0",
		imageRef,
	); err != nil {
		return fmt.Errorf("trivy image: %w", err)
	}

	return nil
}

// loadContainerVulnIDs reads the trivy JSON report. Returns (ids,
// empty, err) where `empty` is true when the JSON file is zero-sized
// (trivy ran but found nothing at the configured severity threshold).
func loadContainerVulnIDs(jsonPath string) ([]string, bool, error) {
	info, err := os.Stat(jsonPath)
	if err != nil {
		return nil, false, fmt.Errorf("stat %s: %w", jsonPath, err)
	}

	if info.Size() == 0 {
		return nil, true, nil
	}

	body, err := os.ReadFile(jsonPath) //nolint:gosec // jsonPath is a locally-built filename.
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", jsonPath, err)
	}

	ids, err := security.ExtractTrivyVulnIDs(body)
	if err != nil {
		return nil, false, fmt.Errorf("extract container vuln ids: %w", err)
	}

	return ids, false, nil
}

// deriveContainerReports derives the SARIF + GitLab container-scanning
// reports from the trivy JSON. Both transforms are best-effort: if
// either fails, the gate still fires below but the artefacts may be
// incomplete. annot.Warningf surfaces the failure.
func deriveContainerReports(out io.Writer, annot output.Annotator, in TransformInput, gitlabPath string) {
	_, _ = fmt.Fprintln(out, "Converting JSON to SARIF...")

	if err := TrivyToSARIF(in); err != nil {
		annot.Warningf("convert to SARIF failed: %v", err)
	}

	_, _ = fmt.Fprintln(out, "Generating GitLab container-scanning report...")

	gitlabIn := in
	gitlabIn.OutputPath = gitlabPath

	if _, err := TrivyToGitLabContainer(gitlabIn); err != nil {
		annot.Warningf("TrivyToGitLabContainer: %v", err)
	}
}
