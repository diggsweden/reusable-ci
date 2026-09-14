// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"

	adapteropenpgp "github.com/diggsweden/reusable-ci/v3/internal/pgp"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

// RevParse resolves ref to a SHA. HEAD uses native Git to match the mutation
// context. Other lookups use go-git when available, falling back for revisions
// (e.g. `HEAD~5^2`) the in-process resolver doesn't support natively.
func (r *Repo) RevParse(ctx context.Context, ref string) (string, error) {
	// Release guards/capture must read the same native checkout as git commit,
	// including GIT_DIR/worktree semantics, not a tag that happens to be named HEAD.
	if ref == "HEAD" {
		return r.Run(ctx, headCommitArgs()...)
	}
	// An annotated tag resolves to the TAG OBJECT, not the commit it points
	// at: that is what `git rev-parse refs/tags/v1.2.3` returns, and only an
	// explicit peel suffix (^{commit}, ^{}) asks for the commit.
	//
	// go-git's ResolveRevision always peels, so consulting it first made this
	// adapter disagree with the command it stands in for — silently, and only
	// for annotated tags. Callers comparing a local tag object against a
	// remote one then compared a commit against a tag object and could never
	// match.
	//
	// Every caller in this repository that wants the commit already spells
	// ^{commit} (validate/tags.go, prerequisites.go, changelogrender.go,
	// pinreachability.go), exactly as they would against git itself. Those
	// spellings carry a suffix, so they do not resolve as a tag name here and
	// fall through to ResolveRevision, which peels them correctly.
	if tagObj, found, tagErr := r.lookupTagObject(ref); tagErr == nil && found {
		return tagObj.Hash.String(), nil
	}

	repo, err := r.openGoGit()
	if err == nil {
		hash, err := repo.ResolveRevision(plumbing.Revision(ref))
		if err == nil {
			return hash.String(), nil
		}
	}

	return r.Run(ctx, "-c", hooksDisabledConfig, "rev-parse", ref)
}

func headCommitArgs() []string {
	return []string{"--no-replace-objects", "-c", hooksDisabledConfig, "rev-parse", "--verify", "HEAD^{commit}"} //nolint:goconst // Keep the object-resolution policy visible in native argv.
}

// CommitParents reads the captured object's raw parents, ignoring replacement
// refs and without traversal (so shallow boundaries do not hide its parent).
func (r *Repo) CommitParents(ctx context.Context, commitSHA string) ([]string, error) {
	args, err := commitParentsArgs(commitSHA)
	if err != nil {
		return nil, err
	}

	body, err := r.Run(ctx, args...)
	if err != nil {
		return nil, err
	}

	return commitParentsFromObject(body)
}

func commitParentsArgs(commitSHA string) ([]string, error) {
	if !domaingit.ValidCommitSHA(commitSHA) {
		return nil, fmt.Errorf("parent inspection requires a full commit OID: %w", errs.ErrUsage)
	}

	return []string{"--no-replace-objects", "-c", hooksDisabledConfig, "cat-file", "commit", commitSHA}, nil //nolint:goconst // Native Git object type, not application vocabulary.
}

func commitParentsFromObject(body string) ([]string, error) {
	headers, _, found := strings.Cut(body, "\n\n")
	if !found {
		return nil, fmt.Errorf("commit object has no header separator: %w", errs.ErrValidation)
	}

	var parents []string

	for _, line := range strings.Split(headers, "\n") {
		if parent, ok := strings.CutPrefix(line, "parent "); ok {
			if !domaingit.ValidCommitSHA(parent) {
				return nil, fmt.Errorf("commit object has a malformed parent: %w", errs.ErrValidation)
			}

			parents = append(parents, parent)
		}
	}

	return parents, nil
}

// DescribeLatestTag returns the most recent tag reachable from HEAD,
// matching `git describe --tags --abbrev=0`.
func (r *Repo) DescribeLatestTag(ctx context.Context) (string, error) {
	return r.Run(ctx, "describe", "--tags", "--abbrev=0")
}

