// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

// Config writes a single git config entry in the working-tree-local
// scope (.git/config). Equivalent to `git config <key> <value>`.
func (r *Repo) Config(ctx context.Context, key, value string) error {
	_, err := r.Run(ctx, "config", key, value)

	return err
}

// SetRemoteURL runs `git remote set-url <remote> <url>`.
func (r *Repo) SetRemoteURL(ctx context.Context, remote, url string) error {
	_, err := r.Run(ctx, "remote", "set-url", remote, url)

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

// AddPathspecsStrict runs `git add -- <pathspec> ...` and returns errors.
// Release paths use this stricter variant because a missing changelog should
// fail the signing step instead of becoming a misleading no-op.
func (r *Repo) AddPathspecsStrict(ctx context.Context, pathspecs []string) error {
	args := append([]string{"-c", "core.hooksPath=/dev/null", "add", "--"}, pathspecs...)
	_, err := r.Run(ctx, args...)

	return err
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
	args := make([]string, 0, 12)
	if in.NoHooks {
		args = append(args, "-c", "core.hooksPath=/dev/null")
	}

	args = append(args, "commit")
	if in.Sign {
		args = append(args, "-S")
	}

	if in.MessageFile != "" {
		args = append(args, "-F", in.MessageFile)
	} else {
		args = append(args, "-m", in.Message)
	}

	if in.Signoff {
		args = append(args, "--signoff")
	}

	if in.NoVerify {
		args = append(args, "--no-verify")
	}

	if in.AuthorName != "" || in.AuthorEmail != "" {
		args = append(args, "--author",
			fmt.Sprintf("%s <%s>", in.AuthorName, in.AuthorEmail))
	}

	_, err := r.Run(ctx, args...)

	return err
}

// Push runs `git push origin <localRef>:<remoteBranch>`. force=true is
// used for the branch push in the release flow (the bump commit).
//
// When token is non-empty it authenticates the push with a transient,
// origin-scoped HTTP Basic extraheader (the same forge-neutral scheme the
// fetch verbs use), so a credential-free checkout — `platform checkout`, which
// never persists a token to .git/config — can still push. An empty token
// leaves the push unauthenticated (SSH remotes, or a remote that already
// carries ambient credentials), preserving the previous behaviour.
func (r *Repo) Push(ctx context.Context, localRef, remoteBranch string, force bool, cred runcontext.Credential) error {
	env, err := r.pushAuthEnv(ctx, cred)
	if err != nil {
		return err
	}

	args := []string{"push"}
	if force {
		args = append(args, "--force")
	}

	args = append(args, defaultRemote, fmt.Sprintf("%s:%s", localRef, remoteBranch))

	return r.runEnv(ctx, env, args...)
}

// PushBranchNoForce pushes the checked-out branch without --force, matching
// `git -c core.hooksPath=/dev/null push origin <branch>`.
func (r *Repo) PushBranchNoForce(ctx context.Context, branch string, cred runcontext.Credential) error {
	env, err := r.pushAuthEnv(ctx, cred)
	if err != nil {
		return err
	}

	return r.runEnv(ctx, env, "-c", "core.hooksPath=/dev/null", "push", defaultRemote, branch)
}

// CreateTag creates an annotated tag at ref WITHOUT -f (create-once):
// `git -c core.hooksPath=/dev/null tag (-s|-a) <tag> -m <tag> [<ref>]`.
// Because there is no -f, git errors if the tag already exists — the
// create-once release path relies on that so a release tag is never clobbered
// or moved. ref empty → HEAD.
func (r *Repo) CreateTag(ctx context.Context, tag, ref string, signed bool) error {
	args := []string{"-c", "core.hooksPath=/dev/null", "tag"} //nolint:goconst // git subcommand name; a const for "tag" adds noise without clarity.
	if signed {
		args = append(args, "-s")
	} else {
		args = append(args, "-a")
	}

	args = append(args, tag, "-m", tag)
	if ref != "" {
		args = append(args, ref)
	}

	_, err := r.Run(ctx, args...)

	return err
}

// PushTagNoForce pushes a tag without --force:
// `git -c core.hooksPath=/dev/null push origin <tag>`.
// The remote rejects a non-fast-forward update, so this can only create a
// new tag, never overwrite one — the immutability guarantee for release
// tags. token authenticates the push transiently, identical to Push.
func (r *Repo) PushTagNoForce(ctx context.Context, tag string, cred runcontext.Credential) error {
	env, err := r.pushAuthEnv(ctx, cred)
	if err != nil {
		return err
	}

	return r.runEnv(ctx, env, "-c", "core.hooksPath=/dev/null", "push", defaultRemote, tag)
}

// RemoteURL returns origin's configured URL (`git remote get-url origin`),
// used to scope a push's transient auth header to exactly the remote git will
// contact.
func (r *Repo) RemoteURL(ctx context.Context) (string, error) {
	return r.remoteURL(ctx, defaultRemote)
}

func (r *Repo) remoteURL(ctx context.Context, remote string) (string, error) {
	out, err := r.Run(ctx, "remote", "get-url", remote)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(out), nil
}

// pushAuthEnv builds the transient git environment for an authenticated push:
// the forge-neutral HTTP Basic extraheader the fetch verbs use, scoped to
// origin's URL so the token never reaches argv or .git/config. An empty token
// yields just the no-prompt guard, leaving an unauthenticated push unchanged.
func (r *Repo) pushAuthEnv(ctx context.Context, cred runcontext.Credential) ([]string, error) {
	return r.remoteAuthEnv(ctx, defaultRemote, cred)
}

func (r *Repo) remoteAuthEnv(ctx context.Context, remote string, cred runcontext.Credential) ([]string, error) {
	if !cred.Present() {
		return authEnv("", cred), nil
	}

	remoteURL, err := r.remoteURL(ctx, remote)
	if err != nil {
		return nil, fmt.Errorf("resolve %s URL for authenticated git operation: %w", remote, err)
	}

	return authEnv(remoteURL, cred), nil
}
