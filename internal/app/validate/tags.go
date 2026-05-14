// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate

import (
	"context"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/validate"
)

// gitOps is the slice of internal/adapters/git.Repo this package needs.
// Defining a small interface keeps tests fake-able without importing the
// real adapter from app-layer test code.
type gitOps interface {
	RevParse(ctx context.Context, ref string) (string, error)
	TagsPointingAt(ctx context.Context, commit string) ([]string, error)
	IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error)
	CatFileType(ctx context.Context, ref string) (string, error)
	CatFileTag(ctx context.Context, tag string) (string, error)
	VerifyTag(ctx context.Context, tag string) (string, bool, error)
	TaggerInfo(ctx context.Context, tag string) (string, string, error)
	TagMessage(ctx context.Context, tag string) (string, error)
	TagSHA(ctx context.Context, tag string) (string, error)
}

// gpgOps is the slice of adapter/gpg.Adapter the tag-signature flow uses.
type gpgOps interface {
	ImportKey(ctx context.Context, keyData []byte) error
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
		return fmt.Errorf("Usage: validate tag-uniqueness <tag-name>: %w", errs.ErrUsage)
	}
	fmt.Fprintf(out, "→ Validating Tag Points to Unique Commit\n")
	commit, err := repo.RevParse(ctx, in.Tag+"^{commit}")
	if err != nil {
		return fmt.Errorf("resolve tag commit: %w", err)
	}
	fmt.Fprintf(out, "✓ Tag '%s' points to commit: %s\n", in.Tag, commit)

	all, err := repo.TagsPointingAt(ctx, commit)
	if err != nil {
		return fmt.Errorf("list tags at commit: %w", err)
	}
	others := validate.FilterOutTag(all, in.Tag)
	if len(others) > 0 {
		var b []byte
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

	fmt.Fprintf(out, "✓ Tag '%s' points to a unique commit\n", in.Tag)
	fmt.Fprintf(out, "✓ No other tags found on commit %s\n", commit)
	return nil
}

// TagCommitInput drives `validate tag-commit`.
type TagCommitInput struct {
	Tag    string
	Branch string // empty defaults to "main"
}

// TagCommit verifies the tagged commit is reachable from origin/<branch>.
// On Ahead / Diverged it returns an error with the bash-equivalent guidance.
func TagCommit(ctx context.Context, repo gitOps, out io.Writer, in TagCommitInput) error {
	if in.Tag == "" {
		return fmt.Errorf("Usage: validate tag-commit <tag-name> <branch-name>: %w", errs.ErrUsage)
	}
	branch := in.Branch
	if branch == "" {
		branch = "main"
	}
	fmt.Fprintf(out, "## Validating Tag Commit is Available on Branch\n")
	tagCommit, err := repo.RevParse(ctx, in.Tag+"^{commit}")
	if err != nil {
		return fmt.Errorf("resolve tag commit: %w", err)
	}
	fmt.Fprintf(out, "Tag '%s' points to commit: %s\n", in.Tag, tagCommit)
	branchHead, err := repo.RevParse(ctx, "origin/"+branch)
	if err != nil {
		return fmt.Errorf("resolve origin/%s: %w", branch, err)
	}
	fmt.Fprintf(out, "Branch '%s' HEAD: %s\n", branch, branchHead)

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
			"Tag commit is AHEAD of branch HEAD\n\n"+
				"Tag '%s' points to: %s\n"+
				"Branch '%s' is at: %s\n\n"+
				"This means the commits for this tag were not pushed to '%s' yet.\n\n"+
				"To fix:\n"+
				"  1. Push your commits first: git push origin %s\n"+
				"  2. Then push the tag: git push origin %s: %w",
			in.Tag, tagCommit, branch, branchHead, branch, branch, in.Tag, errs.ErrValidation)
	case validate.BranchPositionDiverged:
		return fmt.Errorf(
			"Tag commit is not in the history of branch '%s'\n\n"+
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
	}

	fmt.Fprintf(out, "✓ Tag commit %s is in branch '%s' history\n", tagCommit, branch)
	if pos == validate.BranchPositionAtHead {
		fmt.Fprintf(out, "✓ Tag points to branch HEAD (ideal)\n")
	} else {
		fmt.Fprintf(out, "ℹ️  Tag commit is an ancestor of branch HEAD\n")
		fmt.Fprintf(out, "   This is normal for existing releases\n")
	}
	return nil
}

// TagSignatureInput drives `validate tag-signature`.
type TagSignatureInput struct {
	Tag                 string
	Repository          string // optional, used to render the docs URL on failure
	ReleaseGPGPublicKey []byte // optional armored key for verification
}

