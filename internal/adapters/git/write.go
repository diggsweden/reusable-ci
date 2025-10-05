// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
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

// AddPathspecs runs `git add -- <pathspec>` once per pathspec. A pathspec
// that matches no files causes git to exit non-zero; this is *expected*
// during bump flows where the bump may legitimately produce nothing to
// stage. The error is swallowed — callers use HasStagedChanges() to
// determine if anything was actually added.
//
// One invocation per pathspec, not one for all of them: git add is
// all-or-nothing across its arguments, so a single unmatched pathspec makes
// it exit 128 having staged NOTHING, not merely skip that one. The bump
// file patterns deliberately name files a project may not have (both Gradle
// DSL spellings, package-lock.json), so the combined call staged nothing
// for every such project and the version-bump commit was silently skipped
// as "No staged changes". Per-pathspec calls make "matches nothing" local to
// the pathspec that matched nothing, which is what this method promises.
func (r *Repo) AddPathspecs(ctx context.Context, pathspecs []string) {
	for _, pathspec := range pathspecs {
		_, _ = r.Run(ctx, "add", "--", pathspec) // intentional swallow
	}
}

// AddPathspecsStrict runs `git add -- <pathspec> ...` and returns errors.
// Release paths use this stricter variant because a missing changelog should
// fail the signing step instead of becoming a misleading no-op.
func (r *Repo) AddPathspecsStrict(ctx context.Context, pathspecs []string) error {
	args := append([]string{"-c", hooksDisabledConfig, "add", "--"}, pathspecs...)
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

	return false, safeexec.WrapError(err, bin, "diff")
}

