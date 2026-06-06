// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/validate"
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
	// ok flag, error. Fingerprint is what TagSignature checks against
	// the project's allowed_gpg_fingerprints file.
	VerifyTagSignature(ctx context.Context, tag string, armoredKeyring []byte) (signer, fingerprint string, ok bool, err error)
	// VerifyTagSSHAgainstAllowedSigners shells out to `git verify-tag`
	// with gpg.ssh.allowedSignersFile pointed at allowedSignersPath.
	// Returns ok=true when signed AND signer is in the file; ok=false
	// + ErrPermissionDenied when signed but not in the file; other
	// errors for unsigned / malformed / missing file.
	VerifyTagSSHAgainstAllowedSigners(ctx context.Context, tag, allowedSignersPath string) (ok bool, output string, err error)
	TaggerInfo(ctx context.Context, tag string) (string, string, error)
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

	_, _ = fmt.Fprintf(out, "✓ Tag '%s' points to commit: %s\n", in.Tag, commit)

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

	_, _ = fmt.Fprintf(out, "✓ Tag '%s' points to a unique commit\n", in.Tag)
	_, _ = fmt.Fprintf(out, "✓ No other tags found on commit %s\n", commit)

	return nil
}

// TagCommitInput drives `validate tag-commit`.
type TagCommitInput struct {
	Tag    string
	Branch string // empty defaults to "main"
}

// TagCommit verifies the tagged commit is reachable from origin/<branch>.
// On Ahead / Diverged it returns an error with operator guidance.
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

	_, _ = fmt.Fprintf(out, "✓ Tag commit %s is in branch '%s' history\n", tagCommit, branch)

	if pos == validate.BranchPositionAtHead {
		_, _ = fmt.Fprintf(out, "✓ Tag points to branch HEAD (ideal)\n")
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
	// project's committed allowlist. When true, missing/empty allowlist
	// files fail closed (ErrPermissionDenied). When false, the allowlist
	// check is still performed if the relevant file exists, but only
	// informationally — release-authorisation policy lives in the
	// per-artefact `require-authorization` flag.
	RequireAllowlistedSigner bool

	// AllowedSignersPath is the OpenSSH allowed_signers file for SSH
	// signatures. Empty → default ".reusable-ci/allowed_signers".
	AllowedSignersPath string

	// AllowedGPGFingerprintsPath is the flat-list fingerprint file for
	// GPG signatures. Empty → default ".reusable-ci/allowed_gpg_fingerprints".
	AllowedGPGFingerprintsPath string
}

// Default file locations for the two allowlist files. Committed to the
// repo and read at release-validation time. Documented in
// docs/verification.md.
const (
	defaultAllowedSignersPath         = ".reusable-ci/allowed_signers"
	defaultAllowedGPGFingerprintsPath = ".reusable-ci/allowed_gpg_fingerprints"
)