// TagSignature verifies that <tag> is annotated and cryptographically
// signed (GPG or SSH). When a public key is provided we additionally
// import it and try `git tag -v` for the signer identity. Verification
// failure is informational, not fatal — only "no signature at all" or
// "lightweight tag" return errors.
func TagSignature(ctx context.Context, gitr gitOps, gpg gpgOps, out io.Writer, in TagSignatureInput) error {
	if in.Tag == "" {
		return fmt.Errorf("Usage: validate tag-signature <tag-name> [repository]: %w", errs.ErrUsage)
	}
	fmt.Fprintf(out, "## Validating Release Tag Security\n")

	objType, err := gitr.CatFileType(ctx, in.Tag)
	if err != nil {
		objType = "unknown"
	}
	fmt.Fprintf(out, "Tag '%s' object type: %s\n", in.Tag, objType)
	if objType != "tag" {
		return fmt.Errorf(
			"Tag '%s' is a lightweight tag (not annotated)\n"+
				"📝 Requirement: Use annotated tags for releases\n"+
				"💡 Example: git tag -a v1.0.0 -m 'Release v1.0.0': %w",
			in.Tag, errs.ErrValidation)
	}
	fmt.Fprintf(out, "✓ Tag '%s' is annotated\n", in.Tag)

	body, err := gitr.CatFileTag(ctx, in.Tag)
	if err != nil {
		return fmt.Errorf("read tag body: %w", err)
	}
	fmt.Fprintf(out, "Checking tag signature...\n")
	sigs := validate.DetectTagSignatures(body)
	if sigs.HasGPG {
		fmt.Fprintf(out, "✓ Tag has a GPG signature\n")
	}
	if sigs.HasSSH {
		fmt.Fprintf(out, "✓ Tag has an SSH signature\n")
	}
	if !sigs.Any() {
		return fmt.Errorf(
			"Tag '%s' is not signed\n\n"+
				"Release tags must be cryptographically signed.\n"+
				"Create with: git tag -s v1.0.0 -m \"Release v1.0.0\"\n"+
				"Learn more: https://docs.github.com/en/authentication/managing-commit-signature-verification: %w",
			in.Tag, errs.ErrValidation)
	}

	if sigs.HasGPG {
		if len(in.ReleaseGPGPublicKey) > 0 && gpg != nil {
			_ = gpg.ImportKey(ctx, in.ReleaseGPGPublicKey)
		}
		verifyOut, ok, _ := gitr.VerifyTag(ctx, in.Tag)
		if ok {
			fmt.Fprintf(out, "✓ GPG signature verification successful\n")
			signer := validate.ParseGoodSignerFromVerify(verifyOut)
			if signer == "" {
				signer = "Unknown"
			}
			fmt.Fprintf(out, "   Signed by: %s\n", signer)
		} else {
			fmt.Fprintf(out, "ℹ️ GPG signature present (verification requires signer's public key)\n")
		}
	}
	if sigs.HasSSH {
		fmt.Fprintf(out, "ℹ️ SSH signature present\n")
		fmt.Fprintf(out, "   GitHub will show 'Verified' if the SSH key is uploaded to the signer's account\n")
	}

	fmt.Fprintf(out, "\n### Tag Security Summary:\n")
	fmt.Fprintf(out, "✓ Tag is annotated (not lightweight)\n")
	fmt.Fprintf(out, "✓ Tag is cryptographically signed\n")
	fmt.Fprintf(out, "✓ Release security requirements met\n\n")

	commit, _ := gitr.TagSHA(ctx, in.Tag)
	tagger, date, _ := gitr.TaggerInfo(ctx, in.Tag)
	msg, _ := gitr.TagMessage(ctx, in.Tag)
	fmt.Fprintf(out, "### Tag Information:\n")
	fmt.Fprintf(out, "Tagged commit: %s\n", commit)
	fmt.Fprintf(out, "Tagger: %s\n", tagger)
	fmt.Fprintf(out, "Tag date: %s\n\n", date)
	fmt.Fprintf(out, "Tag message:\n  %s\n", msg)
	return nil
}

// GPGPublicKey checks that the RELEASE_GPG_PUBLIC_KEY env / input is set.
// The bash equivalent is a one-liner; we keep it as its own use case so
// the CLI surface mirrors the script names 1:1.
func GPGPublicKey(out io.Writer, releaseGPGPublicKey string) error {
	if releaseGPGPublicKey == "" {
		return fmt.Errorf("Missing RELEASE_GPG_PUBLIC_KEY secret\n"+
			"This secret is needed for GPG operations and signing\n"+
			"Add it in Settings → Secrets → Actions: %w", errs.ErrPermissionDenied)
	}
	fmt.Fprintf(out, "✓ GPG public key configured\n")
	return nil
}
