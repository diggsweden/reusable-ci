// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	domaingit "github.com/diggsweden/reusable-ci/internal/domain/git"
)

// Config writes a single git config entry in the working-tree-local scope
// (i.e. .git/config). Equivalent to `git config <key> <value>`.
func (r *Repo) Config(ctx context.Context, key, value string) error {
	_, err := r.Run(ctx, "config", key, value)
	return err
}

// AddPathspecs runs `git add -- <pathspec> ...`. A pathspec that matches no
// files causes git to exit non-zero; this is *expected* during bump flows
// where the bump may legitimately produce nothing to stage. The error is
// swallowed — callers use HasStagedChanges() to determine if anything was
// actually added.
//
// Mirrors the `git add -- $FILE_PATTERN 2>/dev/null || true` line in
// scripts/version/commit-and-push.sh.
func (r *Repo) AddPathspecs(ctx context.Context, pathspecs []string) {
	args := append([]string{"add", "--"}, pathspecs...)
	_, _ = r.Run(ctx, args...) // intentional swallow
}

// HasStagedChanges reports whether the index differs from HEAD.
func (r *Repo) HasStagedChanges(ctx context.Context) (bool, error) {
	bin := r.GitBin
	if bin == "" {
		bin = "git"
	}
	args := []string{"diff", "--cached", "--quiet"}
	// We don't use Run() here because exit code 1 is the success signal
	// (means there *are* staged changes), not an error.
	cmd := exec.CommandContext(ctx, bin, args...)
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
		return false, fmt.Errorf("git diff --cached --quiet: exit %d\n%s", exitErr.ExitCode(), exitErr.Stderr)
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

// Push runs `git push origin <localRef>:<remoteBranch>`. Pass force=true
// for tag re-pushes (move-tag).
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

// RevParse returns `git rev-parse <ref>` (the resolved SHA).
func (r *Repo) RevParse(ctx context.Context, ref string) (string, error) {
	return r.Run(ctx, "rev-parse", ref)
}

// DescribeLatestTag returns the most recent tag reachable from HEAD,
// matching `git describe --tags --abbrev=0`.
func (r *Repo) DescribeLatestTag(ctx context.Context) (string, error) {
	return r.Run(ctx, "describe", "--tags", "--abbrev=0")
}

// TagSHA returns the commit SHA the given tag points to (`git rev-list -n 1`).
func (r *Repo) TagSHA(ctx context.Context, tag string) (string, error) {
	return r.Run(ctx, "rev-list", "-n", "1", tag)
}

// MoveTag deletes-and-recreates an annotated tag at HEAD.
// signed=true adds -s (GPG signing) — production behaviour. Tests pass
// signed=false to skip the GPG dependency in their isolated repos.
//
// Mirrors `git tag -f [-s] <tag> -m <tag>` from move-tag.sh.
func (r *Repo) MoveTag(ctx context.Context, tag string, signed bool) error {
	args := []string{"tag", "-f"}
	if signed {
		args = append(args, "-s")
	}
	args = append(args, tag, "-m", tag)
	_, err := r.Run(ctx, args...)
	return err
}

// ShortSHA returns `git rev-parse --short=<n> <ref>`.
func (r *Repo) ShortSHA(ctx context.Context, ref string, n int) (string, error) {
	return r.Run(ctx, "rev-parse", fmt.Sprintf("--short=%d", n), ref)
}

// ListTags returns `git tag -l <pattern>` as a slice (one tag per line).
// Empty slice when nothing matches.
func (r *Repo) ListTags(ctx context.Context, pattern string) ([]string, error) {
	args := []string{"tag", "-l"}
	if pattern != "" {
		args = append(args, pattern)
	}
	out, err := r.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// TagsPointingAt returns `git tag --points-at <commit>` as a slice.
// Empty when nothing matches.
func (r *Repo) TagsPointingAt(ctx context.Context, commit string) ([]string, error) {
	out, err := r.Run(ctx, "tag", "--points-at", commit)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// IsAncestor reports whether ancestor is in the history of descendant
// (`git merge-base --is-ancestor`). Exit 0 → true, exit 1 → false,
// other exit codes → error.
func (r *Repo) IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	bin := r.GitBin
	if bin == "" {
		bin = "git"
	}
	cmd := exec.CommandContext(ctx, bin, "merge-base", "--is-ancestor", ancestor, descendant)
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git merge-base --is-ancestor %s %s: exit %d", ancestor, descendant, exitErr.ExitCode())
	}
	return false, err
}

// CatFileType returns `git cat-file -t <ref>`. For tag refs:
// "tag" = annotated, "commit" = lightweight.
func (r *Repo) CatFileType(ctx context.Context, ref string) (string, error) {
	return r.Run(ctx, "cat-file", "-t", ref)
}

// CatFileTag returns the raw object body of an annotated tag
// (`git cat-file tag <tag>`). Used to detect embedded PGP/SSH signatures.
func (r *Repo) CatFileTag(ctx context.Context, tag string) (string, error) {
	return r.Run(ctx, "cat-file", "tag", tag)
}

// VerifyTag runs `git tag -v <tag>` and returns combined output.
// Returns (output, true, nil) on success, (output, false, nil) when
// verification fails (signer key missing locally, etc.). Other errors
// (e.g. tag doesn't exist) propagate.
func (r *Repo) VerifyTag(ctx context.Context, tag string) (string, bool, error) {
	bin := r.GitBin
	if bin == "" {
		bin = "git"
	}
	cmd := exec.CommandContext(ctx, bin, "tag", "-v", tag)
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// git tag -v exits 1 on verification failure (e.g. unknown key).
		// We surface the output but report ok=false instead of erroring.
		return string(out), false, nil
	}
	return "", false, fmt.Errorf("git tag -v %s: %w", tag, err)
}

