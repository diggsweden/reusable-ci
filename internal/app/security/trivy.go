// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package security wires `reusable-ci security <subcmd>` to the domain
// transforms. Each subcommand is one file.
package security

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
)

// reportTimestamp returns the timestamp baked into security-report
// metadata. SOURCE_DATE_EPOCH (seconds since UNIX epoch,
// reproducible-builds.org convention) wins when set so two runs over
// the same trivy input produce byte-identical reports; otherwise
// time.Now().UTC() is used. Mirrors resolveBuildDate in app/build/go.go
// — keeping the same env-var contract across artifact production AND
// security-report production means a deterministic pipeline run is
// reproducible end-to-end.
func reportTimestamp() time.Time {
	if raw := strings.TrimSpace(os.Getenv("SOURCE_DATE_EPOCH")); raw != "" {
		if secs, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return time.Unix(secs, 0).UTC()
		}
	}

	return time.Now().UTC()
}

// TransformInput drives both the dep and container transforms.
type TransformInput struct {
	InputPath    string
	OutputPath   string
	TrivyVersion string
	ImageRef     string // container only; ignored by the dep transform
}

// TrivyToGitLabDep loads the Trivy JSON at InputPath, transforms it to
// the dependency-scanning shape, and writes the result to OutputPath.
// Returns the number of findings written.
func TrivyToGitLabDep(in TransformInput) (int, error) {
	report, err := loadTrivy(in.InputPath)
	if err != nil {
		return 0, err
	}

	gl := security.TrivyToGitLabDep(report, security.Options{
		TrivyVersion: in.TrivyVersion,
		Now:          reportTimestamp(),
	})
	if err := writeJSON(in.OutputPath, gl); err != nil {
		return 0, err
	}

	return len(gl.Vulnerabilities), nil
}

// TrivyToGitLabContainer loads the Trivy JSON at InputPath, transforms
// it to the container-scanning shape (location.image / operating_system),
// and writes the result.
func TrivyToGitLabContainer(in TransformInput) (int, error) {
	report, err := loadTrivy(in.InputPath)
	if err != nil {
		return 0, err
	}

	gl := security.TrivyToGitLabContainer(report, security.Options{
		TrivyVersion: in.TrivyVersion,
		ImageRef:     in.ImageRef,
		Now:          reportTimestamp(),
	})
	if err := writeJSON(in.OutputPath, gl); err != nil {
		return 0, err
	}

	return len(gl.Vulnerabilities), nil
}

// TrivyToSARIF loads the Trivy JSON at InputPath, transforms it via the
// in-process SARIF builder, and writes the SARIF v2.1.0 document to
// OutputPath. Replaces `trivy convert --format sarif` — eliminates one
// subprocess per container scan and removes trivy as a runtime
// dependency for the conversion step.
func TrivyToSARIF(in TransformInput) error {
	report, err := loadTrivy(in.InputPath)
	if err != nil {
		return err
	}

	doc := security.TrivyToSARIF(report, security.Options{
		TrivyVersion: in.TrivyVersion,
		ImageRef:     in.ImageRef,
		Now:          reportTimestamp(),
	})

	out, err := os.OpenFile(in.OutputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644) //nolint:gosec // SARIF report read by workflow / Code Scanning.
	if err != nil {
		return fmt.Errorf("create %q: %w", in.OutputPath, err)
	}

	defer func() { _ = out.Close() }()

	if err := doc.PrettyWrite(out); err != nil {
		return fmt.Errorf("write SARIF: %w", err)
	}

	return nil
}

func loadTrivy(path string) (*security.TrivyReport, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is a CLI-flag value.
	if err != nil {
		return nil, fmt.Errorf("read trivy report %q: %w", path, err)
	}

	var r security.TrivyReport
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse trivy report %q: %w", path, err)
	}

	return &r, nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil { //nolint:gosec // GitLab dependency-scan report read by runner.
		return fmt.Errorf("write %q: %w", path, err)
	}

	return nil
}
