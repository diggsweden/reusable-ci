// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package security wires `reusable-ci security <subcmd>` to the domain
// transforms. Each subcommand is one file.
package security

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/security"
)

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
		Now:          time.Now().UTC(),
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
		Now:          time.Now().UTC(),
	})
	if err := writeJSON(in.OutputPath, gl); err != nil {
		return 0, err
	}
	return len(gl.Vulnerabilities), nil
}

func loadTrivy(path string) (*security.TrivyReport, error) {
	data, err := os.ReadFile(path)
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
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}
