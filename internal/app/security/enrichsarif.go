// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security

import (
	"fmt"
	"io"
	"os"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/security"
)

// EnrichGitHubSARIFInput drives EnrichGitHubSARIFFile.
type EnrichGitHubSARIFInput struct {
	// Path to the SARIF file. Missing files → skip with a warning
	// (matches the bash, which exited 0 with a warning).
	Path string
}

// EnrichGitHubSARIFFile reads a SARIF file, populates
// partialFingerprints.primaryLocationLineHash on every result that
// lacks one, and writes the document back to the same path.
//
// Mirrors scripts/security/enrich-github-sarif.sh — including its
// skip-on-missing behaviour.
func EnrichGitHubSARIFFile(stdout, stderr io.Writer, annot output.Annotator, in EnrichGitHubSARIFInput) error {
	if in.Path == "" {
		annot.Errorf("SARIF_FILE environment variable is required")
		return fmt.Errorf("SARIF_FILE is required: %w", errs.ErrUsage)
	}
	body, err := os.ReadFile(in.Path)
	if err != nil {
		if os.IsNotExist(err) {
			annot.Warningf("SARIF file not found, skipping GitHub enrichment: %s", in.Path)
			return nil
		}
		return fmt.Errorf("read %s: %w", in.Path, err)
	}
	out, err := security.EnrichGitHubSARIF(body)
	if err != nil {
		return fmt.Errorf("enrich: %w", err)
	}
	if err := os.WriteFile(in.Path, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", in.Path, err)
	}
	fmt.Fprintf(stdout, "Enriched SARIF with GitHub partialFingerprints: %s\n", in.Path)
	return nil
}