// Commit runs `git commit` with the given input. Author is passed via
// --author "Name <email>"; signoff appends the trailer when set.
func (r *Repo) Commit(ctx context.Context, in domaingit.CommitInput) error {
	args := make([]string, 0, 12)
	if in.NoHooks {
		args = append(args, "-c", hooksDisabledConfig)
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

// Push runs `git -c core.hooksPath=/dev/null push origin
// <localRef>:<remoteBranch>`. force=true is
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

	args := []string{"-c", hooksDisabledConfig, "push"} //nolint:goconst // Keep native Git command names visible in argv.
	if force {
		args = append(args, "--force")
	}

	args = append(args, defaultRemote, fmt.Sprintf("%s:%s", localRef, remoteBranch))

	return r.runEnv(ctx, env, args...)
}

// PushCommitWithLease publishes only the captured commit and literal branch.
// The caller verifies its parent before calling. Keep origin as the repository
// argument: passing get-url output back to Git could rewrite it a second time
// and would discard named-remote transport settings (e.g. receivepack/proxy).
func (r *Repo) PushCommitWithLease(ctx context.Context, in domaingit.BranchPushInput, cred runcontext.Credential) error {
	args, err := leasedBranchPushArgs(in)
	if err != nil {
		return err
	}

	if destinationErr := r.CheckOriginPushDestination(ctx); destinationErr != nil {
		return destinationErr
	}

	env, err := r.pushAuthEnv(ctx, cred)
	if err != nil {
		return err
	}

	return r.runEnv(ctx, env, args...)
}

func leasedBranchPushArgs(in domaingit.BranchPushInput) ([]string, error) {
	ref := "refs/heads/" + in.Branch
	if !domaingit.ValidCommitSHA(in.CommitSHA) || !domaingit.ValidCommitSHA(in.ExpectedSHA) ||
		in.CommitSHA == in.ExpectedSHA || len(in.CommitSHA) != len(in.ExpectedSHA) || !domaingit.ValidRefName(ref) {
		return nil, fmt.Errorf("leased push requires new/full matching-format OIDs and a literal branch: %w", errs.ErrUsage)
	}

	// Do not let repo config expand this authorization to other refs or repos.
	return []string{"--no-replace-objects", "-c", hooksDisabledConfig, "-c", "remote.origin.mirror=false", "push", //nolint:goconst // Keep the exact publication policy visible in argv.
		"--no-follow-tags", "--recurse-submodules=no", "--force-with-lease=" + ref + ":" + in.ExpectedSHA, //nolint:goconst // Native Git flags, not application vocabulary.
		"--", defaultRemote, in.CommitSHA + "^{commit}:" + ref}, nil
}

// PushBranchNoForce publishes one literal local branch to the same branch on
// origin, without force or config-driven publication of other refs/repositories.
func (r *Repo) PushBranchNoForce(ctx context.Context, branch string, cred runcontext.Credential) error {
	args, err := branchPushArgs(branch)
	if err != nil {
		return err
	}

	if destinationErr := r.CheckOriginPushDestination(ctx); destinationErr != nil {
		return destinationErr
	}

	env, err := r.pushAuthEnv(ctx, cred)
	if err != nil {
		return err
	}

	return r.runEnv(ctx, env, args...)
}

func branchPushArgs(branch string) ([]string, error) {
	ref := "refs/heads/" + branch
	if strings.HasPrefix(branch, "-") || strings.HasPrefix(branch, "+") || !domaingit.ValidRefName(ref) {
		return nil, fmt.Errorf("branch push requires a literal branch name, not an option or refspec: %w", errs.ErrUsage)
	}

	return []string{"--no-replace-objects", "-c", hooksDisabledConfig, "-c", "remote.origin.mirror=false", "push",
		"--no-follow-tags", "--recurse-submodules=no", "--", defaultRemote, ref + ":" + ref}, nil
}

// CreateTag creates an annotated tag at ref WITHOUT -f (create-once):
// `git --no-replace-objects -c core.hooksPath=/dev/null tag (-s|-a) <tag> -m <tag> [<ref>]`.
// Because there is no -f, git errors if the tag already exists — the
// create-once release path relies on that so a release tag is never clobbered
// or moved. ref empty → HEAD. Full commit SHAs use an explicit commit expression.
func (r *Repo) CreateTag(ctx context.Context, tag, ref string, signed bool) error {
	_, err := r.Run(ctx, createTagArgs(tag, ref, signed)...)

	return err
}

func createTagArgs(tag, ref string, signed bool) []string {
	args := []string{"--no-replace-objects", "-c", hooksDisabledConfig, "tag"} //nolint:goconst // git subcommand name; a const for "tag" adds noise without clarity.
	if signed {
		args = append(args, "-s")
	} else {
		args = append(args, "-a")
	}

	args = append(args, tag, "-m", tag)

	if domaingit.ValidCommitSHA(ref) {
		ref += "^{commit}"
	}

	if ref != "" {
		args = append(args, ref)
	}

	return args
}

// PushTagNoForce pushes a tag without --force:
// `git -c core.hooksPath=/dev/null push origin refs/tags/<tag>:refs/tags/<tag>`.
// The remote rejects a non-fast-forward update, so this can only create a
// new tag, never overwrite one — the immutability guarantee for release
// tags. token authenticates the push transiently, identical to Push.
func (r *Repo) PushTagNoForce(ctx context.Context, tag string, cred runcontext.Credential) error {
	args, err := tagPushArgs(tag)
	if err != nil {
		return err
	}

	if destinationErr := r.CheckOriginPushDestination(ctx); destinationErr != nil {
		return destinationErr
	}

	env, err := r.pushAuthEnv(ctx, cred)
	if err != nil {
		return err
	}

	return r.runEnv(ctx, env, args...)
}

func tagPushArgs(tag string) ([]string, error) {
	ref := refsTagsPrefix + tag
	if !domaingit.ValidRefName(ref) {
		return nil, fmt.Errorf("tag push requires a literal tag name: %w", errs.ErrUsage)
	}

	return []string{"--no-replace-objects", "-c", hooksDisabledConfig, "-c", "remote.origin.mirror=false", "push",
		"--no-follow-tags", "--recurse-submodules=no", "--", defaultRemote, ref + ":" + ref}, nil
}

// RemoteURL returns origin's effective fetch URL, after native URL rewriting.
// It is an audience/display value, not an argument to feed back into Git: that
// could apply a second rewrite. Queries against origin should pass "origin".
func (r *Repo) RemoteURL(ctx context.Context) (string, error) {
	return r.effectiveRepositoryURL(ctx, defaultRemote)
}

func (r *Repo) effectiveRepositoryURL(ctx context.Context, repository string) (string, error) {
	out, err := r.Run(ctx, repositoryURLArgs(repository)...)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(out), nil
}

func repositoryURLArgs(repository string) []string {
	// Unlike remote get-url, this accepts both remote names and literal URLs,
	// expands their fetch rewrites, and exits without contacting the repository.
	return []string{"-c", hooksDisabledConfig, "ls-remote", "--get-url", "--", repository} //nolint:goconst // Native no-network repository-argument resolution.
}

// CheckOriginPushDestination refuses split or multiple publication destinations.
// Native get-url expands insteadOf/pushInsteadOf with Git's own config rules.
// Under the single-writer checkout assumption, reads and pushes through origin
// then contact the same destination, with the same URL-scoped authentication.
func (r *Repo) CheckOriginPushDestination(ctx context.Context) error {
	if err := validateRepositoryRouting(os.Environ()); err != nil {
		return err
	}

	fetch, err := r.Run(ctx, originURLArgs(false)...)
	if err != nil {
		return fmt.Errorf("inspect origin fetch destination: %w", err)
	}

	push, err := r.Run(ctx, originURLArgs(true)...)
	if err != nil {
		return fmt.Errorf("inspect origin push destination: %w", err)
	}

	return validateOriginURLs(fetch, push)
}

func validateRepositoryRouting(env []string) error {
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE",
			"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE",
			"GIT_SHALLOW_FILE", "GIT_GRAFT_FILE", "GIT_REPLACE_REF_BASE",
			"GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM":
			// Native and go-git reads must not select different repositories.
			// Reject even empty overrides, without echoing their values. Named
			// remote/URL config through GIT_CONFIG_COUNT remains supported.
			return fmt.Errorf("release publication does not support %s repository-routing overrides: %w", key, errs.ErrValidation)
		}
	}

	return nil
}

