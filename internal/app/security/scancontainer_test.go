// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	appsecurity "github.com/diggsweden/reusable-ci/v3/internal/app/security"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
)

// recordingTrivy captures invocations and optionally pre-stages output files.
type recordingTrivy struct {
	runs      [][]string
	runErr    error
	writeJSON []byte // non-nil writes this report at the requested private output path
}

func (r *recordingTrivy) RunInherit(_ context.Context, _, _ io.Writer, args ...string) (int, error) {
	r.runs = append(r.runs, append([]string{}, args...))
	if r.writeJSON != nil {
		writeFixtureReport(scanOutput(args), string(r.writeJSON), args)
	}

	return 0, r.runErr
}

// TestScanContainer_FailsWhenFindingsAtThreshold is the deployable-pipeline
// guarantee: if trivy reports any vulnerability at the configured severity
// or above, the workflow fails with ErrValidation. SARIF + GitLab reports
// are still derived so Code Scanning gets the finding detail.
func TestScanContainer_FailsWhenFindingsAtThreshold(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "trivy.json")
	sarifPath := filepath.Join(dir, "trivy.sarif")
	glPath := filepath.Join(dir, "gl.json")

	body := []byte(`{
        "ArtifactName": "ghcr.io/example/img",
        "Results": [{
            "Target": "alpine",
            "Vulnerabilities": [{
                "VulnerabilityID": "CVE-2025-0001",
                "PkgName": "openssl",
                "InstalledVersion": "3.0.0",
                "FixedVersion": "3.0.1",
                "Severity": "HIGH",
                "Title": "OpenSSL bug"
            }]
        }]
    }`)

	trivy := &recordingTrivy{writeJSON: body}

	err := appsecurity.ScanContainer(context.Background(), trivy, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appsecurity.ScanContainerInput{
		ImageRef:         "ghcr.io/example/img@sha256:deadbeef",
		JSONFile:         jsonPath,
		SARIFFile:        sarifPath,
		GitLabReportFile: glPath,
		Severity:         "CRITICAL,HIGH",
	})
	if err == nil {
		t.Fatal("expected error when HIGH finding at threshold, got nil")
	}

	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("err = %v, want wrapped ErrValidation", err)
	}

	if !strings.Contains(err.Error(), "1 container vulnerability") {
		t.Errorf("err message should mention finding count: %v", err)
	}

	// SARIF + GitLab reports must be present so the failure is visible
	// in Code Scanning + GitLab. The pipeline rejects the artifact, but
	// the rejection comes with full triage detail attached.
	if _, statErr := os.Stat(sarifPath); statErr != nil {
		t.Errorf("SARIF should be written even when gate fails: %v", statErr)
	}

	if _, statErr := os.Stat(glPath); statErr != nil {
		t.Errorf("GitLab report should be written even when gate fails: %v", statErr)
	}
}

// Empty bytes are missing scan evidence, not a clean JSON report.
func TestScanContainer_RejectsEmptyJSON(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "trivy.json")

	trivy := &recordingTrivy{writeJSON: []byte{}}
	if err := appsecurity.ScanContainer(context.Background(), trivy, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appsecurity.ScanContainerInput{
		ImageRef:         "img",
		JSONFile:         jsonPath,
		SARIFFile:        filepath.Join(dir, "trivy.sarif"),
		GitLabReportFile: filepath.Join(dir, "gl.json"),
	}); !errors.Is(err, errs.ErrMalformedInput) {
		t.Fatalf("expected malformed-input refusal on empty JSON, got: %v", err)
	}

	if len(trivy.runs) != 1 {
		t.Errorf("expected only the image scan when JSON is empty, got %d invocations", len(trivy.runs))
	}
}

