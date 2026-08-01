// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/openpgp"
	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/validate"
)

// gitOps is the slice of internal/adapters/git.Repo this package needs.
// Defining a small interface keeps tests fake-able without importing
// the real adapter from app-layer test code. Everything is in-process
// via go-git / go-crypto — no subprocess git calls remain on the read
// path used during release validation.
type gitOps interface {
	RevParse(ctx context.Context, ref string) (string, error)
	TagsPointingAt(ctx context.Context, commit string) ([]string, error)
	IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error)
	CatFileType(ctx context.Context, ref string) (string, error)
	CatFileTag(ctx context.Context, tag string) (string, error)
	// VerifyTagSignature verifies a GPG-signed tag in-process; returns
	// signer display string, primary-key fingerprint (uppercase hex),
	// ok flag, error. Fingerprint is what TagSignature checks against the
	// fingerprints derived from the project's allowed_gpg_keys.asc bundle.
	VerifyTagSignature(ctx context.Context, tag string, armoredKeyring []byte) (signer, fingerprint string, ok bool, err error)
	// VerifyTagSSHAgainstAllowedSigners shells out to `git verify-tag`
	// with gpg.ssh.allowedSignersFile pointed at allowedSignersPath.
	// Returns ok=true when signed AND signer is in the file; ok=false
	// + ErrPermissionDenied when signed but not in the file; other
	// errors for unsigned / malformed / missing file.
	VerifyTagSSHAgainstAllowedSigners(ctx context.Context, tag, allowedSignersPath string) (ok bool, output string, err error)
	TaggerInfo(ctx context.Context, tag string) (git.TaggerInfo, error)
	TagMessage(ctx context.Context, tag string) (string, error)
	TagSHA(ctx context.Context, tag string) (string, error)
}

// TagUniquenessInput drives `validate tag-uniqueness`.
type TagUniquenessInput struct {
	Tag string
}

// TagUniqueness errors when one or more *other* tags point at the same
// commit. Used to guard against the git-cliff changelog issue
// (orhun/git-cliff#1036).
func TagUniqueness(ctx context.Context, repo gitOps, out io.Writer, in TagUniquenessInput) error {
	if in.Tag == "" {
		return fmt.Errorf("usage: validate tag-uniqueness <tag-name>: %w", errs.ErrUsage)
	}

	_, _ = fmt.Fprintf(out, "→ Validating Tag Points to Unique Commit\n")

	commit, err := repo.RevParse(ctx, in.Tag+"^{commit}")
	if err != nil {
		return fmt.Errorf("resolve tag commit: %w", err)
	}

	_, _ = fmt.Fprintf(out, "%s Tag '%s' points to commit: %s\n", clicolor.Check(out), in.Tag, commit)

	all, err := repo.TagsPointingAt(ctx, commit)
	if err != nil {
		return fmt.Errorf("list tags at commit: %w", err)
	}

	others := validate.FilterOutTag(all, in.Tag)
	if len(others) > 0 {
		var b []byte //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

		b = fmt.Appendf(b, "Tag '%s' points to the same commit as other tag(s)\n\n", in.Tag)

		b = fmt.Appendf(b, "The following tags also point to commit %s:\n", commit)
		for _, t := range others {
			b = fmt.Appendf(b, "  - %s\n", t)
		}

		b = fmt.Appendf(b, "\nMultiple tags on the same commit cause changelog generation issues.\n")
		b = fmt.Appendf(b, "This is a known limitation in git-cliff:\n")
		b = fmt.Appendf(b, "https://github.com/orhun/git-cliff/issues/1036")

		return fmt.Errorf("%s: %w", b, errs.ErrValidation)
	}

	_, _ = fmt.Fprintf(out, "%s Tag '%s' points to a unique commit\n", clicolor.Check(out), in.Tag)
	_, _ = fmt.Fprintf(out, "%s No other tags found on commit %s\n", clicolor.Check(out), commit)

	return nil
}

// TagCommitInput drives `validate tag-commit`.
type TagCommitInput struct {
	Tag    string
	Branch string // empty defaults to "main"
}

