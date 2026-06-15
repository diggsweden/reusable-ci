// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/diggsweden/reusable-ci/internal/cliio"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/security"
)

// EnrichGitHubSARIFInput drives EnrichGitHubSARIFFile.
type EnrichGitHubSARIFInput struct {
	// Path to the SARIF file. Missing files → skip with a warning.
	Path string
}

// EnrichGitHubSARIFFile reads a SARIF file, populates
// partialFingerprints.primaryLocationLineHash on every result that
// lacks one, and writes the document back to the same path. Missing
// SARIF file is a successful no-op so the workflow step is idempotent.
func EnrichGitHubSARIFFile(w, stderr io.Writer, annot output.Annotator, in EnrichGitHubSARIFInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.Path == "" {
		annot.Errorf("SARIF file is required (pass --sarif-file <path> or set $SARIF_FILE)")

		return fmt.Errorf("SARIF file is required: pass --sarif-file <path> or set $SARIF_FILE: %w", errs.ErrUsage)
	}

	body, err := cliio.ReadFile(in.Path)
	if err != nil {
		// errors.Is (not os.IsNotExist) so the check survives cliio.ReadFile
		// wrapping the os error with its typed sentinel.
		if errors.Is(err, fs.ErrNotExist) {
			annot.Warningf("SARIF file not found, skipping GitHub enrichment: %s", in.Path)

			return nil
		}

		return fmt.Errorf("read %s: %w", in.Path, err)
	}

	out, err := security.EnrichGitHubSARIF(body)
	if err != nil {
		return fmt.Errorf("enrich: %w", err)
	}

	if err := os.WriteFile(in.Path, out, 0o644); err != nil { //nolint:gosec // SARIF read by GitHub Code Scanning; 0644 expected.
		return fmt.Errorf("write %s: %w", in.Path, err)
	}

	_, _ = fmt.Fprintf(w, "Enriched SARIF with GitHub partialFingerprints: %s\n", in.Path)

	return nil
}
