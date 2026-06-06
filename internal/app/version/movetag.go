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

// moveTagOps is the slice of adapter/git.Repo this use case needs.
// Defining a small port keeps app-layer tests fake-able without
// importing the adapter or invoking a real git binary.
type moveTagOps interface {
	DescribeLatestTag(ctx context.Context) (string, error)
	RevParse(ctx context.Context, ref string) (string, error)
	TagSHA(ctx context.Context, tag string) (string, error)
	MoveTag(ctx context.Context, tag string, signed bool) error
	PushTag(ctx context.Context, tag string) error
}

// MoveTagOutput is what MoveTag returns to its caller — emitted as
// release-sha=<hash> on the OutputSink.
type MoveTagOutput struct {
	Tag        string
	ReleaseSHA string
}

// MoveTagInput configures MoveTag. Signed defaults to true in production
// (the bash always passes -s); tests set Signed=false to avoid the GPG
// dependency in their isolated repos.
type MoveTagInput struct {
	Signed bool
}

// MoveTag verifies the latest tag points at HEAD~1, then re-creates it
// (optionally signed) at HEAD and force-pushes. Errors with a clear
// message when the tag is at any other commit (operator's release flow
// has diverged).
//
// The release-sha output is written through sink so both GHA (heredoc)
// and GitLab (dotenv) work.
func MoveTag(ctx context.Context, repo moveTagOps, in MoveTagInput, sink ci.OutputSink, w io.Writer) (*MoveTagOutput, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	tag, err := repo.DescribeLatestTag(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe latest tag: %w", err)
	}

	prevSHA, err := repo.RevParse(ctx, "HEAD~1")
	if err != nil {
		return nil, fmt.Errorf("rev-parse HEAD~1: %w", err)
	}

	tagSHA, err := repo.TagSHA(ctx, tag)
	if err != nil {
		return nil, fmt.Errorf("rev-list -n 1 %s: %w", tag, err)
	}

	if tagSHA != prevSHA {
		return nil, fmt.Errorf(
			"tag %s points to unexpected commit\n  expected: %s (HEAD~1)\n  found:    %s: %w",
			tag, prevSHA, tagSHA, errs.ErrValidation)
	}

	_, _ = fmt.Fprintf(w, "Moving tag %s from previous commit to current\n", tag)

	if moveErr := repo.MoveTag(ctx, tag, in.Signed); moveErr != nil {
		return nil, fmt.Errorf("move tag: %w", moveErr)
	}

	if pushErr := repo.PushTag(ctx, tag); pushErr != nil {
		return nil, fmt.Errorf("push tag: %w", pushErr)
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

	return &MoveTagOutput{Tag: tag, ReleaseSHA: releaseSHA}, nil
}
