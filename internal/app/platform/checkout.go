// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	domainci "github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// CheckoutGit is the git operation subset Checkout drives. The adapter's
// *git.Repo (with Dir set to the workspace) satisfies it structurally; the
// port keeps app/ci free of any adapter import, and the credential
// mechanics stay encapsulated in the adapter's Fetch.
type CheckoutGit interface {
	InitWithObjectFormat(ctx context.Context, format string) error
	RemoteAdd(ctx context.Context, name, url string) error
	Fetch(ctx context.Context, remoteURL string, refspecs []string, cred runcontext.Credential, depth int) error
	FetchTags(ctx context.Context, remoteURL string, cred runcontext.Credential) error
	FetchAllRefs(ctx context.Context, remoteURL string, cred runcontext.Credential) error
	EnablePartialClone(ctx context.Context) error
	SparseInit(ctx context.Context, cone bool) error
	SparseSet(ctx context.Context, patterns []string) error
	CheckoutDetach(ctx context.Context, ref string) error
	RevParse(ctx context.Context, rev string) (string, error)
}

// CheckoutInput drives Checkout.
type CheckoutInput struct {
	Repository   string                // "owner/name"
	ServerURL    string                // e.g. https://codeberg.org
	Ref          string                // commit SHA, refs/tags/…, refs/heads/…, or a bare tag/branch name
	Workspace    string                // target directory (must not already contain .git)
	Token        runcontext.Credential // optional; absent means an anonymous checkout
	ObjectFormat string                // optional explicit override ("sha1"/"sha256")
	FetchBase    string                // optional extra branch to also fetch (diff/commit-range checks)
	FetchTags    bool                  // also fetch all tags (the JS actions/checkout fetch-tags:true)
	FetchAllRefs bool                  // fetch every branch into refs/remotes/origin/* plus all tags (actions/checkout fetch-depth:0)
	Depth        int                   // shallow history depth for the primary ref fetch; 0 = full history (mirrors the JS actions/checkout fetch-depth, where 0 means "all"). Ignored for fetch-all-refs/fetch-tags, which are inherently full.
	Sparse       []string              // cone patterns; non-empty restricts the working tree to these dirs
	OutputKey    string                // OutputSink key for the resolved SHA (default "checkout-sha")
}

// Checkout materializes a consumer repository at an exact ref into the
// workspace: a credential-free, sha256-aware git checkout for jobs where
// the JavaScript actions/checkout cannot run. It is the Go port of
// forgejo-ci's checkout-consumer.sh and reproduces its resolution order
// (full refs → SHA → bare-name tag-then-branch probe) and hardened
// behaviours (named errors, quiet probe misses, no interactive prompts,
// unsafe-ref rejection). The resolved commit SHA is written to the
// OutputSink and returned.
func Checkout(ctx context.Context, git CheckoutGit, sink domainci.OutputSink, w io.Writer, in CheckoutInput) (string, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err := validateCheckoutInput(in); err != nil {
		return "", err
	}

	if err := guardEmptyWorkspace(in.Workspace); err != nil {
		return "", err
	}

	// The command resolves the override-or-provider object format and
	// passes it in; an empty value (no override, forge does not report
	// one) defaults to sha1.
	format := in.ObjectFormat
	if format == "" {
		format = "sha1"
	}

	if err := git.InitWithObjectFormat(ctx, format); err != nil {
		return "", fmt.Errorf("init %s repository in %s: %w", format, in.Workspace, err)
	}

	remoteURL := strings.TrimRight(in.ServerURL, "/") + "/" + in.Repository + ".git"
	if err := git.RemoteAdd(ctx, "origin", remoteURL); err != nil {
		return "", fmt.Errorf("add origin %s: %w", remoteURL, err)
	}

	if err := configureSparse(ctx, git, in); err != nil {
		return "", err
	}

	if err := fetchAllRefs(ctx, git, remoteURL, in); err != nil {
		return "", err
	}

	if err := fetchAndCheckout(ctx, git, remoteURL, in); err != nil {
		return "", err
	}

	if err := fetchExtras(ctx, git, remoteURL, in); err != nil {
		return "", err
	}

	return reportCheckout(ctx, git, sink, w, in, format)
}