// TagSHA returns the commit SHA the given tag points to.
//
// Annotated tags hold their target commit; lightweight tags ARE the
// commit. ResolveRevision via go-git handles both via the
// "tag^{commit}" suffix, matching `git rev-parse tag^{commit}`
// semantics.
func (r *Repo) TagSHA(ctx context.Context, tag string) (string, error) {
	repo, err := r.openGoGit()
	if err == nil {
		hash, err := repo.ResolveRevision(plumbing.Revision(tag + "^{commit}"))
		if err == nil {
			return hash.String(), nil
		}
	}

	return r.Run(ctx, "rev-list", "-n", "1", tag)
}

// ShortSHA returns `git rev-parse --short=<n> <ref>`.
func (r *Repo) ShortSHA(ctx context.Context, ref string, n int) (string, error) {
	return r.Run(ctx, "rev-parse", fmt.Sprintf("--short=%d", n), ref)
}

// StatusPorcelain returns `git status --porcelain -- <pathspec>`.
func (r *Repo) StatusPorcelain(ctx context.Context, pathspec string) (string, error) {
	out, err := r.Run(ctx, "status", "--porcelain", "--", pathspec)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(out), nil
}

// RemoteTagCommit resolves a tag to the commit it points to on the
// remote (`git ls-remote`), peeling annotated tags. Querying the remote
// — not the local checkout — is the point: it re-verifies the published
// tag at the trust boundary. Returns ErrValidation when the tag is
// absent on the remote.
func (r *Repo) RemoteTagCommit(ctx context.Context, repoURL, tag string, cred runcontext.Credential) (string, error) {
	env, err := r.remoteAuthEnv(ctx, repoURL, cred)
	if err != nil {
		return "", err
	}

	out, err := r.runEnvOutput(ctx, env, remoteTagQueryArgs(repoURL, tag, true)...)
	if err != nil {
		return "", err
	}

	return remoteTagCommitFromOutput(out, repoURL, tag)
}

// RemoteTagCommitIfExists resolves a remote tag to the commit it points to,
// returning exists=false when the remote has no matching tag.
func (r *Repo) RemoteTagCommitIfExists(ctx context.Context, remote, tag string, cred runcontext.Credential) (string, bool, error) {
	if remote == "" {
		remote = defaultRemote
	}

	env, err := r.remoteAuthEnv(ctx, remote, cred)
	if err != nil {
		return "", false, err
	}

	out, err := r.runEnvOutput(ctx, env, remoteTagQueryArgs(remote, tag, true)...)
	if err != nil {
		return "", false, err
	}

	commit, err := remoteTagCommitFromOutput(out, remote, tag)
	if err != nil {
		if errors.Is(err, errs.ErrValidation) {
			return "", false, nil
		}

		return "", false, err
	}

	return commit, true, nil
}

func remoteTagQueryArgs(repository, tag string, peel bool) []string {
	args := []string{"ls-remote", "--tags", "--", repository} //nolint:goconst // Keep native argv and the original repository argument visible.
	if peel {
		args = append(args, refsTagsPrefix+tag+"^{}")
	}

	return append(args, refsTagsPrefix+tag)
}

// RemoteBranchCommit resolves refs/heads/<branch> on a named remote. Absence
// is reported separately so release preparation can distinguish a deleted
// branch from a transport or authentication failure.
func (r *Repo) RemoteBranchCommit(ctx context.Context, remote, branch string, cred runcontext.Credential) (string, bool, error) {
	if remote == "" {
		remote = defaultRemote
	}

	env, err := r.remoteAuthEnv(ctx, remote, cred)
	if err != nil {
		return "", false, err
	}

	ref := "refs/heads/" + branch

	out, err := r.runEnvOutput(ctx, env, "-c", hooksDisabledConfig, "ls-remote", "--heads", remote, ref)
	if err != nil {
		return "", false, err
	}

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == ref {
			return fields[0], true, nil
		}
	}

	return "", false, nil
}

func remoteTagCommitFromOutput(out, repoURL, tag string) (string, error) {
	plainRef, peeledRef := refsTagsPrefix+tag, refsTagsPrefix+tag+"^{}"

	ids, err := remoteRefIDs(out, plainRef, peeledRef)
	if err != nil {
		return "", err
	}

	if peeled := ids[peeledRef]; peeled != "" {
		return peeled, nil
	}

	if plain := ids[plainRef]; plain != "" {
		return plain, nil
	}

	return "", fmt.Errorf("remote tag %q not found on %s: %w", tag, repoURL, errs.ErrValidation)
}

