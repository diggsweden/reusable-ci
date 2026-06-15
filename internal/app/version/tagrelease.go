// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"context"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// tagReleaseOps is the slice of adapter/git.Repo this use case needs.
type tagReleaseOps interface {
	TagExists(ctx context.Context, tag string) (bool, error)
	CreateTag(ctx context.Context, tag, ref string, signed bool) error
	PushTagNoForce(ctx context.Context, tag string) error
	RevParse(ctx context.Context, ref string) (string, error)
}

// TagReleaseInput configures TagRelease. Signed defaults to true in
// production; tests set Signed=false to avoid the GPG dependency.
type TagReleaseInput struct {
	Tag    string // final release tag, e.g. "v3.5.7"
	Signed bool
}

// TagReleaseOutput is emitted as release-sha=<hash> on the OutputSink.
type TagReleaseOutput struct {
	Tag        string
	ReleaseSHA string
}

// TagRelease creates the final release tag ONCE at HEAD (the bump commit)
// and pushes it without --force. The human pushes a separate signed
// `release-request/<tag>` ref; this promotes it to the final `<tag>`,
// created once — no tag is ever deleted, moved, or force-pushed.
//
// It refuses if the final tag already exists — release tags are immutable
// and created exactly once; this never moves or clobbers a tag, and the
// push is non-force so the remote rejects any clobber too.
func TagRelease(ctx context.Context, repo tagReleaseOps, in TagReleaseInput, sink ci.OutputSink, w io.Writer) (*TagReleaseOutput, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.Tag == "" {
		return nil, fmt.Errorf("tag-release: tag is required: %w", errs.ErrUsage)
	}

	exists, err := repo.TagExists(ctx, in.Tag)
	if err != nil {
		return nil, fmt.Errorf("check tag %s: %w", in.Tag, err)
	}

	if exists {
		return nil, fmt.Errorf(
			"release tag %s already exists — release tags are immutable and created once; refusing to move it: %w",
			in.Tag, errs.ErrValidation)
	}

	_, _ = fmt.Fprintf(w, "Creating release tag %s at HEAD (bump commit)\n", in.Tag)

	if err := repo.CreateTag(ctx, in.Tag, "HEAD", in.Signed); err != nil {
		return nil, fmt.Errorf("create tag: %w", err)
	}

	if err := repo.PushTagNoForce(ctx, in.Tag); err != nil {
		return nil, fmt.Errorf("push tag: %w", err)
	}

	releaseSHA, err := repo.RevParse(ctx, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("rev-parse HEAD: %w", err)
	}

	if sink != nil {
		if err := sink.Set(ctx, "release-sha", releaseSHA); err != nil {
			return nil, fmt.Errorf("emit release-sha: %w", err)
		}
	}

	return &TagReleaseOutput{Tag: in.Tag, ReleaseSHA: releaseSHA}, nil
}