// TagSignature verifies that <tag> is annotated and cryptographically
// signed (GPG or SSH). When a public key is provided, the in-process
// verifier (go-git + go-crypto/openpgp) is invoked for the signer
// identity. Verification failure is informational, not fatal — only
// "no signature at all" or "lightweight tag" return errors.
//nolint:cyclop // tag-signature validation: present → annotated → key-source → verify → render.
func TagSignature(ctx context.Context, gitr gitOps, out io.Writer, in TagSignatureInput) error {
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

	_, _ = fmt.Fprintf(out, "✓ Tag '%s' is annotated\n", in.Tag)

	body, err := gitr.CatFileTag(ctx, in.Tag)
	if err != nil {
		return fmt.Errorf("read tag body: %w", err)
	}

	_, _ = fmt.Fprintf(out, "Checking tag signature...\n")

	sigs := validate.DetectTagSignatures(body)
	if sigs.HasGPG {
		_, _ = fmt.Fprintf(out, "✓ Tag has a GPG signature\n")
	}

	if sigs.HasSSH {
		_, _ = fmt.Fprintf(out, "✓ Tag has an SSH signature\n")
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

	allowedFingerprintsPath := in.AllowedGPGFingerprintsPath
	if allowedFingerprintsPath == "" {
		allowedFingerprintsPath = defaultAllowedGPGFingerprintsPath
	}

	if sigs.HasGPG {
		if err := checkGPGSignerAllowlist(ctx, gitr, out, in, allowedFingerprintsPath); err != nil {
			return err
		}
	}

	if sigs.HasSSH {
		if err := checkSSHSignerAllowlist(ctx, gitr, out, in, allowedSignersPath); err != nil {
			return err
		}
	}

	_, _ = fmt.Fprintf(out, "\n### Tag Security Summary:\n")
	_, _ = fmt.Fprintf(out, "✓ Tag is annotated (not lightweight)\n")
	_, _ = fmt.Fprintf(out, "✓ Tag is cryptographically signed\n")
	_, _ = fmt.Fprintf(out, "✓ Release security requirements met\n\n")

	commit, _ := gitr.TagSHA(ctx, in.Tag)
	tagger, date, _ := gitr.TaggerInfo(ctx, in.Tag)
	msg, _ := gitr.TagMessage(ctx, in.Tag)

	_, _ = fmt.Fprintf(out, "### Tag Information:\n")
	_, _ = fmt.Fprintf(out, "Tagged commit: %s\n", commit)
	_, _ = fmt.Fprintf(out, "Tagger: %s\n", tagger)
	_, _ = fmt.Fprintf(out, "Tag date: %s\n\n", date)
	_, _ = fmt.Fprintf(out, "Tag message:\n  %s\n", msg)

	return nil
}

// checkGPGSignerAllowlist runs the in-process GPG verification, extracts
// the signing primary-key fingerprint, and asserts it appears in the
// project's allowed_gpg_fingerprints file. Behaviour matrix:
//
//   - file missing + RequireAllowlistedSigner=true → ErrPermissionDenied
//   - file missing + RequireAllowlistedSigner=false → informational
//     skip (the project hasn't opted in)
//   - signature didn't verify (wrong key, no key available) →
//     informational, same as the pre-allowlist behaviour
//   - signature verified + fingerprint in allowlist → pass
//   - signature verified + fingerprint NOT in allowlist →
//     ErrPermissionDenied (regardless of RequireAllowlistedSigner: if the
//     file exists, having it makes it authoritative)
func checkGPGSignerAllowlist(ctx context.Context, gitr gitOps, out io.Writer, in TagSignatureInput, allowedFingerprintsPath string) error {
	signer, fingerprint, ok, vErr := gitr.VerifyTagSignature(ctx, in.Tag, in.ReleaseGPGPublicKey)
	if !reportGPGVerificationOutcome(out, signer, fingerprint, ok, vErr, in.ReleaseGPGPublicKey) {
		return nil
	}

	body, readErr := os.ReadFile(allowedFingerprintsPath) //nolint:gosec // allowedFingerprintsPath is the project's allowlist file, derived from the CI plan, not user-controlled input.
	if readErr != nil {
		if !errors.Is(readErr, fs.ErrNotExist) {
			return fmt.Errorf("read %s: %w", allowedFingerprintsPath, readErr)
		}

		if in.RequireAllowlistedSigner {
			return fmt.Errorf(
				"require-authorization is enabled but %s is missing — commit a fingerprints file listing every GPG primary-key fingerprint allowed to sign releases: %w",
				allowedFingerprintsPath, errs.ErrPermissionDenied)
		}

		_, _ = fmt.Fprintf(out, "ℹ️ No %s present — allowlist enforcement skipped (require-authorization=false)\n", allowedFingerprintsPath)

		return nil
	}

	set, parseErr := validate.ParseAllowedFingerprints(body)
	if parseErr != nil {
		return fmt.Errorf("parse %s: %w", allowedFingerprintsPath, parseErr)
	}

	if !set.Has(fingerprint) {
		return fmt.Errorf(
			"tag signer fingerprint %s is not in %s (%d authorised key(s))\n\n"+
				"Either:\n"+
				"  - sign the tag with one of the authorised keys, or\n"+
				"  - add this fingerprint to %s in a reviewed PR: %w",
			fingerprint, allowedFingerprintsPath, set.Len(),
			allowedFingerprintsPath, errs.ErrPermissionDenied)
	}

	_, _ = fmt.Fprintf(out, "✓ Signer fingerprint is in %s\n", allowedFingerprintsPath)

	return nil
}

// reportGPGVerificationOutcome writes the GPG signature-verification
// message to out and reports whether the caller should proceed to the
// allowlist check. Returns false (and writes a "skipped" /
// "present-but-unverified" note) when verification couldn't conclude;
// returns true (and writes a "verified" line) when the fingerprint
// was confirmed and the caller should look it up against the
// allowlist.
func reportGPGVerificationOutcome(out io.Writer, signer, fingerprint string, ok bool, vErr error, releaseGPGPublicKey []byte) bool {
	switch {
	case vErr != nil:
		_, _ = fmt.Fprintf(out, "ℹ️ GPG signature verification skipped: %v\n", vErr)

		return false
	case !ok && len(releaseGPGPublicKey) == 0:
		_, _ = fmt.Fprintf(out, "ℹ️ GPG signature present (verification requires signer's public key)\n")

		return false
	case !ok:
		_, _ = fmt.Fprintf(out, "ℹ️ GPG signature present but did not verify against the configured public key\n")

		return false
	}

	_, _ = fmt.Fprintf(out, "✓ GPG signature verified (fingerprint %s)\n", fingerprint)

	if signer != "" {
		_, _ = fmt.Fprintf(out, "   Signed by: %s\n", signer)
	}

	return true
}

// checkSSHSignerAllowlist delegates to `git verify-tag` with
// gpg.ssh.allowedSignersFile set — git invokes ssh-keygen -Y verify
// internally and does the right thing in one call. Behaviour mirrors
// checkGPGSignerAllowlist: file missing + require=true → fail closed.
func checkSSHSignerAllowlist(ctx context.Context, gitr gitOps, out io.Writer, in TagSignatureInput, allowedSignersPath string) error {
	if _, err := os.Stat(allowedSignersPath); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("stat %s: %w", allowedSignersPath, err)
		}

		if in.RequireAllowlistedSigner {
			return fmt.Errorf(
				"require-authorization is enabled but %s is missing — commit an OpenSSH allowed_signers file (see man ssh-keygen, ALLOWED SIGNERS) listing every SSH key allowed to sign releases: %w",
				allowedSignersPath, errs.ErrPermissionDenied)
		}

		_, _ = fmt.Fprintf(out, "ℹ️ No %s present — SSH allowlist enforcement skipped (require-authorization=false)\n", allowedSignersPath)

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

	_, _ = fmt.Fprintf(out, "✓ SSH signature verified against %s\n", allowedSignersPath)

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

	_, _ = fmt.Fprintf(out, "✓ GPG public key configured\n")

	return nil
}