// TagCommit verifies the tagged commit is reachable from origin/<branch>.
// On Ahead / Diverged it returns an error with operator guidance.
//
//nolint:cyclop // tag-commit validation: tag → release-branch → workflow → annotated check.
func TagCommit(ctx context.Context, repo gitOps, out io.Writer, in TagCommitInput) error {
	if in.Tag == "" {
		return fmt.Errorf("usage: validate tag-commit <tag-name> <branch-name>: %w", errs.ErrUsage)
	}

	branch := in.Branch
	if branch == "" {
		branch = "main"
	}

	_, _ = fmt.Fprintf(out, "## Validating Tag Commit is Available on Branch\n")

	tagCommit, err := repo.RevParse(ctx, in.Tag+"^{commit}")
	if err != nil {
		// Adapter already classifies as validation failure with the
		// git command + stderr appended; the "resolve tag commit:"
		// prefix would just shadow that with less context.
		return err
	}

	_, _ = fmt.Fprintf(out, "Tag '%s' points to commit: %s\n", in.Tag, tagCommit)

	branchHead, err := repo.RevParse(ctx, "origin/"+branch)
	if err != nil {
		return fmt.Errorf("resolve origin/%s: %w", branch, err)
	}

	_, _ = fmt.Fprintf(out, "Branch '%s' HEAD: %s\n", branch, branchHead)

	tagInBranch, err := repo.IsAncestor(ctx, tagCommit, "origin/"+branch)
	if err != nil {
		return fmt.Errorf("ancestor probe (tag→branch): %w", err)
	}

	branchInTag, err := repo.IsAncestor(ctx, branchHead, tagCommit)
	if err != nil {
		return fmt.Errorf("ancestor probe (branch→tag): %w", err)
	}

	pos := validate.ClassifyBranchPosition(tagInBranch, branchInTag, tagCommit == branchHead)

	switch pos {
	case validate.BranchPositionAhead:
		return fmt.Errorf(
			"tag commit is AHEAD of branch HEAD\n\n"+
				"Tag '%s' points to: %s\n"+
				"Branch '%s' is at: %s\n\n"+
				"This means the commits for this tag were not pushed to '%s' yet.\n\n"+
				"To fix:\n"+
				"  1. Push your commits first: git push origin %s\n"+
				"  2. Then push the tag: git push origin %s: %w",
			in.Tag, tagCommit, branch, branchHead, branch, branch, in.Tag, errs.ErrValidation)
	case validate.BranchPositionDiverged:
		return fmt.Errorf(
			"tag commit is not in the history of branch '%s'\n\n"+
				"Tag '%s' points to commit %s\n"+
				"This commit is NOT an ancestor of origin/%s\n\n"+
				"This means either:\n"+
				"  1. The tag is on a different branch\n"+
				"  2. The tag was created from a stale local branch\n"+
				"  3. The branches have diverged\n\n"+
				"To fix:\n"+
				"  1. Verify: git log --oneline --graph --all\n"+
				"  2. Ensure tag is on correct branch\n"+
				"  3. Delete and recreate tag: git tag -d %s && git tag -s %s: %w",
			branch, in.Tag, tagCommit, branch, in.Tag, in.Tag, errs.ErrValidation)
	case validate.BranchPositionAtHead, validate.BranchPositionAncestor:
		// The two non-fatal positions — fall through to the success-log
		// branch below.
	}

	_, _ = fmt.Fprintf(out, "%s Tag commit %s is in branch '%s' history\n", clicolor.Check(out), tagCommit, branch)

	if pos == validate.BranchPositionAtHead {
		_, _ = fmt.Fprintf(out, "%s Tag points to branch HEAD (ideal)\n", clicolor.Check(out))
	} else {
		_, _ = fmt.Fprintf(out, "ℹ️  Tag commit is an ancestor of branch HEAD\n")
		_, _ = fmt.Fprintf(out, "   This is normal for existing releases\n")
	}

	return nil
}