func originURLArgs(push bool) []string {
	args := []string{"remote", "get-url", "--all"} //nolint:goconst // Native Git flags, not application vocabulary.
	if push {
		args = append(args, "--push")
	}

	return append(args, defaultRemote)
}

func validateOriginURLs(fetch, push string) error {
	fetch = strings.TrimSuffix(fetch, "\n")

	push = strings.TrimSuffix(push, "\n")
	if fetch == "" || fetch != strings.TrimSpace(fetch) || strings.ContainsAny(fetch, "\r\n\x00") || fetch != push {
		// URLs may embed credentials. Do not include either value in diagnostics.
		return fmt.Errorf("origin must have one identical fetch and push destination: %w", errs.ErrValidation)
	}

	return nil
}

// pushAuthEnv builds the transient git environment for an authenticated push:
// the forge-neutral HTTP Basic extraheader the fetch verbs use, scoped to
// origin's URL so the token never reaches argv or .git/config. An empty token
// yields just the no-prompt guard, leaving an unauthenticated push unchanged.
func (r *Repo) pushAuthEnv(ctx context.Context, cred runcontext.Credential) ([]string, error) {
	return r.remoteAuthEnv(ctx, defaultRemote, cred)
}

func (r *Repo) remoteAuthEnv(ctx context.Context, repository string, cred runcontext.Credential) ([]string, error) {
	if !cred.Present() {
		return authEnv("", cred), nil
	}

	remoteURL, err := r.effectiveRepositoryURL(ctx, repository)
	if err != nil {
		return nil, fmt.Errorf("resolve repository audience for authenticated git operation: %w", err)
	}

	return appendRemoteAuthConfig(authEnv(remoteURL, cred), os.Getenv("GIT_CONFIG_COUNT"))
}

// authEnv owns one transient config entry. Append it after inherited entries
// rather than replacing their count and silently dropping URL/transport config.
func appendRemoteAuthConfig(env []string, inheritedCount string) ([]string, error) {
	if len(env) == 1 || inheritedCount == "" {
		return env, nil
	}

	count, err := strconv.ParseUint(inheritedCount, 10, 31)
	if err != nil || count == 1<<31-1 {
		return nil, fmt.Errorf("invalid inherited Git config count: %w", errs.ErrValidation)
	}

	return []string{env[0], "GIT_CONFIG_COUNT=" + strconv.FormatUint(count+1, 10),
		"GIT_CONFIG_KEY_" + strconv.FormatUint(count, 10) + "=" + strings.TrimPrefix(env[2], "GIT_CONFIG_KEY_0="),
		"GIT_CONFIG_VALUE_" + strconv.FormatUint(count, 10) + "=" + strings.TrimPrefix(env[3], "GIT_CONFIG_VALUE_0=")}, nil
}
