//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
)

func newRealGit(t *testing.T) (*adaptergit.Repo, *isolatedgit.Repo) {
	t.Helper()
	ig := isolatedgit.NewRepo(t)
	return &adaptergit.Repo{Dir: ig.Dir}, ig
}

func TestTagUniqueness_PassesWhenSingleTag(t *testing.T) {
	gitr, ig := newRealGit(t)
	ig.AddTag("v1.0.0", "release v1.0.0")

	var buf bytes.Buffer
	err := appvalidate.TagUniqueness(context.Background(), gitr, &buf, appvalidate.TagUniquenessInput{Tag: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "points to a unique commit") {
		t.Errorf("output = %s", buf.String())
	}
	if !strings.Contains(buf.String(), ig.HeadSHA()) {
		t.Errorf("output should include commit hash: %s", buf.String())
	}
}

func TestTagUniqueness_FailsWhenCollisions(t *testing.T) {
	gitr, ig := newRealGit(t)
	ig.AddTag("v1.0.0", "main")
	ig.AddTag("v1.0.0-alias", "alias")

	err := appvalidate.TagUniqueness(context.Background(), gitr, &bytes.Buffer{}, appvalidate.TagUniquenessInput{Tag: "v1.0.0"})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "v1.0.0-alias") {
		t.Errorf("error should list other tag: %v", err)
	}
	if !strings.Contains(err.Error(), "git-cliff/issues/1036") {
		t.Errorf("error should mention git-cliff issue: %v", err)
	}
}

func TestTagUniqueness_IgnoreTagDoesNotHideOtherCollisions(t *testing.T) {
	gitr, ig := newRealGit(t)
	ig.AddTag("release-request/v1.0.0", "request")
	ig.AddTag("v1.0.0", "expected final")
	ig.AddTag("v1.0.0-alias", "unexpected alias")

	err := appvalidate.TagUniqueness(context.Background(), gitr, &bytes.Buffer{}, appvalidate.TagUniquenessInput{
		Tag:       "release-request/v1.0.0",
		IgnoreTag: "v1.0.0",
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "v1.0.0-alias") {
		t.Errorf("error should list unexpected tag: %v", err)
	}

	// And it does not re-report the tag the caller told it to ignore.
	if strings.Contains(err.Error(), "v1.0.0\n") {
		t.Errorf("the ignored tag was reported as a collision: %v", err)
	}
}

func TestTagUniqueness_EmptyTagUsage(t *testing.T) {
	gitr, _ := newRealGit(t)
	err := appvalidate.TagUniqueness(context.Background(), gitr, &bytes.Buffer{}, appvalidate.TagUniquenessInput{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("err = %v, want a usage line", err)
	}
}

func TestTagCommit_AtHeadIdeal(t *testing.T) {
	gitr, ig := newRealGit(t)
	ig.AddBareRemote()
	ig.AddTag("v1.0.0", "rel")

	var buf bytes.Buffer
	err := appvalidate.TagCommit(context.Background(), gitr, &buf, appvalidate.TagCommitInput{Tag: "v1.0.0", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "points to branch HEAD (ideal)") {
		t.Errorf("output = %s", buf.String())
	}
}

func TestTagCommit_AncestorIsOK(t *testing.T) {
	gitr, ig := newRealGit(t)
	ig.AddTag("v1.0.0", "earlier release")
	// Push HEAD to remote and add a follow-up commit so the tag becomes
	// an ancestor (not at head).
	ig.AddBareRemote()
	ig.AddCommit("post-release fixup")
	ig.Git("push", "-q", "origin", "main")

	var buf bytes.Buffer
	err := appvalidate.TagCommit(context.Background(), gitr, &buf, appvalidate.TagCommitInput{Tag: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "ancestor of branch HEAD") {
		t.Errorf("output = %s", buf.String())
	}
}

func TestTagCommit_AheadFails(t *testing.T) {
	gitr, ig := newRealGit(t)
	ig.AddBareRemote()
	// Make a follow-up commit + tag locally without pushing.
	ig.AddCommit("unpushed commit")
	ig.AddTag("v1.0.0", "tag-on-unpushed")

	err := appvalidate.TagCommit(context.Background(), gitr, &bytes.Buffer{}, appvalidate.TagCommitInput{Tag: "v1.0.0"})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	for _, want := range []string{"AHEAD of branch HEAD", "git push origin main", "git push origin v1.0.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%s", want, err.Error())
		}
	}
}

func TestTagSignature_LightweightTagFails(t *testing.T) {
	gitr, ig := newRealGit(t)
	ig.Git("tag", "v1.0.0") // lightweight (no -a)

	err := appvalidate.TagSignature(context.Background(), gitr, &bytes.Buffer{}, output.NewAnnotator(&bytes.Buffer{}, output.FormatGitHub), appvalidate.TagSignatureInput{Tag: "v1.0.0"})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "lightweight tag") {
		t.Errorf("err = %v, want it to name the tag kind", err)
	}
}

func TestTagSignature_AnnotatedUnsignedFails(t *testing.T) {
	gitr, ig := newRealGit(t)
	ig.AddTag("v1.0.0", "annotated, unsigned")

	var buf bytes.Buffer
	err := appvalidate.TagSignature(context.Background(), gitr, &buf, output.NewAnnotator(&buf, output.FormatGitHub), appvalidate.TagSignatureInput{Tag: "v1.0.0"})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}

	// Split by where each string actually belongs. Accepting "either the
	// error or the log" meant the refusal could have lost its remediation
	// text, or the log its progress lines, without the test noticing.
	for _, want := range []string{"is not signed", "cryptographically signed", "git tag -s"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err missing %q: %v", want, err)
		}
	}

	for _, want := range []string{"object type: tag", "is annotated"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("log missing %q:\n%s", want, buf.String())
		}
	}
}

