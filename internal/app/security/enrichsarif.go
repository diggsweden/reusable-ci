// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security

import (
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
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

	// The document is written back in place, so stdin cannot be the input: the
	// write would land in a file literally named "-".
	if in.Path == cliio.StdSentinel {
		annot.Errorf("SARIF file must be a path, not \"-\": enrichment rewrites the file in place")

		return fmt.Errorf("SARIF file must be a path, not %q: enrichment rewrites the file in place: %w", cliio.StdSentinel, errs.ErrUsage)
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

	// Atomic, because the rewrite replaces the only copy: a write cut short
	// would leave a truncated document that the rerun can no longer parse.
	if err := cliio.WriteFile(in.Path, out, 0o644); err != nil { //nolint:gosec // SARIF read by GitHub Code Scanning; 0644 expected.
		return fmt.Errorf("write %s: %w", in.Path, err)
	}

	_, _ = fmt.Fprintf(w, "Enriched SARIF with GitHub partialFingerprints: %s\n", in.Path)

	return nil
}