// remoteRefIDs collects the object IDs `git ls-remote` reported for the wanted
// refs, ignoring every other ref.
//
// Both refusals exist because this output decides which commit a release signs
// and neither case has a right answer to pick. An ID that is not a full object
// hash used to be returned as the answer -- including one shaped like a git
// option -- and a ref reported twice with different IDs silently resolved to
// whichever line the scan kept, which was the last line for commits and the
// first for tag objects, so the two readers of one response could disagree.
//
// The class is deliberately ErrMalformedInput, not ErrValidation: the
// *IfExists callers read ErrValidation as "the tag is not there", and a remote
// answering nonsense must not be mistaken for a tag that can be created.
func remoteRefIDs(out string, wanted ...string) (map[string]string, error) {
	ids := make(map[string]string, len(wanted))

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !slices.Contains(wanted, fields[1]) {
			continue
		}

		ref, id := fields[1], fields[0]

		// Object IDs share the commit-hash shape: 40 hex for SHA-1
		// repositories, 64 for SHA-256.
		if !domaingit.ValidCommitSHA(id) {
			return nil, fmt.Errorf("remote reported a malformed object id for %s: %q: %w", ref, id, errs.ErrMalformedInput)
		}

		if prior, seen := ids[ref]; seen && prior != id {
			return nil, fmt.Errorf("remote reported %s twice with different object ids (%s, %s): %w", ref, prior, id, errs.ErrMalformedInput)
		}

		ids[ref] = id
	}

	return ids, nil
}

// CommitSubject returns `git log -1 --format=%s <commit>`.
func (r *Repo) CommitSubject(ctx context.Context, commit string) (string, error) {
	return r.Run(ctx, "log", "-1", "--format=%s", commit)
}

// CommitUnixTime returns `git log -1 --format=%ct <ref>`.
func (r *Repo) CommitUnixTime(ctx context.Context, ref string) (string, error) {
	return r.Run(ctx, "log", "-1", "--format=%ct", ref)
}

// RecentLogOneline returns `git log <ref> --oneline -<limit>`.
func (r *Repo) RecentLogOneline(ctx context.Context, ref string, limit int) (string, error) {
	if limit <= 0 {
		limit = 20
	}

	return r.Run(ctx, "log", ref, "--oneline", fmt.Sprintf("-%d", limit))
}

// RemoteVersionTags returns the `v*` tags present on the remote
// (`git ls-remote --tags --refs`). The git-side `v*` glob is coarse IO
// narrowing only; deciding which of these are valid release tags (and
// which is highest) is the domain's job — see domain/version.
func (r *Repo) RemoteVersionTags(ctx context.Context, repoURL string, cred runcontext.Credential) ([]string, error) {
	env, err := r.remoteAuthEnv(ctx, repoURL, cred)
	if err != nil {
		return nil, err
	}

	out, err := r.runEnvOutput(ctx, env, "ls-remote", "--tags", "--refs", "--", repoURL, refsTagsPrefix+"v*")
	if err != nil {
		return nil, err
	}

	var tags []string

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		tags = append(tags, strings.TrimPrefix(fields[1], refsTagsPrefix))
	}

	return tags, nil
}

// FetchTagFromRemote fetches exactly one tag object into the local refs/tags
// namespace. It mirrors forgejo-ci's release-request guard: no implicit tag
// following, no hooks, and no ref mutation beyond the requested tag.
func (r *Repo) FetchTagFromRemote(ctx context.Context, remote, tag string) error {
	if remote == "" {
		remote = defaultRemote
	}

	ref := refsTagsPrefix + tag
	_, err := r.Run(ctx, "-c", hooksDisabledConfig, "fetch", "--no-tags", remote, ref+":"+ref)

	return err
}

// RemoteTagObject returns the unpeeled object id published at refs/tags/<tag>
// on remote. For annotated tags this is the tag-object id, not the target
// commit id; release-request validation compares this value with the local tag
// object to catch stale or locally moved authorisation tags.
func (r *Repo) RemoteTagObject(ctx context.Context, remote, tag string) (string, error) {
	if remote == "" {
		remote = defaultRemote
	}

	out, err := r.Run(ctx, "-c", hooksDisabledConfig, "ls-remote", "--tags", remote, refsTagsPrefix+tag)
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == refsTagsPrefix+tag {
			return fields[0], nil
		}
	}

	return "", fmt.Errorf("remote tag %q not found on %s: %w", tag, remote, errs.ErrValidation)
}

