// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package git

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/internal/domain/git"
)

// RevParse resolves ref to a SHA. In-process via go-git when the repo
// is available; falls back to `git rev-parse` for unusual revisions
// (e.g. `HEAD~5^2`) the in-process resolver doesn't support natively.
func (r *Repo) RevParse(ctx context.Context, ref string) (string, error) {
	repo, err := r.openGoGit()
	if err == nil {
		hash, err := repo.ResolveRevision(plumbing.Revision(ref))
		if err == nil {
			return hash.String(), nil
		}
	}

	return r.Run(ctx, "rev-parse", ref)
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
// Pure in-process: go-git resolves both revisions and walks the commit
// graph. No subprocess.
func (r *Repo) IsAncestor(_ context.Context, ancestor, descendant string) (bool, error) {
	repo, err := r.openGoGit()
	if err != nil {
		return false, fmt.Errorf("open repo: %w", err)
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

	entity, verifyErr := tagObj.Verify(string(armoredKeyring))
	if verifyErr != nil || entity == nil {
		// Contract: report (signer, fingerprint, verified). A failed
		// verify is not a function-level error — it's "no Good signer,"
		// ok=false.
		return "", "", false, nil //nolint:nilerr // verify failure → unverified, by contract
	}

	fingerprint := strings.ToUpper(hex.EncodeToString(entity.PrimaryKey.Fingerprint))

	for _, ident := range entity.Identities {
		if signer := identityDisplayString(ident); signer != "" {
			return signer, fingerprint, true, nil
		}
	}

	return "", fingerprint, true, nil
}

// VerifyTagSSHAgainstAllowedSigners runs `git verify-tag` with
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
	out, err := r.Run(ctx, "-c", "gpg.ssh.allowedSignersFile="+allowedSignersPath, "verify-tag", tag)
	if err == nil {
		return true, out, nil
	}

	// On non-zero exit, the wrapped error carries git's stderr appended
	// after the safeexec wrap. "No principal matched." is the canonical
	// ssh-keygen -Y verify message when the signature is valid but the
	// signer isn't in allowed_signers — distinct from "no signature".
	msg := err.Error()
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
func (r *Repo) TaggerInfo(ctx context.Context, tag string) (string, string, error) {
	tagObj, found, err := r.lookupTagObject(tag)
	if err == nil && found {
		who := fmt.Sprintf("%s <%s>", tagObj.Tagger.Name, tagObj.Tagger.Email)
		date := tagObj.Tagger.When.Format("2006-01-02 15:04:05 -0700")

		return who, date, nil
	}

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
func (r *Repo) lookupTagObject(tag string) (*object.Tag, bool, error) {
	repo, err := r.openGoGit()
	if err != nil {
		return nil, false, err
	}

	ref, err := repo.Tag(tag)
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
