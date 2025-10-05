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
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	domainversion "github.com/diggsweden/reusable-ci/v3/internal/domain/version"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// defaultRemoteName is the git remote used when the caller does not name one.
const defaultRemoteName = "origin"

// tagReleaseOps is the slice of adapter/git.Repo this use case needs.
type tagReleaseOps interface {
	CheckOriginPushDestination(ctx context.Context) error
	TagExists(ctx context.Context, tag string) (bool, error)
	TagSHA(ctx context.Context, tag string) (string, error)
	RemoteTagCommitIfExists(ctx context.Context, remote, tag string, cred runcontext.Credential) (string, bool, error)
	RemoteTagObjectIfExists(ctx context.Context, remote, tag string, cred runcontext.Credential) (string, bool, error)
	CatFileType(ctx context.Context, ref string) (string, error)
	VerifyConfiguredTagSignature(ctx context.Context, tag string) error
	CreateTag(ctx context.Context, tag, ref string, signed bool) error
	PushTagNoForce(ctx context.Context, tag string, cred runcontext.Credential) error
	RevParse(ctx context.Context, ref string) (string, error)
}

// TagReleaseInput configures TagRelease. Signed defaults to true in
// production; tests set Signed=false to avoid the GPG dependency.
type TagReleaseInput struct {
	Tag    string // final release tag, e.g. "v3.5.7"
	Signed bool
	Remote string                // empty defaults to origin; other remotes are unsupported
	Token  runcontext.Credential // optional; authenticates the tag push when the checkout did not persist credentials
	DryRun bool                  // preview: narrate the tag create/push instead of performing them; validation still runs
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
// An exact-commit rerun is idempotent. Existing local or remote tags are
// accepted only when they resolve to HEAD; any mismatch is rejected and no tag
// is moved or force-pushed. If creation succeeded locally but the first push
// failed, a rerun pushes that existing exact tag without recreating it.
// The owned checkout (including config and tag refs) must have a single writer.
// Creation uses the captured release OID, not a second resolution of live HEAD.
//
// DryRun runs every validation (input shape, local/remote tag-exists) and
// then narrates the create/push instead of performing them. The release-sha
// output is still emitted — it is HEAD, the commit the tag would point at.
//
//nolint:cyclop // Sequential destination, tag, publication and output boundaries.
func TagRelease(ctx context.Context, repo tagReleaseOps, in TagReleaseInput, sink ci.OutputSink, w io.Writer) (*TagReleaseOutput, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err := validateTagReleaseInput(in); err != nil {
		return nil, err
	}

	remote := in.Remote
	if remote == "" {
		remote = defaultRemoteName
	}

	if err := repo.CheckOriginPushDestination(ctx); err != nil {
		return nil, fmt.Errorf("tag-release: inspect origin publication destination: %w", err)
	}

	releaseSHA, localExists, remoteExists, err := inspectReleaseTag(ctx, repo, remote, in)
	if err != nil {
		return nil, err
	}

	if !remoteExists {
		if err := createAndPushReleaseTag(ctx, repo, in, remote, releaseSHA, !localExists, w); err != nil {
			return nil, err
		}
	}

	if sink != nil {
		if err := sink.Set(ctx, "release-sha", releaseSHA); err != nil {
			return nil, fmt.Errorf("emit release-sha: %w", err)
		}
	}

	if remoteExists {
		_, _ = fmt.Fprintf(w, "Release tag %s already exists at %s; rerun is a no-op\n", in.Tag, releaseSHA)
	} else if !in.DryRun {
		_, _ = fmt.Fprintf(w, "Release tag %s created at %s\n", in.Tag, releaseSHA)
	}

	return &TagReleaseOutput{Tag: in.Tag, ReleaseSHA: releaseSHA}, nil
}

// createAndPushReleaseTag performs the two git mutations (tag create, tag
// push) — or, in dry-run, narrates and skips them — and returns the SHA the
// tag points (or would point) at. The branch sits here, right at the adapter
// calls, so the validation/planning path above it is never duplicated.
func createAndPushReleaseTag(ctx context.Context, repo tagReleaseOps, in TagReleaseInput, remote, releaseSHA string, create bool, w io.Writer) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.DryRun {
		if create {
			_, _ = fmt.Fprintf(w, "[dry-run] would create %s %s at HEAD (%s)\n", tagKind(in.Signed), in.Tag, releaseSHA)
		}

		_, _ = fmt.Fprintf(w, "[dry-run] would push tag %s to %s (no force)\n", in.Tag, remote)

		return nil
	}

	if create {
		if createErr := repo.CreateTag(ctx, in.Tag, releaseSHA, in.Signed); createErr != nil {
			return fmt.Errorf("tag-release: create tag: %w", createErr)
		}
	}

	if pushErr := repo.PushTagNoForce(ctx, in.Tag, in.Token); pushErr != nil {
		return fmt.Errorf("tag-release: push tag: %w", pushErr)
	}

	return nil
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
	if in.Remote != "" && in.Remote != defaultRemoteName {
		return fmt.Errorf("tag-release: only origin is supported as the publication remote: %w", errs.ErrUsage)
	}

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

//nolint:cyclop,nestif // inspect local and remote create-once invariants before mutation; not an atomic transaction.
func inspectReleaseTag(ctx context.Context, repo tagReleaseOps, remote string, in TagReleaseInput) (string, bool, bool, error) {
	headSHA, err := repo.RevParse(ctx, "HEAD")
	if err != nil {
		return "", false, false, fmt.Errorf("tag-release: rev-parse HEAD: %w", err)
	}

	if !domaingit.ValidCommitSHA(headSHA) {
		return "", false, false, fmt.Errorf("tag-release: HEAD must resolve to a canonical commit OID: %w", errs.ErrValidation)
	}

	localExists, err := repo.TagExists(ctx, in.Tag)
	if err != nil {
		return "", false, false, fmt.Errorf("tag-release: check local tag %s: %w", in.Tag, err)
	}

	if localExists {
		localSHA, resolveErr := repo.TagSHA(ctx, in.Tag)
		if resolveErr != nil {
			return "", false, false, fmt.Errorf("tag-release: resolve local tag %s: %w", in.Tag, resolveErr)
		}

		if localSHA != headSHA {
			return "", false, false, fmt.Errorf(
				"tag-release: local tag %s resolves to %s, not HEAD %s; refusing to move it: %w",
				in.Tag, localSHA, headSHA, errs.ErrValidation)
		}

		if tagErr := validateExistingTag(ctx, repo, in.Tag, in.Signed); tagErr != nil {
			return "", false, false, tagErr
		}
	}

	remoteSHA, remoteExists, err := repo.RemoteTagCommitIfExists(ctx, remote, in.Tag, in.Token)
	if err != nil {
		return "", false, false, fmt.Errorf("tag-release: check release tag %s on %s: %w", in.Tag, remote, err)
	}

	if remoteExists && remoteSHA != headSHA {
		return "", false, false, fmt.Errorf(
			"tag-release: remote tag %s on %s resolves to %s, not HEAD %s; refusing to move it: %w",
			in.Tag, remote, remoteSHA, headSHA, errs.ErrValidation)
	}

	if remoteExists {
		if !localExists {
			return "", false, false, fmt.Errorf(
				"tag-release: remote tag %s exists but the exact local tag object is unavailable for signature verification: %w",
				in.Tag, errs.ErrValidation)
		}

		localObject, objectErr := repo.RevParse(ctx, "refs/tags/"+in.Tag)
		if objectErr != nil {
			return "", false, false, fmt.Errorf("tag-release: resolve local tag object %s: %w", in.Tag, objectErr)
		}

		remoteObject, objectExists, objectErr := repo.RemoteTagObjectIfExists(ctx, remote, in.Tag, in.Token)
		if objectErr != nil {
			return "", false, false, fmt.Errorf("tag-release: resolve remote tag object %s: %w", in.Tag, objectErr)
		}

		if !objectExists || remoteObject != localObject {
			return "", false, false, fmt.Errorf(
				"tag-release: remote tag object %s does not match the verified local object %s: %w",
				remoteObject, localObject, errs.ErrValidation)
		}
	}

	return headSHA, localExists, remoteExists, nil
}

func validateExistingTag(ctx context.Context, repo tagReleaseOps, tag string, signed bool) error {
	objType, err := repo.CatFileType(ctx, "refs/tags/"+tag)
	if err != nil {
		return fmt.Errorf("tag-release: inspect existing tag %s: %w", tag, err)
	}

	if objType != "tag" {
		return fmt.Errorf("tag-release: existing tag %s is lightweight; exact reruns require an annotated tag: %w", tag, errs.ErrValidation)
	}

	if !signed {
		return nil
	}

	if err := repo.VerifyConfiguredTagSignature(ctx, tag); err != nil {
		return fmt.Errorf("tag-release: existing tag %s does not have a valid configured signature: %w", tag, errs.ErrPermissionDenied)
	}

	return nil
}