// RemoteTagObjectAtURL returns the unpeeled object id published at
// refs/tags/<tag> on repoURL, authenticating with cred when supplied.
func (r *Repo) RemoteTagObjectAtURL(ctx context.Context, repoURL, tag string, cred runcontext.Credential) (string, error) {
	env, err := r.remoteAuthEnv(ctx, repoURL, cred)
	if err != nil {
		return "", err
	}

	out, err := r.runEnvOutput(ctx, env, remoteTagQueryArgs(repoURL, tag, false)...)
	if err != nil {
		return "", err
	}

	return remoteTagObjectFromOutput(out, repoURL, tag)
}

// RemoteTagObjectIfExists is the named-remote counterpart used by create-once
// tag recovery. Absence is a boolean; auth and transport errors still surface.
func (r *Repo) RemoteTagObjectIfExists(ctx context.Context, remote, tag string, cred runcontext.Credential) (string, bool, error) {
	if remote == "" {
		remote = defaultRemote
	}

	env, err := r.remoteAuthEnv(ctx, remote, cred)
	if err != nil {
		return "", false, err
	}

	out, err := r.runEnvOutput(ctx, env, remoteTagQueryArgs(remote, tag, false)...)
	if err != nil {
		return "", false, err
	}

	if strings.TrimSpace(out) == "" {
		return "", false, nil
	}

	object, err := remoteTagObjectFromOutput(out, remote, tag)
	if err != nil {
		return "", false, err
	}

	return object, true, nil
}

func remoteTagObjectFromOutput(out, repoURL, tag string) (string, error) {
	ref := refsTagsPrefix + tag

	ids, err := remoteRefIDs(out, ref)
	if err != nil {
		return "", err
	}

	if id := ids[ref]; id != "" {
		return id, nil
	}

	return "", fmt.Errorf("remote tag %q not found on %s: %w", tag, repoURL, errs.ErrValidation)
}

// RemoteTagExists reports whether refs/tags/<tag> is present on remote.
// It intentionally avoids --exit-code so absence is a boolean, not a wrapped
// git failure, while transport/auth failures still surface as errors.
func (r *Repo) RemoteTagExists(ctx context.Context, remote, tag string) (bool, error) {
	if remote == "" {
		remote = defaultRemote
	}

	out, err := r.Run(ctx, "-c", hooksDisabledConfig, "ls-remote", "--tags", remote, refsTagsPrefix+tag)
	if err != nil {
		return false, err
	}

	return strings.TrimSpace(out) != "", nil
}

// VerifyConfiguredTagSignature asks git to verify an annotated tag with the
// configured GPG keyring or SSH allowed-signers file. Release preparation sets
// that configuration before creating or reusing a final tag.
func (r *Repo) VerifyConfiguredTagSignature(ctx context.Context, tag string) error {
	_, err := r.Run(ctx, "-c", hooksDisabledConfig, "verify-tag", tag)

	return err
}

// refsTagsPrefix is the git ref namespace for tags.
const refsTagsPrefix = "refs/tags/"