// fetchAllRefs performs the all-history checkout when requested: it populates
// every sibling branch (refs/remotes/origin/*) and all tags up front, so
// builds that read git topology — `git rev-list --count origin/main`,
// `git describe` — see the same refs the JS actions/checkout fetch-depth:0
// provides. The targeted fetchAndCheckout that follows still pins the exact
// ref. A no-op when not requested.
func fetchAllRefs(ctx context.Context, git CheckoutGit, remoteURL string, in CheckoutInput) error {
	if !in.FetchAllRefs {
		return nil
	}

	if err := git.FetchAllRefs(ctx, remoteURL, in.Token); err != nil {
		return fmt.Errorf("fetch all refs: %w", err)
	}

	return nil
}

// configureSparse sets up cone-mode sparse-checkout BEFORE any checkout, so the
// working tree is only ever populated with the requested cone — the subtree
// never lands on disk in full. (Partial-clone blob filtering is a download
// optimization that can be layered on later without changing this.) A no-op
// when no patterns are requested.
func configureSparse(ctx context.Context, git CheckoutGit, in CheckoutInput) error {
	if len(in.Sparse) == 0 {
		return nil
	}

	// Pair sparse-checkout with a blob:none partial clone (as the JS
	// actions/checkout does): only the cone's blobs download. Degrades to a
	// full fetch on servers without partial-clone support.
	if err := git.EnablePartialClone(ctx); err != nil {
		return fmt.Errorf("enable partial clone: %w", err)
	}

	if err := git.SparseInit(ctx, true); err != nil {
		return fmt.Errorf("sparse-checkout init: %w", err)
	}

	if err := git.SparseSet(ctx, in.Sparse); err != nil {
		return fmt.Errorf("sparse-checkout set: %w", err)
	}

	return nil
}

// fetchExtras performs the optional post-checkout fetches: the best-effort base
// branch (an optimization for diff/commit-range checks — a miss must not fail,
// matching the shell wrapper's `|| true`) and the opt-in tag fetch (explicitly
// requested, so a failure is real).
func fetchExtras(ctx context.Context, git CheckoutGit, remoteURL string, in CheckoutInput) error {
	if in.FetchBase != "" {
		// Full history for the base branch: diff/commit-range checks need its
		// ancestry, so this is not narrowed by --depth.
		_ = git.Fetch(ctx, remoteURL,
			[]string{"+refs/heads/" + in.FetchBase + ":refs/heads/" + in.FetchBase}, in.Token, 0)
	}

	if in.FetchTags {
		if err := git.FetchTags(ctx, remoteURL, in.Token); err != nil {
			return fmt.Errorf("fetch tags: %w", err)
		}
	}

	return nil
}

// reportCheckout resolves the checked-out HEAD, records it on the output
// sink, and prints the summary line. Split out so Checkout stays within
// the cyclomatic budget.
func reportCheckout(ctx context.Context, git CheckoutGit, sink domainci.OutputSink, w io.Writer, in CheckoutInput, format string) (string, error) { //nolint:varnamelen // idiomatic short names (testing/http/io conventions).
	sha, err := git.RevParse(ctx, "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve checked-out HEAD: %w", err)
	}

	key := in.OutputKey
	if key == "" {
		key = "checkout-sha"
	}

	if err := sink.Set(ctx, key, sha); err != nil {
		return "", err
	}

	if w != nil {
		_, _ = fmt.Fprintf(w, "Checked out %s at %s (%s)\n", in.Repository, sha, format)
	}

	return sha, nil
}