// TagSignatureInput drives `validate tag-signature`.
type TagSignatureInput struct {
	Tag                 string
	Repository          string // optional, used to render the docs URL on failure
	ReleaseGPGPublicKey []byte // optional armored key for verification

	// RequireAllowlistedSigner enforces that the signer is in the
	// project's committed allowlist. When true, a missing/empty allowlist
	// or an unverifiable signature fails closed (ErrPermissionDenied).
	// When false, the check still runs informationally and warns loudly if
	// no allowlist is present — release-authorisation policy lives in the
	// per-artefact `require-authorization` flag.
	RequireAllowlistedSigner bool

	// AllowedSignersPath is the OpenSSH allowed_signers file for SSH
	// signatures. Empty → default ".reusable-ci/allowed_signers". Each line
	// carries the public key inline, so it is both the authorised set and
	// the verification material.
	AllowedSignersPath string

	// AllowedGPGKeysPath is an armored public-key bundle (.asc) whose keys
	// are BOTH the verification material and the GPG signer allowlist
	// (single source — the keys' primary fingerprints are the authorised
	// set). Empty → default ".reusable-ci/allowed_gpg_keys.asc". A
	// human-signed trigger tag verifies because the signer's public key
	// travels with the repo, so enforcement is self-contained.
	AllowedGPGKeysPath string
}

// Default file locations for the two allowlist files (one per signature
// type). Committed to the repo and read at release-validation time.
// Documented in docs/verification.md.
const (
	defaultAllowedSignersPath = ".reusable-ci/allowed_signers"
	defaultAllowedGPGKeysPath = ".reusable-ci/allowed_gpg_keys.asc"
)

// TagSignature verifies that <tag> is annotated and cryptographically
// signed (GPG or SSH). When a public key is provided, the in-process
// verifier (go-git + go-crypto/openpgp) is invoked for the signer
// identity. Verification failure is informational, not fatal — only
// "no signature at all" or "lightweight tag" return errors.
//
//nolint:cyclop // tag-signature validation: present → annotated → key-source → verify → render.
func TagSignature(ctx context.Context, gitr gitOps, out io.Writer, annot output.Annotator, in TagSignatureInput) error {
	if in.Tag == "" {
		return fmt.Errorf("usage: validate tag-signature <tag-name> [repository]: %w", errs.ErrUsage)
	}

	_, _ = fmt.Fprintf(out, "## Validating Release Tag Security\n")

	objType, err := gitr.CatFileType(ctx, in.Tag)
	if err != nil {
		objType = "unknown"
	}

	_, _ = fmt.Fprintf(out, "Tag '%s' object type: %s\n", in.Tag, objType)

	if objType != "tag" {
		return fmt.Errorf(
			"tag '%s' is a lightweight tag (not annotated)\n"+
				"📝 Requirement: Use annotated tags for releases\n"+
				"💡 Example: git tag -a v1.0.0 -m 'Release v1.0.0': %w",
			in.Tag, errs.ErrValidation)
	}

	_, _ = fmt.Fprintf(out, "%s Tag '%s' is annotated\n", clicolor.Check(out), in.Tag)

	body, err := gitr.CatFileTag(ctx, in.Tag)
	if err != nil {
		return fmt.Errorf("read tag body: %w", err)
	}

	_, _ = fmt.Fprintf(out, "Checking tag signature...\n")

	sigs := validate.DetectTagSignatures(body)
	if sigs.HasGPG {
		_, _ = fmt.Fprintf(out, "%s Tag has a GPG signature\n", clicolor.Check(out))
	}

	if sigs.HasSSH {
		_, _ = fmt.Fprintf(out, "%s Tag has an SSH signature\n", clicolor.Check(out))
	}

	if !sigs.Any() {
		return fmt.Errorf(
			"tag '%s' is not signed\n\n"+
				"Release tags must be cryptographically signed.\n"+
				"Create with: git tag -s v1.0.0 -m \"Release v1.0.0\"\n"+
				"Learn more: https://docs.github.com/en/authentication/managing-commit-signature-verification: %w",
			in.Tag, errs.ErrValidation)
	}

	allowedSignersPath := in.AllowedSignersPath
	if allowedSignersPath == "" {
		allowedSignersPath = defaultAllowedSignersPath
	}

	allowedKeysPath := in.AllowedGPGKeysPath
	if allowedKeysPath == "" {
		allowedKeysPath = defaultAllowedGPGKeysPath
	}

	if sigs.HasGPG {
		if err := checkGPGSignerAllowlist(ctx, gitr, out, annot, in, allowedKeysPath); err != nil {
			return err
		}
	}

	if sigs.HasSSH {
		if err := checkSSHSignerAllowlist(ctx, gitr, out, annot, in, allowedSignersPath); err != nil {
			return err
		}
	}

	_, _ = fmt.Fprintf(out, "\n### Tag Security Summary:\n")
	_, _ = fmt.Fprintf(out, "%s Tag is annotated (not lightweight)\n", clicolor.Check(out))
	_, _ = fmt.Fprintf(out, "%s Tag is cryptographically signed\n", clicolor.Check(out))
	_, _ = fmt.Fprintf(out, "%s Release security requirements met\n\n", clicolor.Check(out))

	commit, _ := gitr.TagSHA(ctx, in.Tag)
	tag, _ := gitr.TaggerInfo(ctx, in.Tag)
	msg, _ := gitr.TagMessage(ctx, in.Tag)

	_, _ = fmt.Fprintf(out, "### Tag Information:\n")
	_, _ = fmt.Fprintf(out, "Tagged commit: %s\n", commit)
	_, _ = fmt.Fprintf(out, "Tagger: %s\n", tag.Tagger)
	_, _ = fmt.Fprintf(out, "Tag date: %s\n\n", tag.Date)
	_, _ = fmt.Fprintf(out, "Tag message:\n  %s\n", msg)

	return nil
}