// ListTags returns `git tag -l <pattern>` as a slice (one tag per
// line). Empty slice when nothing matches.
func (r *Repo) ListTags(ctx context.Context, pattern string) ([]string, error) {
	args := []string{"tag", "-l"} //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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

// TagExists reports whether a local tag with this exact name exists.
// `git tag -l <exact>` lists the tag only when it exists, so a non-empty
// result is an exact-match existence check. Used by the create-once
// release-tag path to refuse clobbering/moving an existing tag.
func (r *Repo) TagExists(ctx context.Context, tag string) (bool, error) {
	out, err := r.Run(ctx, "-c", hooksDisabledConfig, "tag", "-l", tag)
	if err != nil {
		return false, err
	}

	return strings.TrimSpace(out) != "", nil
}

// TagsPointingAt returns tag names whose target commit is commit.
// Annotated and lightweight tags are both included, matching the CLI's
// `git tag --points-at` behaviour.
func (r *Repo) TagsPointingAt(ctx context.Context, commit string) ([]string, error) {
	repo, err := r.openGoGit()
	if err != nil {
		return r.runTagsPointingAt(ctx, commit)
	}

	target, err := repo.ResolveRevision(plumbing.Revision(commit))
	if err != nil {
		return r.runTagsPointingAt(ctx, commit)
	}

	iter, err := repo.Tags()
	if err != nil {
		return r.runTagsPointingAt(ctx, commit)
	}

	var out []string

	_ = iter.ForEach(func(ref *plumbing.Reference) error {
		// Resolve the tag ref to a commit. Annotated tags need
		// dereferencing through the tag object; lightweight tags are
		// already a commit ref.
		hash := ref.Hash()
		if tagObj, err := repo.TagObject(hash); err == nil {
			hash = tagObj.Target
		}

		if hash == *target {
			out = append(out, ref.Name().Short())
		}

		return nil
	})

	if len(out) == 0 {
		return nil, nil
	}

	return out, nil
}

// runTagsPointingAt is the subprocess fallback for TagsPointingAt.
func (r *Repo) runTagsPointingAt(ctx context.Context, commit string) ([]string, error) {
	out, err := r.Run(ctx, "tag", "--points-at", commit)
	if err != nil {
		return nil, err
	}

	if out == "" {
		return nil, nil
	}

	return strings.Split(out, "\n"), nil
}

// IsAncestor reports whether ancestor is in the history of descendant.
// In-process via go-git; falls back to `git merge-base --is-ancestor`
// when go-git cannot open the repository — it rejects sha256 repos
// ("does not support extension: objectformat"), which broke the
// same-version release recovery path on a sha256 consumer.
func (r *Repo) IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	repo, err := r.openGoGit()
	if err != nil {
		return r.runIsAncestor(ctx, ancestor, descendant)
	}

	anc, err := repo.ResolveRevision(plumbing.Revision(ancestor))
	if err != nil {
		return false, fmt.Errorf("resolve ancestor %q: %w", ancestor, err)
	}

	desc, err := repo.ResolveRevision(plumbing.Revision(descendant))
	if err != nil {
		return false, fmt.Errorf("resolve descendant %q: %w", descendant, err)
	}

	ancCommit, err := repo.CommitObject(*anc)
	if err != nil {
		return false, fmt.Errorf("read commit %s: %w", anc, err)
	}

	descCommit, err := repo.CommitObject(*desc)
	if err != nil {
		return false, fmt.Errorf("read commit %s: %w", desc, err)
	}

	return ancCommit.IsAncestor(descCommit)
}

// runIsAncestor is the subprocess fallback for IsAncestor. We don't use
// Run() because merge-base --is-ancestor answers through its exit
// status: 0 = ancestor, 1 = not an ancestor, anything else is a real
// failure.
func (r *Repo) runIsAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	bin := r.GitBin
	if bin == "" {
		bin = "git" //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}

	cmd := safeexec.Command(ctx, bin, "merge-base", "--is-ancestor", ancestor, descendant)
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

		return false, fmt.Errorf("git merge-base --is-ancestor: exit %d\n%s: %w", exitErr.ExitCode(), exitErr.Stderr, errs.ErrValidation)
	}

	return false, safeexec.WrapError(err, bin, "merge-base")
}

// CatFileType returns the object type for ref. For tag refs:
// "tag" = annotated, "commit" = lightweight. In-process via go-git
// when available.
func (r *Repo) CatFileType(ctx context.Context, ref string) (string, error) {
	if _, found, err := r.lookupTagObject(ref); err == nil && found {
		return "tag", nil
	}
	// Resolve as a ref → check what it points at.
	if objType, ok := r.lookupRefObjectType(ref); ok {
		return objType, nil
	}

	return r.Run(ctx, "cat-file", "-t", ref)
}

// lookupRefObjectType resolves ref in-process via go-git and reports
// whether it's a commit/blob/tree. Returns ok=false on any resolution
// failure so the caller can fall back to shelling out.
func (r *Repo) lookupRefObjectType(ref string) (string, bool) {
	repo, err := r.openGoGit()
	if err != nil {
		return "", false
	}

	hash, rerr := repo.ResolveRevision(plumbing.Revision(ref))
	if rerr != nil {
		return "", false
	}

	if _, oerr := repo.CommitObject(*hash); oerr == nil {
		return "commit", true
	}

	if _, oerr := repo.BlobObject(*hash); oerr == nil {
		return "blob", true
	}

	if _, oerr := repo.TreeObject(*hash); oerr == nil {
		return "tree", true
	}

	return "", false
}