// TestScanContainer_PassesWhenJSONHasNoVulns: trivy emitted JSON with
// ArtifactName / Results metadata but no actual vulnerabilities (e.g.
// the per-arch trivy output for a clean image). Pipeline passes.
func TestScanContainer_PassesWhenJSONHasNoVulns(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "trivy.json")
	sarifPath := filepath.Join(dir, "trivy.sarif")
	glPath := filepath.Join(dir, "gl.json")

	body := []byte(`{"ArtifactName": "ghcr.io/example/img", "Results": [{"Target": "alpine", "Vulnerabilities": []}]}`)
	trivy := &recordingTrivy{writeJSON: body}

	if err := appsecurity.ScanContainer(context.Background(), trivy, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appsecurity.ScanContainerInput{
		ImageRef:         "ghcr.io/example/img@sha256:deadbeef",
		JSONFile:         jsonPath,
		SARIFFile:        sarifPath,
		GitLabReportFile: glPath,
		Severity:         "CRITICAL,HIGH",
	}); err != nil {
		t.Fatalf("expected success on zero-vuln JSON, got: %v", err)
	}
}

func TestScanContainer_RejectsMissingImageRef(t *testing.T) {
	trivy := &recordingTrivy{}

	err := appsecurity.ScanContainer(context.Background(), trivy, io.Discard, io.Discard, output.Annotator{}, appsecurity.ScanContainerInput{})
	// A missing --image-ref is a broken invocation, not a finding: ErrUsage
	// exits 2 rather than 1, which would read as "the image is vulnerable".
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if !strings.Contains(err.Error(), "image-ref is required") {
		t.Errorf("err = %v, want it to name the missing flag", err)
	}

	if len(trivy.runs) != 0 {
		t.Errorf("scanned without an image ref: %v", trivy.runs)
	}
}

func TestScanContainer_PropagatesScanFailure(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "trivy.json")
	boom := errors.New("trivy boom") //nolint:err113 // test mock error
	trivy := &recordingTrivy{runErr: boom}

	err := appsecurity.ScanContainer(context.Background(), trivy, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appsecurity.ScanContainerInput{
		ImageRef:         "img",
		JSONFile:         jsonPath,
		SARIFFile:        filepath.Join(dir, "report.sarif"),
		GitLabReportFile: filepath.Join(dir, "gitlab.json"),
	})
	// The cause has to survive the wrap, or an operator debugging a broken
	// scanner sees only "trivy image" with no reason attached.
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap %v", err, boom)
	}

	if !strings.Contains(err.Error(), "trivy image") {
		t.Errorf("err = %v, want it to name the failing step", err)
	}
}

// TestScanContainer_ImageScanArgsCarryThresholdAndExitZero pins the
// trivy invocation shape: severity filter applied, --exit-code 0 so the
// gate decision stays in-process, and the image ref carried verbatim.
func TestScanContainer_ImageScanArgsCarryThresholdAndExitZero(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "trivy.json")
	trivy := &recordingTrivy{writeJSON: []byte(`{"Results":[]}`)}

	if err := appsecurity.ScanContainer(context.Background(), trivy, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appsecurity.ScanContainerInput{
		ImageRef:         "ghcr.io/example/img@sha256:deadbeef",
		JSONFile:         jsonPath,
		Severity:         "CRITICAL",
		SARIFFile:        filepath.Join(dir, "report.sarif"),
		GitLabReportFile: filepath.Join(dir, "gitlab.json"),
	}); err != nil {
		t.Fatal(err)
	}

	if len(trivy.runs) != 1 {
		t.Fatalf("trivy invocations = %d, want exactly the image scan", len(trivy.runs))
	}

	privateOutput := scanOutput(trivy.runs[0])
	if privateOutput == jsonPath || !filepath.IsAbs(privateOutput) {
		t.Fatalf("scan did not use a private absolute report: %q", privateOutput)
	}

	// The whole invocation, not a membership check per flag: a set test
	// cannot tell "--severity CRITICAL" from "--severity" followed by some
	// other value with CRITICAL sitting elsewhere in the line.
	want := []string{
		"image",
		"--format", "json",
		"--output", privateOutput,
		"--severity", "CRITICAL",
		"--vuln-type", "os,library",
		"--exit-code", "0",
		"ghcr.io/example/img@sha256:deadbeef",
	}
	if !slices.Equal(trivy.runs[0], want) {
		t.Errorf("image args =\n%q\nwant\n%q", trivy.runs[0], want)
	}
}