// checkGPGSignerAllowlist verifies the GPG-signed tag against the keys the
// project trusts, then asserts the signer is authorised. Trust comes from
// one committed file:
//
//   - allowed_gpg_keys.asc — an armored public-key bundle. Its keys are
//     BOTH the verification material AND the authorised set (single
//     source): a human-signed trigger tag verifies because the signer's
//     public key travels with the repo, and membership is the set of
//     primary-key fingerprints in the bundle. No separate fingerprint list
//     exists, so "listed but unverifiable" is impossible by construction.
//
// Behaviour matrix:
//
//   - no allowlist present + require=true  → ErrPermissionDenied
//   - no allowlist present + require=false → loud warning, then pass
//     (the project hasn't opted in; the release proceeds but the gap is
//     surfaced in the Annotations pane and the summary)
//   - signature can't be verified + require=true  → ErrPermissionDenied
//     (fail closed — "I required allowlisting but couldn't establish the
//     signer")
//   - signature can't be verified + require=false → loud warning, pass
//   - signer fingerprint in the set → pass
//   - signer fingerprint NOT in the set → ErrPermissionDenied (the
//     allowlist exists, so it is authoritative regardless of require)
func checkGPGSignerAllowlist(ctx context.Context, gitr gitOps, out io.Writer, annot output.Annotator, in TagSignatureInput, allowedKeysPath string) error {
	allowedKeys, _, err := readOptionalFile(allowedKeysPath)
	if err != nil {
		return err
	}

	allowlistPresent := len(bytes.TrimSpace(allowedKeys)) > 0

	// Verification keyring: the optionally-supplied release key (e.g. the
	// bot key for an already-re-signed tag) plus every committed allowed
	// key. go-git checks the signature against any key in the bundle.
	keyring := combineArmoredKeys(in.ReleaseGPGPublicKey, allowedKeys)

	signer, fingerprint, ok, vErr := gitr.VerifyTagSignature(ctx, in.Tag, keyring)
	verified := reportGPGVerificationOutcome(out, signer, fingerprint, ok, vErr, keyring)

	if !allowlistPresent {
		if in.RequireAllowlistedSigner {
			return fmt.Errorf(
				"require-authorization is enabled but no GPG allowlist is present — commit %s (armored public keys of authorised signers): %w",
				allowedKeysPath, errs.ErrPermissionDenied)
		}

		warnNoAllowlist(out, annot, "GPG", allowedKeysPath)

		return nil
	}

	if !verified {
		if in.RequireAllowlistedSigner {
			return fmt.Errorf(
				"require-authorization is enabled but the tag signature could not be verified against any authorised key — commit the signer's public key to %s: %w",
				allowedKeysPath, errs.ErrPermissionDenied)
		}

		warnUnverifiableSigner(out, annot, allowedKeysPath)

		return nil
	}

	set, err := buildGPGAllowlist(allowedKeys)
	if err != nil {
		return err
	}

	if !set.Has(fingerprint) {
		return fmt.Errorf(
			"tag signer fingerprint %s is not authorised (%d key(s) in %s)\n\n"+
				"Either:\n"+
				"  - sign the tag with one of the authorised keys, or\n"+
				"  - add this signer's public key to %s in a reviewed PR: %w",
			fingerprint, set.Len(), allowedKeysPath, allowedKeysPath, errs.ErrPermissionDenied)
	}

	_, _ = fmt.Fprintf(out, "%s Signer fingerprint is authorised\n", clicolor.Check(out))

	return nil
}