// CatFileTag returns the raw object body of an annotated tag.
// In-process via go-git; reconstructs the canonical tag-object body
// (object/type/tag/tagger header lines + blank line + message + PGP
// signature) so downstream signature-detection parsers see the same
// shape as `git cat-file tag <tag>` produced.
func (r *Repo) CatFileTag(ctx context.Context, tag string) (string, error) {
	tagObj, found, err := r.lookupTagObject(tag)
	if err == nil && found {
		return renderTagObject(tagObj), nil
	}

	return r.Run(ctx, "cat-file", "tag", tag)
}

// VerifyTagSignature verifies the PGP signature on an annotated tag
// against an armored public-key ring, in-process via go-git +
// go-crypto/openpgp. Replaces `git tag -v` (which spawned `gpg
// --verify` against a disk keyring that had to be populated first via
// `gpg --import`).
//
// Returns:
//   - signer: the signing entity's primary User ID ("Name <email>"),
//     empty when no identity was attached to the key.
//   - ok: true when the signature verifies; false when verification
//     fails (wrong key, corrupted signature, etc.).
//   - err: non-nil only for hard structural failures — tag not found,
//     not annotated, no signature attached, malformed keyring. A
//     failed verification with intact inputs returns ok=false, err=nil.
//
// armoredKeyring must contain at least the public half of the
// signer's key. An empty keyring returns ok=false, err=nil so callers
// can render the existing "verification requires signer's public key"
// hint.
func (r *Repo) VerifyTagSignature(_ context.Context, tag string, armoredKeyring []byte) (string, string, bool, error) { //nolint:gocritic // wants tuple return for parallel signer+fingerprint reporting.
	if len(armoredKeyring) == 0 {
		return "", "", false, nil
	}

	keys, err := adapteropenpgp.PrimaryFingerprints(armoredKeyring)
	if err != nil || len(keys) == 0 {
		return "", "", false, fmt.Errorf("parse tag verification keyring: %w", errs.ErrMalformedInput)
	}

	tagObj, found, err := r.lookupTagObject(tag)
	if err != nil {
		return "", "", false, fmt.Errorf("read tag %q: %w", tag, err)
	}

	if !found {
		return "", "", false, fmt.Errorf("tag %q is not annotated: %w", tag, errs.ErrValidation)
	}

	if tagObj.PGPSignature == "" {
		return "", "", false, nil
	}

	entity := verifyTagAgainstAnyBlock(tagObj, armoredKeyring)
	if entity == nil {
		// Contract: report (signer, fingerprint, verified). A failed
		// verify is not a function-level error — it's "no Good signer,"
		// ok=false.
		return "", "", false, nil
	}

	fingerprint := strings.ToUpper(hex.EncodeToString(entity.PrimaryKey.Fingerprint))

	for _, ident := range entity.Identities {
		if signer := identityDisplayString(ident); signer != "" {
			return signer, fingerprint, true, nil
		}
	}

	return "", fingerprint, true, nil
}