// TaggerInfo returns the tagger metadata for refs/tags/<tag> as
// (name+email, ISO8601 date). Both fields are best-effort: missing
// values come back as empty strings.
//
// Two separate `git for-each-ref` invocations because the `%n` newline
// placeholder isn't honored consistently across git versions.
func (r *Repo) TaggerInfo(ctx context.Context, tag string) (string, string, error) {
	tagger, err := r.Run(ctx, "for-each-ref", "refs/tags/"+tag,
		"--format=%(taggername) <%(taggeremail)>")
	if err != nil {
		return "", "", err
	}
	date, err := r.Run(ctx, "for-each-ref", "refs/tags/"+tag,
		"--format=%(taggerdate:iso8601)")
	if err != nil {
		return "", "", err
	}
	return tagger, date, nil
}

// CommitInfo returns metadata about commit sha. Used by the summary
// prerequisites writer.
func (r *Repo) CommitInfo(ctx context.Context, sha string) (domaingit.CommitInfo, error) {
	author, err := r.Run(ctx, "log", "-1", "--format=%an <%ae>", sha)
	if err != nil {
		return domaingit.CommitInfo{}, err
	}
	date, err := r.Run(ctx, "log", "-1", "--format=%cs", sha)
	if err != nil {
		return domaingit.CommitInfo{}, err
	}
	message, err := r.Run(ctx, "log", "-1", "--format=%s", sha)
	if err != nil {
		return domaingit.CommitInfo{}, err
	}
	body, err := r.Run(ctx, "cat-file", "commit", sha)
	if err != nil {
		return domaingit.CommitInfo{}, err
	}
	return domaingit.CommitInfo{Author: author, Date: date, Message: message, Body: body}, nil
}

// TagMessage returns the message body of an annotated tag — equivalent
// to `git tag -l -n999 <tag>` with the leading "<tag>" column stripped.
func (r *Repo) TagMessage(ctx context.Context, tag string) (string, error) {
	out, err := r.Run(ctx, "tag", "-l", "-n999", tag)
	if err != nil {
		return "", err
	}
	// Format: "<tag> <message line 1>\n    <message line 2>\n..."
	// Strip the first whitespace-separated column on each line.
	lines := strings.Split(out, "\n")
	stripped := make([]string, 0, len(lines))
	for _, line := range lines {
		// `git tag -l -n999` left-pads continuation lines with the same
		// width as the tag column. We just trim leading whitespace.
		// First line has the tag name first; later lines are pure message.
		idx := strings.Index(line, " ")
		if idx == -1 {
			stripped = append(stripped, "")
			continue
		}
		stripped = append(stripped, strings.TrimLeft(line[idx:], " "))
	}
	return strings.Join(stripped, "\n"), nil
}
