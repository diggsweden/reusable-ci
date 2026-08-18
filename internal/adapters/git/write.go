// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// AddPathspecs stages each pathspec with a separate `git add -- <pathspec>`.
//
// One invocation per pathspec is deliberate, not wasteful. `git add`
// aborts the *entire* invocation when any pathspec matches nothing —
// it does not stage the ones that did match:
//
//	git add -- CHANGELOG.md build.gradle.kts build.gradle  # build.gradle absent
//	fatal: pathspec 'build.gradle' did not match any files  (exit 128, nothing staged)
//
// Version-bump patterns legitimately list files a given project may not
// have (a Kotlin-DSL Gradle project has no build.gradle; a pnpm project
// has no package-lock.json), so a single batched add would silently stage
// nothing and the bump commit would be skipped with no error. Pathspec
// glob magic does not help: `:(glob)settings.gradle*` is equally fatal
// when it matches nothing.
//
// Per-pathspec failures are best-effort — a missing optional project file
// is normal — but they are logged rather than swallowed, because that
// silence is exactly what hid the batched-add bug. Callers still use
// HasStagedChanges() to decide whether anything was actually added.
func (r *Repo) AddPathspecs(ctx context.Context, pathspecs []string) {
	for _, pathspec := range pathspecs {
		if _, err := r.Run(ctx, "add", "--", pathspec); err != nil {
			slog.Debug("git add staged nothing for pathspec",
				"pathspec", pathspec, "err", err)
		}
	}
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

// Push runs `git push origin <localRef>:<remoteBranch>`. force=true is
// used for the branch push in the release flow (the bump commit).
func (r *Repo) Push(ctx context.Context, localRef, remoteBranch string, force bool) error {
	args := []string{"push"}
	if force {
		args = append(args, "--force")
	}

	args = append(args, "origin", fmt.Sprintf("%s:%s", localRef, remoteBranch))
	_, err := r.Run(ctx, args...)

	return err
}

// CreateTag creates an annotated tag at ref WITHOUT -f (create-once):
// `git tag [-s] <tag> -m <tag> [<ref>]`. Because there is no -f, git
// errors if the tag already exists — the create-once release path relies
// on that so a release tag is never clobbered or moved. ref empty → HEAD.
func (r *Repo) CreateTag(ctx context.Context, tag, ref string, signed bool) error {
	args := []string{"tag"}
	if signed {
		args = append(args, "-s")
	}

	args = append(args, tag, "-m", tag)
	if ref != "" {
		args = append(args, ref)
	}

	_, err := r.Run(ctx, args...)

	return err
}

// PushTagNoForce pushes a tag without --force: `git push origin <tag>`.
// The remote rejects a non-fast-forward update, so this can only create a
// new tag, never overwrite one — the immutability guarantee for release
// tags.
func (r *Repo) PushTagNoForce(ctx context.Context, tag string) error {
	_, err := r.Run(ctx, "push", "origin", tag)

	return err
}
