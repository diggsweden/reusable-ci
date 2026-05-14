// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

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
// Mirrors scripts/security/upload-sarif.sh end-to-end.
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
// failure. Adapters that don't support SARIF upload (gitlab, local)
// surface errs.ErrUnsupported — callers either propagate or branch.
func UploadSARIF(ctx context.Context, prov provider.Provider, stdout io.Writer, annot output.Annotator, in UploadSARIFInput) error {
	if in.Token == "" {
		annot.Noticef("SARIF upload to Code Scanning skipped — CODE_SCANNING_TOKEN secret is not configured")
		return nil
	}
	if in.SARIFFile == "" {
		annot.Errorf("SARIF_FILE environment variable is required")
		return fmt.Errorf("SARIF_FILE is required: %w", errs.ErrUsage)
	}
	if _, err := os.Stat(in.SARIFFile); err != nil {
		annot.Noticef("SARIF file not found: %s — skipping upload", in.SARIFFile)
		return nil
	}
	if in.Repository == "" {
		annot.Errorf("GITHUB_REPOSITORY environment variable is required")
		return fmt.Errorf("GITHUB_REPOSITORY is required: %w", errs.ErrUsage)
	}
	if in.SHA == "" {
		annot.Errorf("GITHUB_SHA environment variable is required")
		return fmt.Errorf("GITHUB_SHA is required: %w", errs.ErrUsage)
	}
	if in.Ref == "" {
		annot.Errorf("GITHUB_REF environment variable is required")
		return fmt.Errorf("GITHUB_REF is required: %w", errs.ErrUsage)
	}

	raw, err := os.ReadFile(in.SARIFFile)
	if err != nil {
		return fmt.Errorf("read SARIF: %w", err)
	}

	shortSHA := in.SHA
	if len(shortSHA) > 7 {
		shortSHA = shortSHA[:7]
	}
	if in.Category != "" {
		fmt.Fprintf(stdout, "Uploading %s to Code Scanning [%s] (%s @ %s)\n",
			in.SARIFFile, in.Category, in.Repository, shortSHA)
	} else {
		fmt.Fprintf(stdout, "Uploading %s to Code Scanning (%s @ %s)\n",
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
		// ErrUnsupported on non-GitHub platforms is a clean skip; the
		// SARIF goes nowhere but the caller proceeds. Everything else
		// is a real upload failure that propagates.
		if errors.Is(err, errs.ErrUnsupported) {
			annot.Noticef("SARIF upload skipped — not supported on this platform")
			return nil
		}
		annot.Errorf("SARIF upload failed: %v", err)
		return fmt.Errorf("upload sarif: %w", err)
	}
	fmt.Fprintln(stdout, "✓ SARIF accepted by Code Scanning (results appear after async processing)")
	return nil
}
