// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/diggsweden/reusable-ci/internal/cliio"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// UploadSARIFInput drives UploadSARIF.
type UploadSARIFInput struct {
	// SARIFFile is the path to the SARIF JSON. Missing files → skip.
	SARIFFile string
	// Token is the code_scanning_alerts:write token. Empty → skip.
	Token string
	// Repository is "owner/repo" (typically $GITHUB_REPOSITORY).
	Repository string
	// SHA is the commit SHA (typically $GITHUB_SHA).
	SHA string
	// Ref is the full git ref (typically $GITHUB_REF).
	Ref string
	// Category is the SARIF tool category. Empty omits the tool_name
	// field in the request body.
	Category string
}

// UploadSARIF reads a SARIF file from disk and hands it to the
// platform's code-scanning upload via prov.UploadSARIF. The transport
// (HTTP / headers / encoding) lives in the adapter; this use case
// only handles I/O of the SARIF file and the skip rules.
//
// Consistency: the upload is asynchronous from the workflow's
// perspective — GitHub Code Scanning returns 2xx when the SARIF is
// accepted for processing, not when results are visible in the UI or
// queryable via the API. There's typically a seconds-to-minutes
// window before analyses become readable. Downstream steps that
// query Code Scanning must tolerate the lag (or poll explicitly).
//
// Skip semantics (return nil error):
//   - Token is empty
//   - SARIFFile does not exist on disk
//
// Hard failure (return error): required fields missing, transport
// failure. Only github currently implements provider.SARIFUploader —
// the CLI gates on platform before reaching this use case.
//nolint:cyclop // SARIF upload flow: discover → gzip → enrich → upload → summary.
func UploadSARIF(ctx context.Context, prov provider.SARIFUploader, w io.Writer, annot output.Annotator, in UploadSARIFInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.Token == "" {
		annot.Noticef("SARIF upload to Code Scanning skipped — CODE_SCANNING_TOKEN secret is not configured")

		return nil
	}

	if in.SARIFFile == "" {
		annot.Errorf("SARIF file is required (pass --sarif-file <path> or set $SARIF_FILE)")

		return fmt.Errorf("SARIF file is required: pass --sarif-file <path> or set $SARIF_FILE: %w", errs.ErrUsage)
	}
	// Soft-skip when the path points at a non-existent file (workflow
	// safety net). The "-" stdin sentinel bypasses the stat: piped
	// input is always honoured.
	if in.SARIFFile != cliio.StdSentinel {
		if _, err := os.Stat(in.SARIFFile); err != nil {
			annot.Noticef("SARIF file not found: %s — skipping upload", in.SARIFFile)

			return nil //nolint:nilerr // soft-skip: notice already emitted
		}
	}

	if in.Repository == "" {
		annot.Errorf("repository is required (pass --repository <owner/repo> or set $GITHUB_REPOSITORY)")

		return fmt.Errorf("repository is required: pass --repository <owner/repo> or set $GITHUB_REPOSITORY: %w", errs.ErrUsage)
	}

	if in.SHA == "" {
		annot.Errorf("commit SHA is required (pass --sha <hash> or set $GITHUB_SHA)")

		return fmt.Errorf("commit SHA is required: pass --sha <hash> or set $GITHUB_SHA: %w", errs.ErrUsage)
	}

	if in.Ref == "" {
		annot.Errorf("ref is required (pass --ref <refs/heads/X> or set $GITHUB_REF)")

		return fmt.Errorf("ref is required: pass --ref <refs/heads/X> or set $GITHUB_REF: %w", errs.ErrUsage)
	}

	raw, err := cliio.ReadFile(in.SARIFFile)
	if err != nil {
		return fmt.Errorf("read SARIF: %w", err)
	}

	shortSHA := in.SHA
	if len(shortSHA) > 7 {
		shortSHA = shortSHA[:7]
	}

	if in.Category != "" {
		_, _ = fmt.Fprintf(w, "Uploading %s to Code Scanning [%s] (%s @ %s)\n",
			in.SARIFFile, in.Category, in.Repository, shortSHA)
	} else {
		_, _ = fmt.Fprintf(w, "Uploading %s to Code Scanning (%s @ %s)\n",
			in.SARIFFile, in.Repository, shortSHA)
	}

	err = prov.UploadSARIF(ctx, provider.SARIFUpload{
		Repository: in.Repository,
		SHA:        in.SHA,
		Ref:        in.Ref,
		SARIF:      raw,
		Category:   in.Category,
		Token:      in.Token,
	})
	if err != nil {
		annot.Errorf("SARIF upload failed: %v", err)

		return fmt.Errorf("upload sarif: %w", err)
	}

	_, _ = fmt.Fprintln(w, "✓ SARIF accepted by Code Scanning (results appear after async processing)")

	return nil
}