// readOptionalFile returns a file's contents and whether it existed.
// A missing file is (nil, false, nil); any other read error is returned.
// Allowlist files are CI-plan-derived locations, not user input.
func readOptionalFile(path string) ([]byte, bool, error) {
	body, err := os.ReadFile(path) //nolint:gosec // path is a CI-plan-derived allowlist location, not user-controlled input.
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}

		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}

	return body, true, nil
}

// combineArmoredKeys concatenates armored key blocks into a single
// keyring blob accepted by openpgp.ReadArmoredKeyRing. Empty blocks are
// skipped; a newline separates blocks so two armors that each lack a
// trailing newline don't merge their boundary lines.
func combineArmoredKeys(blocks ...[]byte) []byte {
	var buf []byte

	for _, block := range blocks {
		trimmed := bytes.TrimSpace(block)
		if len(trimmed) == 0 {
			continue
		}

		if len(buf) > 0 {
			buf = append(buf, '\n')
		}

		buf = append(buf, trimmed...)
	}

	return buf
}

// buildGPGAllowlist builds the authorised set from the primary-key
// fingerprints of the committed allowed_gpg_keys.asc bundle — the single
// source of both verification material and authorisation.
func buildGPGAllowlist(allowedKeys []byte) (validate.AllowedFingerprintSet, error) {
	fps, err := openpgp.PrimaryFingerprints(allowedKeys)
	if err != nil {
		return validate.AllowedFingerprintSet{}, fmt.Errorf("parse allowed GPG keys: %w", err)
	}

	set := validate.NewAllowedFingerprintSet()
	for _, fingerprint := range fps {
		set.Add(fingerprint)
	}

	return set, nil
}

// warnNoAllowlist loudly surfaces that a release ran with no signer allowlist.
// The annotation lands in the GitHub Actions Annotations pane (`::warning::`)
// or a plain `Warning:` line off-GitHub; the ⚠️ line is collected by
// WritePrerequisitesSummary into the summary's warning callout.
func warnNoAllowlist(out io.Writer, annot output.Annotator, method, paths string) {
	annot.WarningAt(output.Annotation{Title: "No release signer allowlist"},
		"%s release ran with NO signer allowlist — any valid signature was accepted. Commit %s to authorise specific signers.",
		method, paths)

	_, _ = fmt.Fprintf(out,
		"⚠️ No %s signer allowlist present (%s) — enforcement skipped (require-authorization=false)\n",
		method, paths)
}

// warnUnverifiableSigner loudly surfaces that an allowlist exists but the
// tag signature could not be checked against it (the signer's public key
// is not available). With require-authorization=false this is a warning,
// not a failure; with it true the caller fails closed instead.
func warnUnverifiableSigner(out io.Writer, annot output.Annotator, allowedKeysPath string) {
	annot.WarningAt(output.Annotation{Title: "Unverifiable tag signature"},
		"Tag signature could not be verified against any authorised key — allowlist not enforced. Commit the signer's public key to %s.",
		allowedKeysPath)

	_, _ = fmt.Fprintf(out,
		"⚠️ GPG signature could not be verified against the allowlist — enforcement skipped (require-authorization=false)\n")
}

