// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	domainci "github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// CheckoutGit is the git operation subset Checkout drives. The adapter's
// *git.Repo (with Dir set to the workspace) satisfies it structurally; the
// port keeps app/ci free of any adapter import, and the credential
// mechanics stay encapsulated in the adapter's Fetch.
type CheckoutGit interface { //nolint:interfacebloat // existing checkout stages plus typed tag/branch existence queries; all belong to this use case.
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
	RemoteTagCommitIfExists(ctx context.Context, remote, tag string, cred runcontext.Credential) (string, bool, error)
	RemoteBranchCommit(ctx context.Context, remote, branch string, cred runcontext.Credential) (string, bool, error)
}

const (
	objectSHA1   = "sha1"
	objectSHA256 = "sha256"
)

// CheckoutInput drives Checkout.
type CheckoutInput struct {
	Repository   string                // "owner/name"
	ServerURL    string                // e.g. https://forgejo.example.com
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
func Checkout(ctx context.Context, newGit func(dir string) CheckoutGit, sink domainci.OutputSink, w io.Writer, in CheckoutInput) (string, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	// Refused before the destination is opened: a missing collaborator
	// used to panic only after the staging directory existed, or after the
	// fetch when the sink was the missing one. The writer is optional.
	if newGit == nil || sink == nil {
		return "", fmt.Errorf("checkout: git factory and output sink are required: %w", errs.ErrUsage)
	}

	// Metadata has now been resolved; only here does an absent format mean SHA-1.
	if in.ObjectFormat == "" {
		in.ObjectFormat = objectSHA1
	}

	if preflightErr := PreflightCheckout(in); preflightErr != nil {
		return "", preflightErr
	}

	staged, err := openCheckoutDestination(in.Workspace)
	if err != nil {
		return "", err
	}

	git := newGit(staged.dir)
	in.Workspace = staged.dir

	sha, err := runCheckout(ctx, git, sink, w, in)

	// Publication is the last thing that happens, and only when everything
	// before it worked.
	if finishErr := staged.finish(err == nil); finishErr != nil {
		return "", errors.Join(err, finishErr)
	}

	return sha, err
}

// runCheckout performs the Git sequence in whichever directory the destination
// resolved to.
func runCheckout(ctx context.Context, git CheckoutGit, sink domainci.OutputSink, w io.Writer, in CheckoutInput) (string, error) { //nolint:varnamelen // writer convention.
	// The command resolves the override-or-provider object format and
	// passes it in; an empty value (no override, forge does not report
	// one) defaults to sha1.
	format := in.ObjectFormat

	if err := git.InitWithObjectFormat(ctx, format); err != nil {
		return "", fmt.Errorf("init %s repository in %s: %w", format, in.Workspace, err)
	}

	remoteURL, _ := checkoutRemoteURL(in.ServerURL, in.Repository)
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

// An absent optional base branch is harmless; lookup or fetch failures are not
// evidence of absence. Supplemental fetches always request full history.
func fetchExtras(ctx context.Context, git CheckoutGit, remoteURL string, in CheckoutInput) error {
	if in.FetchBase != "" {
		_, exists, err := git.RemoteBranchCommit(ctx, "origin", in.FetchBase, in.Token)
		if err != nil {
			return fmt.Errorf("probe base branch: %w", err)
		}

		if exists {
			if err := git.Fetch(ctx, remoteURL, []string{"+refs/heads/" + in.FetchBase + ":refs/heads/" + in.FetchBase}, in.Token, 0); err != nil {
				return fmt.Errorf("fetch base branch: %w", err)
			}
		}
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
func reportCheckout(ctx context.Context, git CheckoutGit, sink domainci.OutputSink, w io.Writer, in CheckoutInput, format string) (string, error) { //nolint:cyclop,varnamelen // canonical HEAD validation precedes sink-first publication and diagnostics.
	sha, err := git.RevParse(ctx, "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve checked-out HEAD: %w", err)
	}

	if !domaingit.ValidCommitSHA(sha) || (format == objectSHA1 && len(sha) != 40) || (format == objectSHA256 && len(sha) != 64) {
		return "", fmt.Errorf("checked-out HEAD does not match the selected object format: %w", errs.ErrValidation)
	}

	key := in.OutputKey
	if key == "" {
		key = "checkout-sha"
	}

	// The machine output is the commit point; diagnostics follow it. A writer
	// error is reported, but cannot undo an already accepted sink value.
	if err := sink.Set(ctx, key, sha); err != nil {
		return "", err
	}

	if w != nil {
		if _, err := fmt.Fprintf(w, "Checked out %s at %s (%s)\n", in.Repository, sha, format); err != nil {
			return "", fmt.Errorf("write checkout summary: %w", err)
		}
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

	_, exists, err := git.RemoteTagCommitIfExists(ctx, "origin", ref, in.Token)
	if err != nil {
		return fmt.Errorf("probe checkout tag: %w", err)
	}

	tagDst := "refs/tags/" + ref
	if exists {
		if fetchErr := git.Fetch(ctx, remoteURL, []string{"+" + tagDst + ":" + tagDst}, in.Token, in.Depth); fetchErr != nil {
			return fetchError(ref, remoteURL, fetchErr)
		}

		return git.CheckoutDetach(ctx, tagDst)
	}

	_, exists, err = git.RemoteBranchCommit(ctx, "origin", ref, in.Token)
	if err != nil {
		return fmt.Errorf("probe checkout branch: %w", err)
	}

	branchDst := "refs/remotes/origin/" + ref
	if exists {
		if err := git.Fetch(ctx, remoteURL, []string{"+refs/heads/" + ref + ":" + branchDst}, in.Token, in.Depth); err != nil {
			return fetchError(ref, remoteURL, err)
		}

		return git.CheckoutDetach(ctx, branchDst)
	}

	return fmt.Errorf("ref %q not found as a tag or branch on %s: %w", ref, remoteURL, errs.ErrValidation)
}

func fetchError(ref, remoteURL string, err error) error {
	return fmt.Errorf("cannot fetch %q from %s (wrong ref, or the server refuses unadvertised SHAs): %w", ref, remoteURL, err)
}

// checkoutDestination is where the Git work actually happens and how it reaches
// the caller's workspace.
//
// A checkout is a long sequence of external Git commands, and any of them can
// fail. Running them straight into the destination means a failure leaves a
// half-populated repository there, which the next run has to reason about. When
// this run is the one creating the destination, that is avoidable: the work goes
// into a sibling staging directory and is renamed into place only once it
// succeeded, so a failure leaves nothing behind and a retry starts clean.
//
// When the destination already exists it belongs to the caller, who may have put
// files there deliberately. Renaming over it would destroy them, so that case
// keeps working in place, and a failure keeps the partial state it always did.
//
// One thing this cannot prevent, only detect. Git takes a pathname, not a
// directory descriptor, so the directory guarded before the run is not provably
// the directory Git writes to: anything with write access to the parent can
// swap the path in between. `identity` is the guarded directory's inode, taken
// at guard time and re-checked before publication. A swap is then a refusal
// with a diagnostic instead of a checkout published into somewhere nobody
// checked. Detecting it after the fact is weaker than preventing it, and it is
// what a pathname-based tool allows.
//
// The comparison is by inode, so it sees a swap — a rename or a symlink
// redirect, which is what a hostile replacement is — and it does NOT reliably
// see a delete-and-recreate at the same path, because the filesystem may hand
// the new directory the inode the old one just freed. Closing that would need
// Git to accept a directory descriptor, which it does not.
type checkoutDestination struct {
	dir      string
	final    string
	staged   bool
	identity os.FileInfo
	rollback func() error
}

// openCheckoutDestination prepares the directory the Git work will run in.
func openCheckoutDestination(workspace string) (checkoutDestination, error) {
	root, err := pathsafe.OpenRoot(workspace)
	if err == nil {
		_ = root.Close()

		if guardErr := guardCheckoutWorkspace(workspace, true); guardErr != nil {
			return checkoutDestination{}, guardErr
		}

		identity, statErr := os.Stat(workspace)
		if statErr != nil {
			return checkoutDestination{}, fmt.Errorf("record checkout workspace identity for %s: %w", workspace, statErr)
		}

		return checkoutDestination{dir: workspace, final: workspace, identity: identity}, nil
	}

	if !errors.Is(err, os.ErrNotExist) {
		return checkoutDestination{}, fmt.Errorf("open checkout workspace %s: %w", workspace, err)
	}

	// The destination's parent may not exist either; creating it is the same
	// component-safe step the in-place path used to do for the whole chain.
	parent, err := pathsafe.MkdirRoot(filepath.Dir(workspace), 0o755)
	if err != nil {
		return checkoutDestination{}, fmt.Errorf("create checkout workspace parent for %s: %w", workspace, err)
	}

	_ = parent.Close()

	staging, err := os.MkdirTemp(parent.Name(), "."+filepath.Base(workspace)+"-staging-")
	if err != nil {
		return checkoutDestination{}, fmt.Errorf("create checkout staging directory beside %s: %w", workspace, err)
	}

	if guardErr := guardCheckoutWorkspace(staging, true); guardErr != nil {
		return checkoutDestination{}, errors.Join(guardErr, os.RemoveAll(staging))
	}

	identity, err := os.Stat(staging)
	if err != nil {
		return checkoutDestination{}, errors.Join(
			fmt.Errorf("record checkout staging identity for %s: %w", staging, err), os.RemoveAll(staging))
	}

	return checkoutDestination{
		dir: staging, final: workspace, staged: true, identity: identity,
		rollback: func() error { return os.RemoveAll(staging) },
	}, nil
}

// stillTheGuardedDirectory reports whether the path this run has been writing
// to is the same directory that was guarded before it started.
func (d checkoutDestination) stillTheGuardedDirectory() error {
	if d.identity == nil {
		return nil
	}

	current, err := os.Lstat(d.dir)
	if err != nil {
		return fmt.Errorf("re-check checkout destination %s: %w", d.dir, err)
	}

	if !os.SameFile(d.identity, current) {
		return fmt.Errorf("checkout destination %s was replaced after it was checked; refusing to publish work from an unverified directory: %w",
			d.dir, errs.ErrValidation)
	}

	return nil
}

// finish publishes a staged checkout, or removes it when the run failed. The
// rename is the publication: until it happens the destination does not exist,
// and after it the checkout is complete.
func (d checkoutDestination) finish(ok bool) error {
	// Checked on both paths, and before the rollback: a swapped path must not
	// be published, and must not be deleted either.
	if identityErr := d.stillTheGuardedDirectory(); identityErr != nil {
		return identityErr
	}

	if !d.staged {
		return nil
	}

	if !ok {
		if err := d.rollback(); err != nil {
			return fmt.Errorf("remove failed checkout staging directory: %w", err)
		}

		return nil
	}

	if err := os.Rename(d.dir, d.final); err != nil {
		return errors.Join(
			fmt.Errorf("publish checkout to %s: %w", d.final, err),
			d.rollback(),
		)
	}

	return nil
}

// PreflightCheckout checks all locally decidable input and workspace constraints
// without creating directories or contacting Git, metadata providers or sinks.
// An empty ObjectFormat defers only SHA-length agreement until metadata resolves.
// Checkout repeats this check before mutation; it does not promise rollback or
// hold a descriptor across pathname-based external Git operations.
func PreflightCheckout(in CheckoutInput) error {
	if err := validateCheckoutInput(in); err != nil {
		return err
	}

	return guardCheckoutWorkspace(in.Workspace, false)
}

func validateCheckoutInput(in CheckoutInput) error { //nolint:cyclop // flat local guards precede every filesystem, Git and credential effect.
	if in.Repository == "" {
		return fmt.Errorf("repository (owner/name) is required: %w", errs.ErrUsage)
	}

	if in.ServerURL == "" {
		return fmt.Errorf("server-url is required: %w", errs.ErrUsage)
	}

	if err := validCheckoutWorkspace(in.Workspace); err != nil {
		return err
	}

	if _, err := checkoutRemoteURL(in.ServerURL, in.Repository); err != nil {
		return err
	}

	format := in.ObjectFormat
	if format != "" && format != objectSHA1 && format != objectSHA256 {
		return fmt.Errorf("unsupported object format: %w", errs.ErrUsage)
	}

	if isHexSHA(in.Ref) && ((format == objectSHA1 && len(in.Ref) != 40) || (format == objectSHA256 && len(in.Ref) != 64)) {
		return fmt.Errorf("ref and object format disagree: %w", errs.ErrValidation)
	}

	if in.Depth < 0 || (in.OutputKey != "" && !validOutputKey(in.OutputKey)) {
		return fmt.Errorf("invalid checkout depth or output key: %w", errs.ErrUsage)
	}

	if in.FetchBase != "" && (strings.HasPrefix(in.FetchBase, "-") || !domaingit.ValidRefName("refs/heads/"+in.FetchBase)) {
		return fmt.Errorf("invalid base branch: %w", errs.ErrValidation)
	}

	seen := map[string]bool{}
	for _, cone := range in.Sparse {
		if !pathsafe.Relative(cone) || cone == "." || filepath.ToSlash(filepath.Clean(cone)) != cone || strings.HasPrefix(cone, "-") || strings.ContainsAny(cone, "*?[]\\") || !utf8.ValidString(cone) || strings.ContainsFunc(cone, unicode.IsControl) || seen[cone] {
			return fmt.Errorf("sparse cones must be unique literal relative directories: %w", errs.ErrValidation)
		}

		seen[cone] = true
	}

	return validCheckoutRef(in.Ref)
}

// validCheckoutWorkspace keeps the destination spellable and component-safe
// before any root is opened. Cleaning the path first would erase a parent step
// whose linked component external Git would still traverse.
func validCheckoutWorkspace(workspace string) error {
	if strings.TrimSpace(workspace) == "" || !utf8.ValidString(workspace) || strings.ContainsFunc(workspace, unicode.IsControl) {
		return fmt.Errorf("a valid workspace path is required: %w", errs.ErrUsage)
	}

	for _, component := range strings.Split(filepath.ToSlash(workspace), "/") {
		if component == ".." {
			return fmt.Errorf("checkout workspace must not contain parent-directory steps: %w", errs.ErrValidation)
		}
	}

	return nil
}

// validCheckoutRef accepts literal Git names or canonical full object IDs,
// excluding options, globs, revision expressions and terminal control text.
func validCheckoutRef(ref string) error {
	switch {
	case ref == "":
		return fmt.Errorf("ref is required: %w", errs.ErrUsage)
	case strings.HasPrefix(ref, "-"):
		return fmt.Errorf("unsafe ref %q (leading dash): %w", ref, errs.ErrValidation)
	case strings.ContainsAny(ref, "\n\r"):
		return fmt.Errorf("unsafe ref (embedded newline): %w", errs.ErrValidation)
	}

	full := ref
	if !strings.HasPrefix(full, "refs/") {
		full = "refs/heads/" + full
	}

	if !isHexSHA(ref) && !domaingit.ValidRefName(full) {
		return fmt.Errorf("ref must be a literal Git reference or full object ID: %w", errs.ErrValidation)
	}

	return nil
}

// Missing components are harmless during preflight, but links, non-directories
// and inspection errors are not absence. Creation rechecks components and .git.
func guardCheckoutWorkspace(workspace string, create bool) error {
	open := pathsafe.OpenRoot
	if create {
		open = func(name string) (*os.Root, error) { return pathsafe.MkdirRoot(name, 0o755) }
	}

	root, err := open(workspace)
	if !create && errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("open checkout workspace %s: %w", workspace, err)
	}

	defer func() { _ = root.Close() }()

	if _, err := root.Lstat(".git"); err == nil {
		return fmt.Errorf("workspace %s already contains a .git: %w", workspace, errs.ErrValidation)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect checkout .git: %w", err)
	}

	return nil
}

// isHexSHA accepts exactly SHA-1 or SHA-256, not the lengths in between.
func isHexSHA(ref string) bool { return domaingit.ValidCommitSHA(ref) }

func checkoutRemoteURL(server, repository string) (string, error) { //nolint:cyclop // explicit URL authority, suffix and path-segment guards avoid ambiguous source coordinates.
	parsed, err := url.Parse(server)
	if err != nil {
		return "", fmt.Errorf("invalid checkout server URL: %w", errs.ErrUsage)
	}

	if !utf8.ValidString(server) || strings.ContainsFunc(server, unicode.IsControl) {
		return "", fmt.Errorf("invalid checkout server text: %w", errs.ErrUsage)
	}

	if (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" || strings.ContainsAny(server, "?#\\") {
		return "", fmt.Errorf("checkout server must be an HTTP(S) base URL without credentials, escapes, query or fragment: %w", errs.ErrUsage)
	}

	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return "", fmt.Errorf("invalid checkout server port: %w", errs.ErrUsage)
		}
	} else if strings.HasSuffix(parsed.Host, ":") {
		return "", fmt.Errorf("empty checkout server port: %w", errs.ErrUsage)
	}

	if len(strings.Split(repository, "/")) < 2 || strings.HasSuffix(repository, ".git") {
		return "", fmt.Errorf("checkout repository must be owner/name without .git: %w", errs.ErrUsage)
	}

	for _, path := range []string{repository, strings.Trim(parsed.Path, "/")} {
		if path == "" {
			continue
		}

		for _, part := range strings.Split(path, "/") {
			if part == "" || part == "." || part == ".." || url.PathEscape(part) != part || strings.ContainsAny(part, "\\:") {
				return "", fmt.Errorf("invalid checkout repository or server path: %w", errs.ErrUsage)
			}
		}
	}

	return strings.TrimRight(server, "/") + "/" + repository + ".git", nil
}
