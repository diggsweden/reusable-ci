// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/internal/domain/git"
	"github.com/diggsweden/reusable-ci/internal/safeexec"
)

// Config writes a single git config entry in the working-tree-local
// scope (.git/config). Equivalent to `git config <key> <value>`.
func (r *Repo) Config(ctx context.Context, key, value string) error {
	_, err := r.Run(ctx, "config", key, value)

	return err
}

// AddPathspecs runs `git add -- <pathspec> ...`. A pathspec that
// matches no files causes git to exit non-zero; this is *expected*
// during bump flows where the bump may legitimately produce nothing to
// stage. The error is swallowed — callers use HasStagedChanges() to
// determine if anything was actually added.
func (r *Repo) AddPathspecs(ctx context.Context, pathspecs []string) {
	args := append([]string{"add", "--"}, pathspecs...)
	_, _ = r.Run(ctx, args...) // intentional swallow
}

// HasStagedChanges reports whether the index differs from HEAD.
//
// We don't use Run() here because exit code 1 is the success signal
// (means there *are* staged changes), not an error condition.
func (r *Repo) HasStagedChanges(ctx context.Context) (bool, error) {
	bin := r.GitBin
	if bin == "" {
		bin = "git" //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}

	cmd := safeexec.Command(ctx, bin, "diff", "--cached", "--quiet")
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}

	err := cmd.Run()
	if err == nil {
		return false, nil // exit 0: no diff
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() == 1 {
			return true, nil // exit 1: there is a diff
		}

		return false, fmt.Errorf("git diff --cached --quiet: exit %d\n%s: %w", exitErr.ExitCode(), exitErr.Stderr, errs.ErrDependencyUnavailable)
	}

	return false, err
}

// Commit runs `git commit` with the given input. Author is passed via
// --author "Name <email>"; signoff appends the trailer when set.
func (r *Repo) Commit(ctx context.Context, in domaingit.CommitInput) error {
	args := []string{"commit"}
	if in.Signoff {
		args = append(args, "--signoff")
	}

	if in.AuthorName != "" || in.AuthorEmail != "" {
		args = append(args, "--author",
			fmt.Sprintf("%s <%s>", in.AuthorName, in.AuthorEmail))
	}

	args = append(args, "-m", in.Message)
	_, err := r.Run(ctx, args...)

	return err
}

// Push runs `git push origin <localRef>:<remoteBranch>`. Pass
// force=true for tag re-pushes (move-tag).
func (r *Repo) Push(ctx context.Context, localRef, remoteBranch string, force bool) error {
	args := []string{"push"}
	if force {
		args = append(args, "--force")
	}

	args = append(args, "origin", fmt.Sprintf("%s:%s", localRef, remoteBranch))
	_, err := r.Run(ctx, args...)

	return err
}

// PushTag is a convenience wrapper for `git push --force origin <tag>`.
func (r *Repo) PushTag(ctx context.Context, tag string) error {
	_, err := r.Run(ctx, "push", "--force", "origin", tag)

	return err
}

// MoveTag deletes-and-recreates an annotated tag at HEAD. Equivalent
// to `git tag -f [-s] <tag> -m <tag>`. signed=true adds -s (GPG
// signing) — production behaviour. Tests pass signed=false to skip
// the GPG dependency in their isolated repos.
func (r *Repo) MoveTag(ctx context.Context, tag string, signed bool) error {
	args := []string{"tag", "-f"} //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	if signed {
		args = append(args, "-s")
	}

	args = append(args, tag, "-m", tag)
	_, err := r.Run(ctx, args...)

	return err
}
