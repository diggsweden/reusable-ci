// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/version"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// tagVerifyGit is the git surface VerifyReleaseTag needs. RevParse reads
// the local checkout; the Remote* calls re-query the published remote at
// the trust boundary.
type tagVerifyGit interface {
	RevParse(ctx context.Context, ref string) (string, error)
	RemoteTagCommit(ctx context.Context, repoURL, tag string, cred runcontext.Credential) (string, error)
	RemoteVersionTags(ctx context.Context, repoURL string, cred runcontext.Credential) ([]string, error)
}

// VerifyReleaseTagInput drives VerifyReleaseTag.
type VerifyReleaseTagInput struct {
	ReleaseSHA string // the commit the release is built from
	Tag        string // the release tag, e.g. v1.2.3
	RepoURL    string // remote URL for ls-remote
	Token      runcontext.Credential
}

// VerifyReleaseTag re-verifies, at the signing trust boundary, that:
//
//  1. the checkout (HEAD) is the release commit,
//  2. the published tag points to that same commit on the remote, and
//  3. the tag has not been superseded by a newer vX.Y.Z tag.
//
// Port of forgejo-ci's verify-release-tag.sh. Any mismatch is a
// validation error — the signer must refuse to publish a stale or
// hijacked release.
//
//nolint:cyclop // three trust-boundary invariants, one branch each — flatter is clearer than splitting.
func VerifyReleaseTag(ctx context.Context, git tagVerifyGit, out io.Writer, in VerifyReleaseTagInput) error {
	if in.ReleaseSHA == "" || in.Tag == "" || in.RepoURL == "" {
		return fmt.Errorf("verify-release-tag: release-sha, tag, and repo-url are required: %w", errs.ErrUsage)
	}

	if !version.IsStableSemverTag(in.Tag) {
		return fmt.Errorf("verify-release-tag: release tag must be stable vMAJOR.MINOR.PATCH: %s: %w", in.Tag, errs.ErrValidation)
	}

	head, err := git.RevParse(ctx, "HEAD")
	if err != nil {
		return fmt.Errorf("verify-release-tag: resolve HEAD: %w", err)
	}

	if head != in.ReleaseSHA {
		return fmt.Errorf("verify-release-tag: checkout %s does not match release-sha %s: %w", head, in.ReleaseSHA, errs.ErrValidation)
	}

	tagCommit, err := git.RemoteTagCommit(ctx, in.RepoURL, in.Tag, in.Token)
	if err != nil {
		return fmt.Errorf("verify-release-tag: resolve remote tag: %w", err)
	}

	if tagCommit != in.ReleaseSHA {
		return fmt.Errorf("verify-release-tag: remote tag %s points to %s, not release-sha %s: %w", in.Tag, tagCommit, in.ReleaseSHA, errs.ErrValidation)
	}

	tags, err := git.RemoteVersionTags(ctx, in.RepoURL, in.Token)
	if err != nil {
		return fmt.Errorf("verify-release-tag: list remote tags: %w", err)
	}

	if latest := version.LatestSemverTag(tags); latest != "" && latest != in.Tag {
		return fmt.Errorf("verify-release-tag: tag %s has been superseded by %s: %w", in.Tag, latest, errs.ErrValidation)
	}

	_, _ = fmt.Fprintf(out, "release tag %s verified at %s (latest, points to release-sha)\n", in.Tag, in.ReleaseSHA)

	return nil
}