// fetchAndCheckout reproduces checkout-consumer.sh's ref resolution order.
// The explicit kinds (full refs, SHA) fetch-or-fail with a named error; the
// bare-name case probes tag then branch, treating a probe miss as control
// flow (the adapter captures git's stderr, so a miss is silent) and naming
// the failure only when neither exists.
func fetchAndCheckout(ctx context.Context, git CheckoutGit, remoteURL string, in CheckoutInput) error {
	ref := in.Ref

	switch {
	case strings.HasPrefix(ref, "refs/tags/"):
		if err := git.Fetch(ctx, remoteURL, []string{"+" + ref + ":" + ref}, in.Token, in.Depth); err != nil {
			return fetchError(ref, remoteURL, err)
		}

		return git.CheckoutDetach(ctx, ref)

	case strings.HasPrefix(ref, "refs/heads/"):
		branch := strings.TrimPrefix(ref, "refs/heads/")

		dst := "refs/remotes/origin/" + branch
		if err := git.Fetch(ctx, remoteURL, []string{"+" + ref + ":" + dst}, in.Token, in.Depth); err != nil {
			return fetchError(ref, remoteURL, err)
		}

		return git.CheckoutDetach(ctx, dst)

	case isHexSHA(ref):
		if err := git.Fetch(ctx, remoteURL, []string{ref}, in.Token, in.Depth); err != nil {
			return fetchError(ref, remoteURL, err)
		}

		return git.CheckoutDetach(ctx, "FETCH_HEAD")

	default:
		return fetchBareName(ctx, git, remoteURL, in)
	}
}

func fetchBareName(ctx context.Context, git CheckoutGit, remoteURL string, in CheckoutInput) error {
	ref := in.Ref

	tagDst := "refs/tags/" + ref
	if err := git.Fetch(ctx, remoteURL, []string{"+" + tagDst + ":" + tagDst}, in.Token, in.Depth); err == nil {
		return git.CheckoutDetach(ctx, tagDst)
	}

	branchDst := "refs/remotes/origin/" + ref
	if err := git.Fetch(ctx, remoteURL, []string{"+refs/heads/" + ref + ":" + branchDst}, in.Token, in.Depth); err == nil {
		return git.CheckoutDetach(ctx, branchDst)
	}

	return fmt.Errorf("ref %q not found as a tag or branch on %s: %w", ref, remoteURL, errs.ErrValidation)
}

func fetchError(ref, remoteURL string, err error) error {
	return fmt.Errorf("cannot fetch %q from %s (wrong ref, or the server refuses unadvertised SHAs): %w", ref, remoteURL, err)
}

func validateCheckoutInput(in CheckoutInput) error {
	if in.Repository == "" {
		return fmt.Errorf("repository (owner/name) is required: %w", errs.ErrUsage)
	}

	if in.ServerURL == "" {
		return fmt.Errorf("server-url is required: %w", errs.ErrUsage)
	}

	if in.Workspace == "" {
		return fmt.Errorf("workspace is required: %w", errs.ErrUsage)
	}

	return validCheckoutRef(in.Ref)
}

// validCheckoutRef rejects refs that are empty, dash-prefixed (would be
// parsed as a git option), or carry embedded newlines/CRs — the unsafe-ref
// guard the shell learned.
func validCheckoutRef(ref string) error {
	switch {
	case ref == "":
		return fmt.Errorf("ref is required: %w", errs.ErrUsage)
	case strings.HasPrefix(ref, "-"):
		return fmt.Errorf("unsafe ref %q (leading dash): %w", ref, errs.ErrValidation)
	case strings.ContainsAny(ref, "\n\r"):
		return fmt.Errorf("unsafe ref (embedded newline): %w", errs.ErrValidation)
	}

	return nil
}

// guardEmptyWorkspace creates the workspace and refuses to check out over
// an existing repository, matching the shell's `[ -e .git ]` guard.
func guardEmptyWorkspace(workspace string) error {
	if err := os.MkdirAll(workspace, 0o755); err != nil { //nolint:gosec,mnd // standard workspace perms; dir is the runner-provided checkout target.
		return fmt.Errorf("create workspace %s: %w", workspace, err)
	}

	if _, err := os.Stat(filepath.Join(workspace, ".git")); err == nil {
		return fmt.Errorf("workspace %s already contains a .git: %w", workspace, errs.ErrValidation)
	}

	return nil
}

// isHexSHA matches the shell's ^[0-9a-f]{40,64}$ — sha1 (40) through
// sha256 (64) lowercase hex.
func isHexSHA(ref string) bool {
	if len(ref) < 40 || len(ref) > 64 {
		return false
	}

	for _, r := range ref {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}

	return true
}
