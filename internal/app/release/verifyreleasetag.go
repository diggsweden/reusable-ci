// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/version"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// tagVerifyGit is the git surface VerifyReleaseTag needs. RevParse reads
// the local checkout; the Remote* calls re-query the published remote at
// the trust boundary.
type tagVerifyGit interface {
	RevParse(ctx context.Context, ref string) (string, error)
	CatFileType(ctx context.Context, ref string) (string, error)
	RemoteTagObjectAtURL(ctx context.Context, repoURL, tag string, cred runcontext.Credential) (string, error)
	RemoteTagCommit(ctx context.Context, repoURL, tag string, cred runcontext.Credential) (string, error)
	RemoteVersionTags(ctx context.Context, repoURL string, cred runcontext.Credential) ([]string, error)
}

// VerifyReleaseTagInput drives VerifyReleaseTag.
type VerifyReleaseTagInput struct {
	ReleaseSHA string // the commit the release is built from
	Tag        string // the release tag, e.g. v1.2.3
	RepoURL    string // original repository argument for ls-remote; empty defaults to origin
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
	if in.ReleaseSHA == "" || in.Tag == "" {
		return fmt.Errorf("verify-release-tag: release-sha and tag are required: %w", errs.ErrUsage)
	}

	if !domaingit.ValidCommitSHA(in.ReleaseSHA) {
		return fmt.Errorf("verify-release-tag: release-sha must be a full lowercase commit ID: %w", errs.ErrValidation)
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

	objType, err := git.CatFileType(ctx, "refs/tags/"+in.Tag)
	if err != nil {
		return fmt.Errorf("verify-release-tag: inspect local tag object: %w", err)
	}

	if objType != "tag" {
		return fmt.Errorf("verify-release-tag: final tag %s is not an annotated tag object: %w", in.Tag, errs.ErrValidation)
	}

	repoURL := in.RepoURL
	if repoURL == "" {
		// Keep the original argument. Replaying an effective RemoteURL result
		// through Git could rewrite it again and inspect a different repository.
		repoURL = "origin"
	}

	localObject, err := git.RevParse(ctx, "refs/tags/"+in.Tag)
	if err != nil {
		return fmt.Errorf("verify-release-tag: resolve local tag object: %w", err)
	}

	remoteObject, err := git.RemoteTagObjectAtURL(ctx, repoURL, in.Tag, in.Token)
	if err != nil {
		return fmt.Errorf("verify-release-tag: resolve remote tag object: %w", err)
	}

	if !domaingit.ValidCommitSHA(localObject) || !domaingit.ValidCommitSHA(remoteObject) || remoteObject != localObject {
		return fmt.Errorf("verify-release-tag: local tag object %s does not match remote object %s: %w", localObject, remoteObject, errs.ErrValidation)
	}

	tagCommit, err := git.RemoteTagCommit(ctx, repoURL, in.Tag, in.Token)
	if err != nil {
		return fmt.Errorf("verify-release-tag: resolve remote tag: %w", err)
	}

	if tagCommit != in.ReleaseSHA {
		return fmt.Errorf("verify-release-tag: remote tag %s points to %s, not release-sha %s: %w", in.Tag, tagCommit, in.ReleaseSHA, errs.ErrValidation)
	}

	tags, err := git.RemoteVersionTags(ctx, repoURL, in.Token)
	if err != nil {
		return fmt.Errorf("verify-release-tag: list remote tags: %w", err)
	}

	if latest := version.LatestSemverTag(tags); latest != "" && latest != in.Tag {
		return fmt.Errorf("verify-release-tag: tag %s has been superseded by %s: %w", in.Tag, latest, errs.ErrValidation)
	}

	_, _ = fmt.Fprintf(out, "release tag %s verified at %s (latest, points to release-sha)\n", in.Tag, in.ReleaseSHA)

	return nil
}
