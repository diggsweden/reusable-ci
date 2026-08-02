// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainversion "github.com/diggsweden/reusable-ci/v3/internal/domain/version"
)

// defaultRemoteName is the git remote used when the caller does not name one.
const defaultRemoteName = "origin"

// tagReleaseOps is the slice of adapter/git.Repo this use case needs.
type tagReleaseOps interface {
	TagExists(ctx context.Context, tag string) (bool, error)
	RemoteTagExists(ctx context.Context, remote, tag string) (bool, error)
	CreateTag(ctx context.Context, tag, ref string, signed bool) error
	PushTagNoForce(ctx context.Context, tag, token string) error
	RevParse(ctx context.Context, ref string) (string, error)
}

// TagReleaseInput configures TagRelease. Signed defaults to true in
// production; tests set Signed=false to avoid the GPG dependency.
type TagReleaseInput struct {
	Tag    string // final release tag, e.g. "v3.5.7"
	Signed bool
	Remote string // empty defaults to origin
	Token  string // optional; authenticates the tag push when the checkout did not persist credentials
	DryRun bool   // preview: narrate the tag create/push instead of performing them; validation still runs
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
//
// DryRun runs every validation (input shape, local/remote tag-exists) and
// then narrates the create/push instead of performing them. The release-sha
// output is still emitted — it is HEAD, the commit the tag would point at.
func TagRelease(ctx context.Context, repo tagReleaseOps, in TagReleaseInput, sink ci.OutputSink, w io.Writer) (*TagReleaseOutput, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err := validateTagReleaseInput(in); err != nil {
		return nil, err
	}

	remote := in.Remote
	if remote == "" {
		remote = defaultRemoteName
	}

	if err := ensureReleaseTagAbsent(ctx, repo, remote, in.Tag); err != nil {
		return nil, err
	}

	releaseSHA, err := createAndPushReleaseTag(ctx, repo, in, remote, w)
	if err != nil {
		return nil, err
	}

	if sink != nil {
		if err := sink.Set(ctx, "release-sha", releaseSHA); err != nil {
			return nil, fmt.Errorf("emit release-sha: %w", err)
		}
	}

	if !in.DryRun {
		_, _ = fmt.Fprintf(w, "Release tag %s created at %s\n", in.Tag, releaseSHA)
	}

	return &TagReleaseOutput{Tag: in.Tag, ReleaseSHA: releaseSHA}, nil
}

// createAndPushReleaseTag performs the two git mutations (tag create, tag
// push) — or, in dry-run, narrates and skips them — and returns the SHA the
// tag points (or would point) at. The branch sits here, right at the adapter
// calls, so the validation/planning path above it is never duplicated.
func createAndPushReleaseTag(ctx context.Context, repo tagReleaseOps, in TagReleaseInput, remote string, w io.Writer) (string, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.DryRun {
		releaseSHA, err := repo.RevParse(ctx, "HEAD")
		if err != nil {
			return "", fmt.Errorf("tag-release: rev-parse HEAD: %w", err)
		}

		_, _ = fmt.Fprintf(w, "[dry-run] would create %s %s at HEAD (%s)\n", tagKind(in.Signed), in.Tag, releaseSHA)
		_, _ = fmt.Fprintf(w, "[dry-run] would push tag %s to %s (no force)\n", in.Tag, remote)

		return releaseSHA, nil
	}

	if createErr := repo.CreateTag(ctx, in.Tag, "HEAD", in.Signed); createErr != nil {
		return "", fmt.Errorf("tag-release: create tag: %w", createErr)
	}

	if pushErr := repo.PushTagNoForce(ctx, in.Tag, in.Token); pushErr != nil {
		return "", fmt.Errorf("tag-release: push tag: %w", pushErr)
	}

	releaseSHA, err := repo.RevParse(ctx, "HEAD")
	if err != nil {
		return "", fmt.Errorf("tag-release: rev-parse HEAD: %w", err)
	}

	return releaseSHA, nil
}

// tagKind names the tag flavour for the dry-run narration.
func tagKind(signed bool) string {
	if signed {
		return "signed tag"
	}

	return "annotated tag"
}

// validateTagReleaseInput rejects missing, multi-line, or non-stable-semver tags.
func validateTagReleaseInput(in TagReleaseInput) error {
	if in.Tag == "" {
		return fmt.Errorf("tag-release: tag is required: %w", errs.ErrUsage)
	}

	if strings.ContainsAny(in.Tag, "\n\r") {
		return fmt.Errorf("tag-release: release tag must be a single-line value: %w", errs.ErrValidation)
	}

	if !domainversion.IsStableSemverTag(in.Tag) {
		return fmt.Errorf("tag-release: release tag must look like stable vMAJOR.MINOR.PATCH: %s: %w", in.Tag, errs.ErrValidation)
	}

	return nil
}

// ensureReleaseTagAbsent refuses to proceed when the release tag already
// exists locally or on the remote — release tags are created exactly once.
func ensureReleaseTagAbsent(ctx context.Context, repo tagReleaseOps, remote, tag string) error {
	exists, err := repo.TagExists(ctx, tag)
	if err != nil {
		return fmt.Errorf("tag-release: check local tag %s: %w", tag, err)
	}

	if exists {
		return fmt.Errorf(
			"tag-release: release tag %s already exists locally; refusing to move it: %w",
			tag, errs.ErrValidation)
	}

	remoteExists, err := repo.RemoteTagExists(ctx, remote, tag)
	if err != nil {
		return fmt.Errorf("tag-release: check release tag %s on %s: %w", tag, remote, err)
	}

	if remoteExists {
		return fmt.Errorf("tag-release: release tag %s already exists on %s; refusing to move it: %w", tag, remote, errs.ErrValidation)
	}

	return nil
}
