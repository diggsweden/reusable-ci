// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package version wires `reusable-ci version <subcmd>` use cases.
//
// Pure version helpers (sanitise, snapshot-version compose, latest-semver-tag
// pick) live in domain/version. This package adds the I/O orchestration
// that drives a real git binary.
package version

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// commitPushOps is the slice of git.Repo this use case needs. Defined
// here so app tests can fake it without importing the adapter; the
// CommitInput DTO lives in domain/git so it's shareable too.
type commitPushOps interface { //nolint:interfacebloat // Read guards plus explicit legacy and leased publication paths; no optional capability fallback.
	Config(ctx context.Context, key, value string) error
	CheckOriginPushDestination(ctx context.Context) error
	RemoteBranchCommit(ctx context.Context, remote, branch string, cred runcontext.Credential) (string, bool, error)
	AddPathspecs(ctx context.Context, pathspecs []string)
	HasStagedChanges(ctx context.Context) (bool, error)
	HasPathspecChanges(ctx context.Context, pathspecs []string) (bool, error)
	RevParse(ctx context.Context, ref string) (string, error)
	Commit(ctx context.Context, in git.CommitInput) error
	CommitParents(ctx context.Context, commitSHA string) ([]string, error)
	PushCommitWithLease(ctx context.Context, in git.BranchPushInput, cred runcontext.Credential) error
	Push(ctx context.Context, localRef, remoteBranch string, force bool, cred runcontext.Credential) error
}

// CommitPushInput drives `reusable-ci version commit-push`.
type CommitPushInput struct {
	Branch      string
	AuthorName  string
	AuthorEmail string
	Message     string
	FilePattern string                // space-separated git pathspecs
	ExpectedSHA string                // optional authorized branch HEAD; checked before mutation
	Token       runcontext.Credential // optional; authenticates the push when the checkout did not persist credentials
	DryRun      bool                  // preview: narrate the config/commit/push mutations instead of performing them
}

// CommitPush stages the file pattern, commits with --signoff (idempotent
// no-op when nothing changed), and pushes to origin/<Branch>. GPG
// signing is inherited from the repo-local git config written by
// GPGImport (commit.gpgsign=true).
//
// Both modes require an initially clean index so unrelated staged changes
// cannot be included in the release commit.
//
// With ExpectedSHA set, the owned checkout must have a single writer, including
// its Git configuration. The created commit must have exactly that parent and
// publication uses its captured OID and an exact-old-OID lease. Failures retain
// any local staging/config/commit changes; there is no rollback. Empty ExpectedSHA
// explicitly retains the legacy unleased HEAD push without destination/ancestry
// authorization.
//
// DryRun reads status without index refresh or staging, then narrates mutations.
//
//nolint:cyclop // validate → guard the authorized branch → stage → decide no-op → commit → push, each gated by the dry-run flag. Phases, not nested logic.
func CommitPush(ctx context.Context, repo commitPushOps, out io.Writer, in CommitPushInput) error {
	in.ExpectedSHA = strings.TrimSpace(in.ExpectedSHA)
	if err := requireFields(in); err != nil {
		return err
	}

	if staged, err := repo.HasStagedChanges(ctx); err != nil {
		return fmt.Errorf("inspect index before staging: %w", err)
	} else if staged {
		return fmt.Errorf("commit-push: index already contains staged changes; commit or unstage them before continuing: %w", errs.ErrValidation)
	}

	if err := requireExpectedBranchHead(ctx, repo, in); err != nil {
		return err
	}

	if in.DryRun {
		// The repo-local user.name/user.email writes only serve the skipped
		// commit — leave the checkout's config untouched in a preview.
		_, _ = fmt.Fprintln(out, "[dry-run] skipping git author config (commit is skipped)")
	}

	// strings.Fields gives us bash-equivalent word-splitting on whitespace.
	// Pathspecs containing literal spaces are not supported (matches bash).
	var (
		hasChanges bool
		err        error
	)
	if in.DryRun {
		hasChanges, err = repo.HasPathspecChanges(ctx, strings.Fields(in.FilePattern))
	} else {
		repo.AddPathspecs(ctx, strings.Fields(in.FilePattern))
		hasChanges, err = repo.HasStagedChanges(ctx)
	}

	if err != nil {
		return fmt.Errorf("check staged changes: %w", err)
	}

	if !hasChanges {
		_, _ = fmt.Fprintln(out, "No staged changes — skipping commit and push.")

		return nil
	}

	if in.DryRun {
		_, _ = fmt.Fprintf(out, "[dry-run] would commit staged changes as %s <%s> (signoff)\n", in.AuthorName, in.AuthorEmail)
		if in.ExpectedSHA == "" {
			_, _ = fmt.Fprintf(out, "[dry-run] would push HEAD to origin/%s\n", in.Branch)
		} else {
			_, _ = fmt.Fprintf(out, "[dry-run] would push the captured commit OID to origin/refs/heads/%s with lease %s\n", in.Branch, in.ExpectedSHA)
		}

		return nil
	}

	if configErr := repo.Config(ctx, "user.name", in.AuthorName); configErr != nil {
		return fmt.Errorf("set user.name: %w", configErr)
	}

	if configErr := repo.Config(ctx, "user.email", in.AuthorEmail); configErr != nil {
		return fmt.Errorf("set user.email: %w", configErr)
	}

	if commitErr := repo.Commit(ctx, git.CommitInput{
		Message:     in.Message,
		AuthorName:  in.AuthorName,
		AuthorEmail: in.AuthorEmail,
		Signoff:     true,
		NoHooks:     true,
	}); commitErr != nil {
		return fmt.Errorf("commit: %w", commitErr)
	}

	pushedRef, err := pushReleaseCommit(ctx, repo, in)
	if err != nil {
		return fmt.Errorf("push: %w", err)
	}

	_, _ = fmt.Fprintf(out, "%s Committed as %s <%s>\n", clicolor.Check(out), in.AuthorName, in.AuthorEmail)
	_, _ = fmt.Fprintf(out, "%s Pushed %s to origin/%s\n", clicolor.Check(out), pushedRef, in.Branch)

	return nil
}