// VerifyTagSSHAgainstAllowedSigners runs `git tag -v` with gpg.format=ssh and
// gpg.ssh.allowedSignersFile pinned to allowedSignersPath. Git
// invokes ssh-keygen -Y verify internally; exit 0 means the tag is
// signed AND the signer's public key appears in the allowed_signers
// file with the right principal/namespace.
//
// Returns:
//   - ok=true: signed by an allow-listed key.
//   - ok=false: signed but signer not in allowed_signers (denial).
//   - err non-nil: the tag isn't signed, isn't annotated, the file is
//     missing, or git itself errored — distinguished by combined-output
//     parsing. ErrPermissionDenied wraps the "not in allowed_signers"
//     case so the prerequisites orchestrator can map it to exit 77.
func (r *Repo) VerifyTagSSHAgainstAllowedSigners(ctx context.Context, tag, allowedSignersPath string) (bool, string, error) {
	out, err := r.Run(ctx,
		"-c", hooksDisabledConfig,
		"-c", "gpg.format=ssh",
		"-c", "gpg.ssh.allowedSignersFile="+allowedSignersPath,
		"tag", "-v", tag)
	if err == nil {
		return true, out, nil
	}

	// On non-zero exit, the wrapped error carries git's stderr appended
	// after the safeexec wrap. "No principal matched." is the canonical
	// ssh-keygen -Y verify message when the signature is valid but the
	// signer isn't in allowed_signers — distinct from "no signature".
	msg := err.Error()
	if strings.Contains(msg, "Unable to open allowed keys file") || strings.Contains(msg, "No such file or directory") {
		return false, msg, fmt.Errorf("read SSH allowed_signers file for tag %q: %w: %w", tag, err, errs.ErrMissingInput)
	}

	if strings.Contains(msg, "No principal matched") {
		return false, msg, fmt.Errorf("tag %q signature is valid but signer is not in allowed_signers: %w", tag, errs.ErrPermissionDenied)
	}

	return false, msg, err
}

// identityDisplayString renders an openpgp.Identity as "name <email>",
// falling back to whichever of name/email is non-empty. Returns "" when
// the identity has no UserId or both fields are blank.
func identityDisplayString(ident *openpgp.Identity) string {
	if ident == nil || ident.UserId == nil {
		return ""
	}

	name := strings.TrimSpace(ident.UserId.Name)
	email := strings.TrimSpace(ident.UserId.Email)

	switch {
	case name != "" && email != "":
		return name + " <" + email + ">"
	case name != "":
		return name
	case email != "":
		return email
	}

	return ""
}