func TestTagSignature_EmptyTagUsage(t *testing.T) {
	gitr, _ := newRealGit(t)
	err := appvalidate.TagSignature(context.Background(), gitr, &bytes.Buffer{}, output.NewAnnotator(&bytes.Buffer{}, output.FormatGitHub), appvalidate.TagSignatureInput{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("err = %v, want a usage line", err)
	}
}

func TestGPGPublicKey_Empty(t *testing.T) {
	t.Parallel()
	err := appvalidate.GPGPublicKey(&bytes.Buffer{}, "")
	// An absent secret is a credential problem (exit 77), so the operator is
	// pointed at repository settings rather than at their command line.
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
	if !strings.Contains(err.Error(), "missing RELEASE_GPG_PUBLIC_KEY") {
		t.Errorf("err = %v, want it to name the secret", err)
	}
	for _, want := range []string{"Settings", "Actions"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

func TestGPGPublicKey_Set(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := appvalidate.GPGPublicKey(&buf, "ARMORED KEY DATA"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "GPG public key configured") {
		t.Errorf("output = %s", buf.String())
	}
}

// TestTagValidation_ABranchNamedLikeTheTagIsNotTheTag: the release tag was
// never fetched, but a local branch carries its name on another commit. A bare
// "v2.0.0^{commit}" resolves to that branch, so validating it would bless an
// unreviewed commit as the release. Both tag validators must refuse instead.
func TestTagValidation_ABranchNamedLikeTheTagIsNotTheTag(t *testing.T) {
	gitr, ig := newRealGit(t)
	ig.AddBareRemote()
	ig.Git("branch", "v2.0.0")

	var out bytes.Buffer

	if err := appvalidate.TagUniqueness(context.Background(), gitr, &out, appvalidate.TagUniquenessInput{Tag: "v2.0.0"}); err == nil {
		t.Errorf("TagUniqueness accepted a branch named v2.0.0 as the tag:\n%s", out.String())
	}

	out.Reset()

	if err := appvalidate.TagCommit(context.Background(), gitr, &out, appvalidate.TagCommitInput{Tag: "v2.0.0", Branch: "main"}); err == nil {
		t.Errorf("TagCommit accepted a branch named v2.0.0 as the tag:\n%s", out.String())
	}

	if branchCommit, err := gitr.RevParse(context.Background(), "v2.0.0^{commit}"); err != nil || branchCommit == "" {
		t.Fatalf("fixture is degenerate: the bare name does not resolve to the branch (%q, %v)", branchCommit, err)
	}
}

// TestTagCommit_ATagNamedLikeTheRemoteBranchDoesNotShadowIt: a tag called
// origin/main on an unrelated commit must not stand in for the remote-tracking
// branch the release tag is compared against.
func TestTagCommit_ATagNamedLikeTheRemoteBranchDoesNotShadowIt(t *testing.T) {
	gitr, ig := newRealGit(t)
	ig.AddBareRemote()
	ig.AddTag("v1.0.0", "rel")
	ig.Git("checkout", "-q", "--orphan", "decoy")
	ig.AddCommit("decoy: unrelated history")
	ig.Git("tag", "origin/main")
	ig.Git("checkout", "-q", "main")

	var buf bytes.Buffer
	if err := appvalidate.TagCommit(context.Background(), gitr, &buf, appvalidate.TagCommitInput{Tag: "v1.0.0", Branch: "main"}); err != nil {
		t.Fatalf("TagCommit compared against the origin/main tag: %v\n%s", err, buf.String())
	}

	if !strings.Contains(buf.String(), "points to branch HEAD (ideal)") {
		t.Errorf("output = %s", buf.String())
	}
}
