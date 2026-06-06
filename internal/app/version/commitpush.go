// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package version wires `reusable-ci version <subcmd>` use cases.
//
// Pure version helpers (sanitise, dev-version compose, latest-semver-tag
// pick) live in domain/version. This package adds the I/O orchestration
// that drives a real git binary.
package version

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/git"
)

// commitPushOps is the slice of git.Repo this use case needs. Defined
// here so app tests can fake it without importing the adapter; the
// CommitInput DTO lives in domain/git so it's shareable too.
type commitPushOps interface {
	Config(ctx context.Context, key, value string) error
	AddPathspecs(ctx context.Context, pathspecs []string)
	HasStagedChanges(ctx context.Context) (bool, error)
	Commit(ctx context.Context, in git.CommitInput) error
	Push(ctx context.Context, localRef, remoteBranch string, force bool) error
}

// CommitPushInput drives `reusable-ci version commit-push`.
type CommitPushInput struct {
	Branch      string
	AuthorName  string
	AuthorEmail string
	Message     string
	FilePattern string // space-separated git pathspecs
}

// CommitPush stages the file pattern, commits with --signoff (idempotent
// no-op when nothing changed), and pushes to origin/<Branch>. GPG
// signing is inherited from the repo-local git config written by
// GPGImport (commit.gpgsign=true).
func CommitPush(ctx context.Context, repo commitPushOps, out io.Writer, in CommitPushInput) error {
	if err := requireFields(in); err != nil {
		return err
	}

	if err := repo.Config(ctx, "user.name", in.AuthorName); err != nil {
		return fmt.Errorf("set user.name: %w", err)
	}

	if err := repo.Config(ctx, "user.email", in.AuthorEmail); err != nil {
		return fmt.Errorf("set user.email: %w", err)
	}

	// strings.Fields gives us bash-equivalent word-splitting on whitespace.
	// Pathspecs containing literal spaces are not supported (matches bash).
	repo.AddPathspecs(ctx, strings.Fields(in.FilePattern))

	hasChanges, err := repo.HasStagedChanges(ctx)
	if err != nil {
		return fmt.Errorf("check staged changes: %w", err)
	}

	if !hasChanges {
		_, _ = fmt.Fprintln(out, "No staged changes — skipping commit and push.")

		return nil
	}

	if err := repo.Commit(ctx, git.CommitInput{
		Message:     in.Message,
		AuthorName:  in.AuthorName,
		AuthorEmail: in.AuthorEmail,
		Signoff:     true,
	}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	_, _ = fmt.Fprintf(out, "✓ Committed as %s <%s>\n", in.AuthorName, in.AuthorEmail)

	if err := repo.Push(ctx, "HEAD", in.Branch, false); err != nil {
		return fmt.Errorf("push: %w", err)
	}

	_, _ = fmt.Fprintf(out, "✓ Pushed HEAD to origin/%s\n", in.Branch)

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