func pushReleaseCommit(ctx context.Context, repo commitPushOps, in CommitPushInput) (string, error) {
	if in.ExpectedSHA == "" {
		return "HEAD", repo.Push(ctx, "HEAD", in.Branch, false, in.Token)
	}

	createdSHA, err := repo.RevParse(ctx, "HEAD")
	if err != nil {
		return "", fmt.Errorf("capture created commit: %w", err)
	}

	if !git.ValidCommitSHA(createdSHA) || createdSHA == in.ExpectedSHA {
		return "", fmt.Errorf("created commit must be a new canonical OID: %w", errs.ErrValidation)
	}

	parents, err := repo.CommitParents(ctx, createdSHA)
	if err != nil {
		return "", fmt.Errorf("inspect created commit parents: %w", err)
	}

	if len(parents) != 1 || parents[0] != in.ExpectedSHA {
		return "", fmt.Errorf("created commit must have only the authorized source as its direct parent: %w", errs.ErrValidation)
	}

	return createdSHA, repo.PushCommitWithLease(ctx, git.BranchPushInput{
		CommitSHA: createdSHA, Branch: in.Branch, ExpectedSHA: in.ExpectedSHA,
	}, in.Token)
}

func requireExpectedBranchHead(ctx context.Context, repo commitPushOps, in CommitPushInput) error {
	expected := strings.TrimSpace(in.ExpectedSHA)
	if expected == "" {
		return nil
	}

	if !git.ValidCommitSHA(expected) || !git.ValidRefName("refs/heads/"+in.Branch) {
		return fmt.Errorf("authorized source must be a canonical commit SHA and destination a literal branch: %w", errs.ErrUsage)
	}

	local, err := repo.RevParse(ctx, "HEAD")
	if err != nil {
		return fmt.Errorf("resolve local authorized HEAD: %w", err)
	}

	if local != expected {
		return fmt.Errorf("local HEAD differs from authorized source: %w", errs.ErrValidation)
	}

	if destinationErr := repo.CheckOriginPushDestination(ctx); destinationErr != nil {
		return fmt.Errorf("inspect origin publication destination: %w", destinationErr)
	}

	actual, exists, err := repo.RemoteBranchCommit(ctx, "origin", in.Branch, in.Token)
	if err != nil {
		return fmt.Errorf("resolve origin/%s before release commit: %w", in.Branch, err)
	}

	if !exists {
		return fmt.Errorf("origin/%s does not exist; expected authorized source %s: %w", in.Branch, expected, errs.ErrValidation)
	}

	if actual != expected {
		return fmt.Errorf("origin/%s moved from authorized source %s to %s; refusing release commit: %w", in.Branch, expected, actual, errs.ErrValidation)
	}

	return nil
}

func requireFields(in CommitPushInput) error {
	switch {
	case in.Branch == "":
		return fmt.Errorf("commit-push: BRANCH is required: %w", errs.ErrUsage)
	case in.AuthorName == "":
		return fmt.Errorf("commit-push: COMMIT_AUTHOR_NAME is required: %w", errs.ErrUsage)
	case in.AuthorEmail == "":
		return fmt.Errorf("commit-push: COMMIT_AUTHOR_EMAIL is required: %w", errs.ErrUsage)
	case in.Message == "":
		return fmt.Errorf("commit-push: COMMIT_MESSAGE is required: %w", errs.ErrUsage)
	case in.FilePattern == "":
		return fmt.Errorf("commit-push: FILE_PATTERN is required: %w", errs.ErrUsage)
	}

	return nil
}
