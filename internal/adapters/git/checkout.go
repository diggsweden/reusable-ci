// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

// Working-tree checkout verbs: the subprocess primitives `platform
// checkout` composes to materialize a consumer repository at an exact
// ref. They are deliberately small one-git-call methods with primitive
// signatures (like the rest of the adapter) so app-layer ports can mirror
// them without importing this package.
//
// They are the Go port of forgejo-ci's checkout-consumer.sh and inherit
// its hardened behaviours: credential-free (the auth header rides
// transient GIT_CONFIG_* env, never argv or .git/config), no interactive
// prompts (GIT_TERMINAL_PROMPT=0), and safe.directory set so a workspace
// whose on-disk owner differs from the job uid (the container-mount case)
// is not refused as dubious ownership.

// runEnv is Run with additional environment entries appended to the inherited
// process environment. It exists for fetch auth: the credential header is
// passed through GIT_CONFIG_* entries set only on this child process, so the
// token stays out of argv and is never written to .git/config. It returns only
// an error — every caller uses --quiet, so stdout is unneeded; on failure the
// captured output is folded (redacted) into the error. Error classification
// mirrors Run exactly.
func (r *Repo) runEnv(ctx context.Context, extraEnv []string, args ...string) error {
	bin := r.GitBin
	if bin == "" {
		bin = "git" //nolint:goconst // generic identifier; matches Run's own default in git.go.
	}

	cmd := safeexec.Command(ctx, bin, args...)
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}

	cmd.Env = append(os.Environ(), extraEnv...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		wrapped := safeexec.WrapError(err, bin, firstGitArg(args))
		if len(out) == 0 {
			return wrapped
		}

		return fmt.Errorf("%w\n%s", wrapped, safeexec.RedactKeyMaterial(out))
	}

	return nil
}

// safeDirArgs returns the -c safe.directory flag for this repo's Dir.
// Every checkout verb runs against its own workspace (Dir), so trusting
// exactly that directory is both correct and minimal. Empty Dir (cwd)
// adds nothing.
func (r *Repo) safeDirArgs() []string {
	if r.Dir == "" {
		return nil
	}

	return []string{"-c", "safe.directory=" + r.Dir}
}

// InitWithObjectFormat runs `git init --quiet` in Dir, selecting the
// repository hash algorithm. Only "sha1" and "sha256" are valid; any
// other value (including the empty string) is rejected before git runs.
func (r *Repo) InitWithObjectFormat(ctx context.Context, format string) error {
	args := append(r.safeDirArgs(), "init", "--quiet")

	switch format {
	case "sha1":
		// git's default; no flag needed.
	case "sha256":
		args = append(args, "--object-format=sha256")
	default:
		return fmt.Errorf("unsupported object format %q (want sha1 or sha256): %w", format, errs.ErrValidation)
	}

	args = append(args, ".")
	_, err := r.Run(ctx, args...)

	return err
}

// RemoteAdd runs `git remote add <name> <url>`.
func (r *Repo) RemoteAdd(ctx context.Context, name, url string) error {
	args := append(r.safeDirArgs(), "remote", "add", name, url)
	_, err := r.Run(ctx, args...)

	return err
}

// Fetch runs a credential-safe `git fetch origin <refspecs...>` using
// protocol v2 and the no-tags/prune/no-submodules flags the consumer
// checkout needs. GIT_TERMINAL_PROMPT is forced off so a missing or wrong
// credential fails fast instead of hanging on a prompt.
//
// When token is non-empty it is sent as HTTP Basic auth
// (x-access-token:<token>, the forge-neutral scheme every major forge
// accepts) via a transient http.<remoteURL>.extraheader git-config entry
// passed in the environment — so the token never appears in argv and is
// never written to .git/config. remoteURL scopes the header to the
// remote, so it is not leaked to a redirect target.
//
// depth > 0 makes a shallow fetch (git --depth=N); depth 0 fetches full
// history (the JS actions/checkout fetch-depth, where 0 means "all").
func (r *Repo) Fetch(ctx context.Context, remoteURL string, refspecs []string, token string, depth int) error {
	args := append(r.safeDirArgs(), "-c", "protocol.version=2",
		"fetch", "--quiet", "--no-tags", "--prune", "--no-recurse-submodules")
	if depth > 0 {
		args = append(args, fmt.Sprintf("--depth=%d", depth))
	}

	args = append(args, "origin")
	args = append(args, refspecs...)

	return r.runEnv(ctx, authEnv(remoteURL, token), args...)
}

