// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
)

// SARIFToGitLabSAST loads the SARIF document at InputPath, transforms it to
// the GitLab SAST report shape, and writes the result to OutputPath. Returns
// the number of findings written. The scanner identity is read from the
// SARIF tool.driver, so TrivyVersion / ImageRef on TransformInput are ignored.
func SARIFToGitLabSAST(in TransformInput) (int, error) {
	doc, err := loadSARIF(in.InputPath)
	if err != nil {
		return 0, err
	}

	gl := security.SARIFToGitLabSAST(doc, security.Options{Now: reportTimestamp()})
	if err := writeJSON(in.OutputPath, gl); err != nil {
		return 0, err
	}

	return len(gl.Vulnerabilities), nil
}

func loadSARIF(path string) (map[string]any, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is a CLI-flag value.
	if err != nil {
		return nil, fmt.Errorf("read sarif report %q: %w", path, err)
	}

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse sarif report %q: %w", path, err)
	}

	return doc, nil
}
