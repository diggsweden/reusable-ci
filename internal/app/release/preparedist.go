// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
)

// PrepareDistInput drives `release prepare-dist`.
type PrepareDistInput struct {
	Path      string
	PruneDirs bool
}

// PrepareDistResult is the prepared dist hand-off metadata.
type PrepareDistResult struct {
	Dir    string
	Digest string
}

// PrepareDist validates and prepares an unsigned dist/ hand-off before upload:
// optionally prune top-level directories, compute the canonical release digest,
// and emit digest=<sha256> for the signer workflow.
func PrepareDist(ctx context.Context, sink ci.OutputSink, stderr io.Writer, in PrepareDistInput) (PrepareDistResult, error) {
	if err := validateSingleLineValue(in.Path, "path"); err != nil {
		return PrepareDistResult{}, err
	}

	dir, err := prepareAssembleDist(in.Path, in.PruneDirs)
	if err != nil {
		return PrepareDistResult{}, err
	}

	digest, err := DistDigest(dir)
	if err != nil {
		return PrepareDistResult{}, err
	}

	if sink != nil {
		if err := sink.Set(ctx, "digest", digest); err != nil {
			return PrepareDistResult{}, fmt.Errorf("write digest output: %w", err)
		}
	}

	_, _ = fmt.Fprintf(stderr, "%s digest: %s\n", dir, digest)

	return PrepareDistResult{Dir: dir, Digest: digest}, nil
}
