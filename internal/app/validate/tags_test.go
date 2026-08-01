//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
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
	if err == nil {
		t.Fatal("expected collision error")
	}
	if !strings.Contains(err.Error(), "v1.0.0-alias") {
		t.Errorf("error should list other tag: %v", err)
	}
	if !strings.Contains(err.Error(), "git-cliff/issues/1036") {
		t.Errorf("error should mention git-cliff issue: %v", err)
	}
}

func TestTagUniqueness_EmptyTagUsage(t *testing.T) {
	gitr, _ := newRealGit(t)
	err := appvalidate.TagUniqueness(context.Background(), gitr, &bytes.Buffer{}, appvalidate.TagUniquenessInput{})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("err = %v", err)
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
	if err == nil {
		t.Fatal("expected ahead error")
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
	if err == nil || !strings.Contains(err.Error(), "lightweight tag") {
		t.Errorf("err = %v", err)
	}
}

func TestTagSignature_AnnotatedUnsignedFails(t *testing.T) {
	gitr, ig := newRealGit(t)
	ig.AddTag("v1.0.0", "annotated, unsigned")

	var buf bytes.Buffer
	err := appvalidate.TagSignature(context.Background(), gitr, &buf, output.NewAnnotator(&buf, output.FormatGitHub), appvalidate.TagSignatureInput{Tag: "v1.0.0"})
	if err == nil || !strings.Contains(err.Error(), "is not signed") {
		t.Errorf("err = %v", err)
	}
	for _, want := range []string{"object type: tag", "is annotated", "git tag -s", "cryptographically signed"} {
		if !strings.Contains(err.Error(), want) && !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q\nout:%s\nerr:%v", want, buf.String(), err)
		}
	}
}

func TestTagSignature_EmptyTagUsage(t *testing.T) {
	gitr, _ := newRealGit(t)
	err := appvalidate.TagSignature(context.Background(), gitr, &bytes.Buffer{}, output.NewAnnotator(&bytes.Buffer{}, output.FormatGitHub), appvalidate.TagSignatureInput{})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("err = %v", err)
	}
}

func TestGPGPublicKey_Empty(t *testing.T) {
	t.Parallel()
	err := appvalidate.GPGPublicKey(&bytes.Buffer{}, "")
	if err == nil || !strings.Contains(err.Error(), "missing RELEASE_GPG_PUBLIC_KEY") {
		t.Errorf("err = %v", err)
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