// TaggerInfo returns the tagger metadata for refs/tags/<tag> as
// (name+email, ISO8601 date). Both fields are best-effort: missing
// values (lightweight tags) come back as empty strings.
//
// In-process via go-git. Replaces two `git for-each-ref` subprocesses
// with a single tag-object read.
func (r *Repo) TaggerInfo(ctx context.Context, tag string) (domaingit.TaggerInfo, error) {
	tagObj, found, err := r.lookupTagObject(tag)
	if err == nil {
		if !found {
			return domaingit.TaggerInfo{}, nil
		}

		who := fmt.Sprintf("%s <%s>", tagObj.Tagger.Name, tagObj.Tagger.Email)
		date := tagObj.Tagger.When.Format("2006-01-02 15:04:05 -0700")

		return domaingit.TaggerInfo{Tagger: who, Date: date}, nil
	}

	tagger, err := r.Run(ctx, "for-each-ref", refsTagsPrefix+tag,
		"--format=%(taggername) <%(taggeremail)>")
	if err != nil {
		return domaingit.TaggerInfo{}, err
	}

	date, err := r.Run(ctx, "for-each-ref", refsTagsPrefix+tag,
		"--format=%(taggerdate:iso8601)")
	if err != nil {
		return domaingit.TaggerInfo{}, err
	}

	if strings.TrimSpace(tagger) == "<>" {
		tagger = ""
	}

	return domaingit.TaggerInfo{Tagger: tagger, Date: date}, nil
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

// TagMessage returns the message body of an annotated tag. In-process
// via go-git when available — no subprocess and no fragile parsing of
// `git tag -l -n999`'s leading-column padding.
func (r *Repo) TagMessage(ctx context.Context, tag string) (string, error) {
	tagObj, found, err := r.lookupTagObject(tag)
	if err == nil && found {
		// go-git's Message includes the PGP signature trailer when
		// present; strip it so callers see only the human-authored body.
		msg := tagObj.Message
		if idx := strings.Index(msg, "-----BEGIN PGP SIGNATURE-----"); idx != -1 {
			msg = strings.TrimRight(msg[:idx], "\n")
		}

		return strings.TrimRight(msg, "\n"), nil
	}

	out, err := r.Run(ctx, "tag", "-l", "-n999", tag)
	if err != nil {
		return "", err
	}
	// Format: "<tag> <message line 1>\n    <message line 2>\n..."
	// Strip the first whitespace-separated column on each line.
	lines := strings.Split(out, "\n")

	stripped := make([]string, 0, len(lines))
	for _, line := range lines {
		idx := strings.Index(line, " ")
		if idx == -1 {
			stripped = append(stripped, "")

			continue
		}

		stripped = append(stripped, strings.TrimLeft(line[idx:], " "))
	}

	return strings.Join(stripped, "\n"), nil
}

// lookupTagObject resolves tag to a refs/tags/<tag> ref and returns
// the annotated tag object. found=false when the ref exists but
// points at a lightweight tag (a commit, not a tag object).
// verifyTagAgainstAnyBlock tries each armor block in a concatenated bundle and
// returns the entity of the first that verifies the tag, or nil.
//
// go-git's Verify hands the keyring to openpgp.ReadArmoredKeyRing, which
// decodes exactly ONE block, so passing a bundle silently verifies against only
// its first key. That is the key-rotation case: docs/verification.md tells
// operators to add a key with `>>`, so during a rotation the incoming key is
// the appended block — and a tag signed with it was reported unverified, then
// refused with EX_NOPERM. Trying each block restores the documented contract
// ("one or more blocks concatenated") without widening it: a key still has to
// be committed to the bundle to be used at all.
func verifyTagAgainstAnyBlock(tagObj *object.Tag, armoredKeyring []byte) *openpgp.Entity {
	for _, block := range adapteropenpgp.SplitArmorBlocks(armoredKeyring) {
		if entity, err := tagObj.Verify(string(block)); err == nil && entity != nil {
			return entity
		}
	}

	return nil
}

// tagRefName accepts either spelling of a tag — the short name ("v1.2.3") or
// the full ref path ("refs/tags/v1.2.3") — and returns the short name.
//
// go-git's Repository.Tag builds refs/tags/<name> itself, so handing it a full
// ref path looks up refs/tags/refs/tags/<name> and finds nothing. git accepts
// both spellings, so this adapter does too, and it normalises here rather than
// asking every call site to remember: the failure mode is silent. The lookup
// misses, resolution falls through to a peeling resolver, and an annotated tag
// is reported as its commit — which is how `release validate-tag` came to
// refuse every annotated release tag as "not an annotated tag object".
//
// A revision expression ("v1.2.3^{commit}") keeps its suffix, so it is not
// found as a tag name and falls through to the resolver that understands it.
func tagRefName(ref string) string {
	return strings.TrimPrefix(ref, "refs/tags/")
}

func (r *Repo) lookupTagObject(tag string) (*object.Tag, bool, error) {
	repo, err := r.openGoGit()
	if err != nil {
		return nil, false, err
	}

	ref, err := repo.Tag(tagRefName(tag))
	if err != nil {
		if errors.Is(err, gogit.ErrTagNotFound) {
			return nil, false, nil
		}

		return nil, false, err
	}

	tagObj, err := repo.TagObject(ref.Hash())
	if err != nil {
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			// Lightweight tag — ref points at a commit, not a tag object.
			return nil, false, nil
		}

		return nil, false, err
	}

	return tagObj, true, nil
}

// renderTagObject re-renders an annotated tag's canonical body so
// callers that parse the raw `git cat-file tag` output see the same
// shape. Header order matches git's spec.
func renderTagObject(t *object.Tag) string { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "object %s\n", t.Target)
	_, _ = fmt.Fprintf(&b, "type %s\n", t.TargetType)
	_, _ = fmt.Fprintf(&b, "tag %s\n", t.Name)
	_, _ = fmt.Fprintf(&b, "tagger %s <%s> %d %s\n", t.Tagger.Name, t.Tagger.Email,
		t.Tagger.When.Unix(), t.Tagger.When.Format("-0700"))
	b.WriteString("\n")
	b.WriteString(t.Message)

	if !strings.HasSuffix(t.Message, "\n") {
		b.WriteString("\n")
	}

	if t.PGPSignature != "" {
		b.WriteString(t.PGPSignature)

		if !strings.HasSuffix(t.PGPSignature, "\n") {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// HasPathspecChanges reads tracked and untracked changes without refreshing the
// index or invoking repository filesystem-monitor hooks. Used by dry-run plans.
func (r *Repo) HasPathspecChanges(ctx context.Context, pathspecs []string) (bool, error) {
	args := []string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "status", "--porcelain=v1", "--"}
	out, err := r.Run(ctx, append(args, pathspecs...)...)

	return out != "", err
}
