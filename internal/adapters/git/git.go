// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package git is the thin shell-out adapter around the `git` CLI.
//
// Each method is one git invocation, returning typed values + sentinel
// errors. Callers in app/ orchestrate sequences of these. Domain code
// never touches this package — git operations are I/O.
package git

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/diggsweden/reusable-ci/internal/safeexec"
	gogit "github.com/go-git/go-git/v5"
)

// Repo is a handle for git operations against a working tree.
// The Dir field, when non-empty, sets the cwd for every invocation.
//
// Two execution paths coexist behind these methods:
//
//   - Read-only object lookups (rev-parse, tag-uniqueness, ancestor checks,
//     tag-object inspection) run in-process via go-git. One process for
//     the whole sequence, typed results, structured errors.
//   - Network ops (push, fetch) and signing-driven ops (commit, CreateTag,
//     VerifyTag) remain subprocess. These integrate with SSH agent,
//     credential helpers, and gpg-agent — pure-Go equivalents would
//     reimplement that integration with weaker security/UX.
type Repo struct {
	Dir    string
	GitBin string // override `git` binary path; empty → exec.LookPath("git")

	// gogitOnce memoises the go-git repository open. Concurrent access
	// is safe; the first caller wins.
	gogitOnce sync.Once
	gogitRepo *gogit.Repository
	gogitErr  error
}

// New returns a Repo that runs git against the current working directory
// (Dir == ""). For test isolation, set Dir to t.TempDir().
func New() *Repo { return &Repo{} }

// Run executes `git <args...>` and returns its trimmed stdout. Errors
// are classified through safeexec.WrapError so missing-on-PATH maps to
// ExitCodeUnavailable and non-zero exit maps to ExitCodeValidation;
// git's own diagnostic output is appended for operator context.
func (r *Repo) Run(ctx context.Context, args ...string) (string, error) {
	bin := r.GitBin
	if bin == "" {
		bin = "git" //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}

	cmd := safeexec.Command(ctx, bin, args...)
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		wrapped := safeexec.WrapError(err, bin, firstGitArg(args))
		if len(out) == 0 {
			return "", wrapped
		}

		return "", fmt.Errorf("%w\n%s", wrapped, safeexec.RedactKeyMaterial(out))
	}

	return strings.TrimRight(string(out), "\n"), nil
}

// firstGitArg returns the first non-flag arg as the subcommand label
// (e.g. "rev-parse", "tag", "commit"). Skipping leading "-*" tokens
// means options like "-c user.signingkey=… commit" still produce the
// useful "commit" label instead of "-c".
func firstGitArg(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}

	return ""
}

// RunStdin is Run with a stdin string (used for things like signing input
// or piping commands to git-connect-style tools).
func (r *Repo) RunStdin(ctx context.Context, stdin string, args ...string) (string, error) {
	bin := r.GitBin
	if bin == "" {
		bin = "git"
	}

	cmd := safeexec.Command(ctx, bin, args...)
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}

	cmd.Stdin = strings.NewReader(stdin)

	out, err := cmd.CombinedOutput()
	if err != nil {
		wrapped := safeexec.WrapError(err, bin, firstGitArg(args))
		if len(out) == 0 {
			return "", wrapped
		}

		return "", fmt.Errorf("%w\n%s", wrapped, safeexec.RedactKeyMaterial(out))
	}

	return strings.TrimRight(string(out), "\n"), nil
}

// openGoGit returns a memoised *gogit.Repository rooted at Dir (or cwd
// when Dir == ""). PlainOpenWithOptions{DetectDotGit:true} matches git
// CLI's "walk up looking for .git" behaviour so callers don't have to
// pass the exact .git path.
func (r *Repo) openGoGit() (*gogit.Repository, error) {
	r.gogitOnce.Do(func() {
		path := r.Dir
		if path == "" {
			var err error

			path, err = os.Getwd()
			if err != nil {
				r.gogitErr = fmt.Errorf("getwd: %w", err)

				return
			}
		}

		repo, err := gogit.PlainOpenWithOptions(path, &gogit.PlainOpenOptions{DetectDotGit: true})
		if err != nil {
			r.gogitErr = fmt.Errorf("open git repo at %q: %w", path, err)

			return
		}

		r.gogitRepo = repo
	})

	return r.gogitRepo, r.gogitErr
}