// FetchTags fetches all tags from origin (git fetch --tags), the additive
// opt-in for callers that need them (the JS actions/checkout fetch-tags:true).
// The default Fetch uses --no-tags; this runs only when explicitly requested,
// so the common checkout stays lean. Same credential-free auth as Fetch.
func (r *Repo) FetchTags(ctx context.Context, remoteURL, token string) error {
	args := append(r.safeDirArgs(), "-c", "protocol.version=2",
		"fetch", "--quiet", "--tags", "--prune", "--no-recurse-submodules", "origin")

	return r.runEnv(ctx, authEnv(remoteURL, token), args...)
}

// FetchAllRefs fetches every branch into refs/remotes/origin/* plus all tags —
// the all-history checkout (the JS actions/checkout fetch-depth:0). It makes
// sibling branches (e.g. origin/main) and every tag available, which builds
// that read git topology (`git rev-list --count origin/main`, `git describe`)
// rely on. Same credential-free auth as Fetch.
func (r *Repo) FetchAllRefs(ctx context.Context, remoteURL, token string) error {
	args := append(r.safeDirArgs(), "-c", "protocol.version=2",
		"fetch", "--quiet", "--prune", "--no-recurse-submodules", "origin",
		"+refs/heads/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*")

	return r.runEnv(ctx, authEnv(remoteURL, token), args...)
}

// authEnv builds the git environment that injects forge-neutral HTTP Basic
// auth for remoteURL without the token ever appearing in argv or .git/config.
// An empty token yields just the no-prompt guard. Shared by every fetch.
func authEnv(remoteURL, token string) []string {
	env := make([]string, 0, 4)
	env = append(env, "GIT_TERMINAL_PROMPT=0")

	if token == "" {
		return env
	}

	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))

	return append(env,
		"GIT_CONFIG_COUNT=1",
		fmt.Sprintf("GIT_CONFIG_KEY_0=http.%s.extraheader", remoteURL),
		"GIT_CONFIG_VALUE_0=Authorization: Basic "+basic,
	)
}

// EnablePartialClone marks origin a promisor remote with a blob:none filter,
// so subsequent fetches download only the objects the working tree needs (the
// sparse cone) and backfill the rest on demand. It configures exactly what
// `git clone --filter=blob:none` does. A server that does not support partial
// clone ignores the filter and fetches fully — harmless, since the promisor
// flag then has nothing to backfill. Run after RemoteAdd, before the fetch.
func (r *Repo) EnablePartialClone(ctx context.Context) error {
	if _, err := r.Run(ctx, append(r.safeDirArgs(), "config", "remote.origin.promisor", "true")...); err != nil {
		return err
	}

	_, err := r.Run(ctx, append(r.safeDirArgs(), "config", "remote.origin.partialclonefilter", "blob:none")...)

	return err
}

// SparseInit enables sparse-checkout for the repo. cone selects cone mode
// (directory-prefix patterns), which is the fast, well-defined form the JS
// actions/checkout uses and the only form we expose. Run before SparseSet and
// before any checkout, so the working tree is only ever populated with the
// selected cone — never the whole tree.
func (r *Repo) SparseInit(ctx context.Context, cone bool) error {
	args := append(r.safeDirArgs(), "sparse-checkout", "init")
	if cone {
		args = append(args, "--cone")
	}

	_, err := r.Run(ctx, args...)

	return err
}

// SparseSet restricts the working tree to the given cone patterns (directory
// prefixes, e.g. "scripts/bootstrap"). A subsequent checkout materializes only
// these paths.
func (r *Repo) SparseSet(ctx context.Context, patterns []string) error {
	args := append(r.safeDirArgs(), "sparse-checkout", "set")
	args = append(args, patterns...)

	_, err := r.Run(ctx, args...)

	return err
}

// CheckoutDetach checks out ref in detached-HEAD state, quietly.
func (r *Repo) CheckoutDetach(ctx context.Context, ref string) error {
	args := append(r.safeDirArgs(), "checkout", "--quiet", "--detach", ref)
	_, err := r.Run(ctx, args...)

	return err
}
