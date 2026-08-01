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
	"strings"
	"testing"

	appsecurity "github.com/diggsweden/reusable-ci/v3/internal/app/security"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
)

// recordingTrivy captures invocations and optionally pre-stages output files.
type recordingTrivy struct {
	runs       [][]string
	runErr     error
	stageFiles map[string][]byte // path → body to write on first matching invocation
}

func (r *recordingTrivy) RunInherit(_ context.Context, _, _ io.Writer, args ...string) (int, error) {
	r.runs = append(r.runs, args)
	// Stage any files this invocation should produce.
	for path, body := range r.stageFiles {
		// Only stage on the matching invocation by --output arg (mirrors how trivy itself decides).
		for i, a := range args {
			if a == "--output" && i+1 < len(args) && args[i+1] == path {
				_ = os.WriteFile(path, body, 0o644) //nolint:gosec // test fixture
			}
		}
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

	trivy := &recordingTrivy{stageFiles: map[string][]byte{jsonPath: body}}

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

// TestScanContainer_PassesWhenEmptyJSON: trivy's --severity filter
// suppressed everything below threshold; empty JSON = no findings at or
// above the gate. Pipeline passes, no SARIF / GitLab to derive.
func TestScanContainer_PassesWhenEmptyJSON(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "trivy.json")

	trivy := &recordingTrivy{stageFiles: map[string][]byte{jsonPath: nil}}
	if err := appsecurity.ScanContainer(context.Background(), trivy, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appsecurity.ScanContainerInput{
		ImageRef:         "img",
		JSONFile:         jsonPath,
		SARIFFile:        filepath.Join(dir, "trivy.sarif"),
		GitLabReportFile: filepath.Join(dir, "gl.json"),
	}); err != nil {
		t.Fatalf("expected success on empty JSON, got: %v", err)
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
	trivy := &recordingTrivy{stageFiles: map[string][]byte{jsonPath: body}}

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
	err := appsecurity.ScanContainer(context.Background(), &recordingTrivy{}, io.Discard, io.Discard, output.Annotator{}, appsecurity.ScanContainerInput{})
	if err == nil || !strings.Contains(err.Error(), "image-ref is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestScanContainer_PropagatesScanFailure(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "trivy.json")
	trivy := &recordingTrivy{runErr: errors.New("trivy boom")} //nolint:err113 // test mock error

	err := appsecurity.ScanContainer(context.Background(), trivy, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appsecurity.ScanContainerInput{
		ImageRef: "img",
		JSONFile: jsonPath,
	})
	if err == nil || !strings.Contains(err.Error(), "trivy image") {
		t.Fatalf("err = %v", err)
	}
}

// TestScanContainer_ImageScanArgsCarryThresholdAndExitZero pins the
// trivy invocation shape: severity filter applied, --exit-code 0 so the
// gate decision stays in-process, and the image ref carried verbatim.
func TestScanContainer_ImageScanArgsCarryThresholdAndExitZero(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "trivy.json")
	trivy := &recordingTrivy{stageFiles: map[string][]byte{jsonPath: nil}}

	_ = appsecurity.ScanContainer(context.Background(), trivy, &bytes.Buffer{}, &bytes.Buffer{}, output.Annotator{}, appsecurity.ScanContainerInput{
		ImageRef: "ghcr.io/example/img@sha256:deadbeef",
		JSONFile: jsonPath,
		Severity: "CRITICAL",
	})

	if len(trivy.runs) == 0 {
		t.Fatal("no trivy invocation recorded")
	}

	args := trivy.runs[0]
	if args[0] != "image" {
		t.Errorf("invocation = %v, want image scan", args)
	}

	for _, want := range []string{"--severity", "CRITICAL", "--exit-code", "0", "ghcr.io/example/img@sha256:deadbeef"} {
		if !containsArg(args, want) {
			t.Errorf("image args missing %q: %v", want, args)
		}
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}

	return false
}
