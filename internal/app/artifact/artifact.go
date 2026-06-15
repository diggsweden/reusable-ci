// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package artifact orchestrates run-artifact upload/download: it validates
// the request, delegates the transport to the forge's provider role, and
// emits the result (machine values to the output sink, a human summary to
// stderr). The transport — and its security hardening — lives in the
// adapter behind the role; this layer is forge-agnostic.
package artifact

import (
	"context"
	"fmt"
	"io"

	domainartifact "github.com/diggsweden/reusable-ci/internal/domain/artifact"
	domainci "github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// Download validates the request, delegates to the provider's
// RunArtifactDownloader, and records the result. The resolved name and
// totals are written to the sink; a one-line summary goes to w (stderr).
func Download(ctx context.Context, dl provider.RunArtifactDownloader, sink domainci.OutputSink, w io.Writer, in provider.RunArtifactDownload) (provider.RunArtifactInfo, error) { //nolint:varnamelen // idiomatic short names (testing/http/io conventions).
	// Pattern selects a set of artifacts by glob; a single named artifact
	// is the exact-match case. Exactly one of the two must be given.
	switch {
	case in.Pattern != "" && in.Name != "":
		return provider.RunArtifactInfo{}, fmt.Errorf("use --name or --pattern, not both: %w", errs.ErrUsage)
	case in.Pattern == "" && in.Name == "":
		// Neither selector supplied — a usage error (exit 2), symmetric with
		// Upload's missing-selector. A *given* but malformed name is a
		// separate, data-level failure handled by ValidateName below.
		return provider.RunArtifactInfo{}, fmt.Errorf("download needs --name or --pattern: %w", errs.ErrUsage)
	case in.Pattern == "":
		if err := domainartifact.ValidateName(in.Name); err != nil {
			return provider.RunArtifactInfo{}, err
		}
	}

	if in.Dir == "" {
		return provider.RunArtifactInfo{}, fmt.Errorf("destination dir is required: %w", errs.ErrUsage)
	}

	info, err := dl.DownloadRunArtifact(ctx, in)
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	emit(ctx, sink, w, info, "Downloaded", in.Dir)

	return info, nil
}

// Upload validates the request, delegates to the provider's
// RunArtifactUploader, and records the result.
func Upload(ctx context.Context, up provider.RunArtifactUploader, sink domainci.OutputSink, w io.Writer, in provider.RunArtifactUpload) (provider.RunArtifactInfo, error) { //nolint:varnamelen // idiomatic short names (testing/http/io conventions).
	if err := domainartifact.ValidateName(in.Name); err != nil {
		return provider.RunArtifactInfo{}, err
	}

	if in.Dir == "" && len(in.Files) == 0 && len(in.Paths) == 0 {
		return provider.RunArtifactInfo{}, fmt.Errorf("upload needs --dir, --path, or at least one --file: %w", errs.ErrUsage)
	}

	if in.IfNoFiles == "" {
		in.IfNoFiles = provider.IfNoFilesError
	}

	info, err := up.UploadRunArtifact(ctx, in)
	if err != nil {
		return provider.RunArtifactInfo{}, err
	}

	emit(ctx, sink, w, info, "Uploaded", "")

	return info, nil
}

// emit writes the machine-readable result to the sink and a single human
// summary line to w. Sink keys are stable for downstream steps.
func emit(ctx context.Context, sink domainci.OutputSink, out io.Writer, info provider.RunArtifactInfo, verb, dir string) {
	if sink != nil {
		_ = sink.Set(ctx, "artifact-name", info.Name)
		if info.ID != "" {
			_ = sink.Set(ctx, "artifact-id", info.ID)
		}
	}

	if out == nil {
		return
	}

	if dir != "" {
		_, _ = fmt.Fprintf(out, "%s %s (%d files, %d bytes) → %s\n", verb, info.Name, info.FileCount, info.Bytes, dir)

		return
	}

	_, _ = fmt.Fprintf(out, "%s %s (%d files, %d bytes)\n", verb, info.Name, info.FileCount, info.Bytes)
}