// reportGPGVerificationOutcome writes the GPG signature-verification
// message to out and reports whether verification concluded. Returns
// false (and writes a diagnostic ℹ️ note explaining why) when the
// signature couldn't be checked against the available key material;
// returns true (and writes a "verified" line) when the fingerprint was
// confirmed. The caller decides what an unverified outcome means
// (fail-closed under require, loud warning otherwise).
func reportGPGVerificationOutcome(out io.Writer, signer, fingerprint string, ok bool, vErr error, keyMaterial []byte) bool {
	switch {
	case vErr != nil:
		_, _ = fmt.Fprintf(out, "ℹ️ GPG signature verification skipped: %v\n", vErr)

		return false
	case !ok && len(bytes.TrimSpace(keyMaterial)) == 0:
		_, _ = fmt.Fprintf(out, "ℹ️ GPG signature present (verification requires the signer's public key)\n")

		return false
	case !ok:
		_, _ = fmt.Fprintf(out, "ℹ️ GPG signature present but did not verify against any available key\n")

		return false
	}

	_, _ = fmt.Fprintf(out, "%s GPG signature verified (fingerprint %s)\n", clicolor.Check(out), fingerprint)

	if signer != "" {
		_, _ = fmt.Fprintf(out, "   Signed by: %s\n", signer)
	}

	return true
}

// checkSSHSignerAllowlist delegates to `git verify-tag` with
// gpg.ssh.allowedSignersFile set — git invokes ssh-keygen -Y verify
// internally and does the right thing in one call. Behaviour mirrors
// checkGPGSignerAllowlist: file missing + require=true → fail closed.
func checkSSHSignerAllowlist(ctx context.Context, gitr gitOps, out io.Writer, annot output.Annotator, in TagSignatureInput, allowedSignersPath string) error {
	if _, err := os.Stat(allowedSignersPath); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("stat %s: %w", allowedSignersPath, err)
		}

		if in.RequireAllowlistedSigner {
			return fmt.Errorf(
				"require-authorization is enabled but %s is missing — commit an OpenSSH allowed_signers file (see man ssh-keygen, ALLOWED SIGNERS) listing every SSH key allowed to sign releases: %w",
				allowedSignersPath, errs.ErrPermissionDenied)
		}

		warnNoAllowlist(out, annot, "SSH", allowedSignersPath)

		return nil
	}

	ok, gitOutput, err := gitr.VerifyTagSSHAgainstAllowedSigners(ctx, in.Tag, allowedSignersPath)
	if err != nil {
		// ErrPermissionDenied case already wrapped by the adapter; let
		// the message through.
		return err
	}

	if !ok {
		// Adapter never returns ok=false without an error today; keep
		// this branch defensive against future refactors.
		return fmt.Errorf("SSH signature rejected by allowed_signers: %s: %w", gitOutput, errs.ErrPermissionDenied)
	}

	_, _ = fmt.Fprintf(out, "%s SSH signature verified against %s\n", clicolor.Check(out), allowedSignersPath)

	if gitOutput != "" {
		_, _ = fmt.Fprintf(out, "   %s\n", strings.TrimSpace(gitOutput))
	}

	return nil
}

// GPGPublicKey checks that the RELEASE_GPG_PUBLIC_KEY env / input is set.
// The equivalent is a one-liner; we keep it as its own use case so
// the CLI surface mirrors the script names 1:1.
func GPGPublicKey(out io.Writer, releaseGPGPublicKey string) error {
	if releaseGPGPublicKey == "" {
		return fmt.Errorf("missing RELEASE_GPG_PUBLIC_KEY secret\n"+
			"This secret is needed for GPG operations and signing\n"+
			"Add it in Settings → Secrets → Actions: %w", errs.ErrPermissionDenied)
	}

	_, _ = fmt.Fprintf(out, "%s GPG public key configured\n", clicolor.Check(out))

	return nil
}
