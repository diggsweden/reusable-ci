//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/adapters/git"
	appversion "github.com/diggsweden/reusable-ci/internal/app/version"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/internal/testutil/isolatedgit"
)

// setupTagAtHEADMinus1 creates a repo, tags HEAD, then adds another commit
// so the tag is at HEAD~1 (the position MoveTag expects).
func setupTagAtHEADMinus1(t *testing.T, tag string) *isolatedgit.Repo {
	t.Helper()
	r := isolatedgit.NewRepo(t)
	r.AddTag(tag, "Release "+tag)
	r.AddFile("CHANGELOG.md", "# v1\n", "chore(release): "+tag)
	r.AddBareRemote()
	// AddBareRemote pushes main but not the tag — push it now so MoveTag's
	// force-push has something to update.
	r.Git("push", "origin", tag)
	return r
}

func TestMoveTag_HappyPath(t *testing.T) {
	r := setupTagAtHEADMinus1(t, "v1.0.0")
	repo := &git.Repo{Dir: r.Dir}
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer
	res, err := appversion.MoveTag(context.Background(), repo,
		appversion.MoveTagInput{Signed: false}, sink, &out)
	if err != nil {
		t.Fatalf("MoveTag: %v", err)
	}
	if res.Tag != "v1.0.0" {
		t.Errorf("res.Tag = %q, want v1.0.0", res.Tag)
	}
	if !strings.Contains(out.String(), "Moving tag v1.0.0") {
		t.Errorf("output missing log line: %q", out.String())
	}

	// release-sha output should be HEAD's SHA
	wantSHA := r.HeadSHA()
	if got := sink.Single("release-sha"); got != wantSHA {
		t.Errorf("release-sha output = %q, want %q", got, wantSHA)
	}

	// After moving, the tag should point at HEAD
	tagSHA := r.Git("rev-list", "-n", "1", "v1.0.0")
	if tagSHA != wantSHA {
		t.Errorf("tag v1.0.0 = %q, want HEAD %q", tagSHA, wantSHA)
	}
}

func TestMoveTag_FailsWhenTagAtUnexpectedCommit(t *testing.T) {
	r := isolatedgit.NewRepo(t)
	r.AddTag("v1.0.0", "Release v1.0.0")
	// Add TWO commits so the tag is at HEAD~2, not HEAD~1.
	r.AddFile("a.md", "a", "first")
	r.AddFile("b.md", "b", "second")

	repo := &git.Repo{Dir: r.Dir}
	_, err := appversion.MoveTag(context.Background(), repo,
		appversion.MoveTagInput{Signed: false}, nil, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error when tag is at HEAD~2")
	}
	if !strings.Contains(err.Error(), "unexpected commit") {
		t.Errorf("error message: %v", err)
	}
}

func TestMoveTag_FailsWithoutTags(t *testing.T) {
	r := isolatedgit.NewRepo(t)
	repo := &git.Repo{Dir: r.Dir}
	_, err := appversion.MoveTag(context.Background(), repo,
		appversion.MoveTagInput{Signed: false}, nil, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error when no tags exist")
	}
}

func TestMoveTag_PushesToRemote(t *testing.T) {
	r := setupTagAtHEADMinus1(t, "v1.2.3")
	repo := &git.Repo{Dir: r.Dir}

	if _, err := appversion.MoveTag(context.Background(), repo,
		appversion.MoveTagInput{Signed: false}, fakeoutputsink.New(t), &bytes.Buffer{}); err != nil {
		t.Fatalf("MoveTag: %v", err)
	}

	// Read back from the remote to confirm the tag was pushed.
	// ls-remote returns the *tag object* SHA for annotated tags, not the
	// commit it peels to. To verify "the tag now points at HEAD", peel
	// the remote tag to its commit via `^{}`.
	wantCommit := r.HeadSHA()
	remoteCommit := strings.TrimSpace(r.Git("ls-remote", "origin", "v1.2.3^{}"))
	if !strings.HasPrefix(remoteCommit, wantCommit) {
		t.Errorf("remote tag commit (peeled) = %q, want prefix %q", remoteCommit, wantCommit)
	}
}

func TestMoveTag_FailureDoesNotEmitReleaseSHA(t *testing.T) {
	r := isolatedgit.NewRepo(t)
	r.AddTag("v1.0.0", "Release v1.0.0")
	r.AddFile("a.md", "a", "first")
	r.AddFile("b.md", "b", "second")

	repo := &git.Repo{Dir: r.Dir}
	sink := fakeoutputsink.New(t)
	_, err := appversion.MoveTag(context.Background(), repo,
		appversion.MoveTagInput{Signed: false}, sink, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error when tag is at HEAD~2")
	}
	if got := sink.Single("release-sha"); got != "" {
		t.Errorf("release-sha should be empty on failure, got %q", got)
	}
}

func TestMoveTag_SucceedsWithoutSink(t *testing.T) {
	r := setupTagAtHEADMinus1(t, "v1.0.0")
	repo := &git.Repo{Dir: r.Dir}

	var out bytes.Buffer
	res, err := appversion.MoveTag(context.Background(), repo,
		appversion.MoveTagInput{Signed: false}, nil, &out)
	if err != nil {
		t.Fatalf("MoveTag: %v", err)
	}
	if res.ReleaseSHA != r.HeadSHA() {
		t.Errorf("release sha = %q, want %q", res.ReleaseSHA, r.HeadSHA())
	}
	if !strings.Contains(out.String(), "Moving tag v1.0.0") {
		t.Errorf("output missing move line: %q", out.String())
	}
}

func TestMoveTag_PrereleaseTagHappyPath(t *testing.T) {
	r := setupTagAtHEADMinus1(t, "v3.0.0-beta.1")
	repo := &git.Repo{Dir: r.Dir}

	var out bytes.Buffer
	res, err := appversion.MoveTag(context.Background(), repo,
		appversion.MoveTagInput{Signed: false}, fakeoutputsink.New(t), &out)
	if err != nil {
		t.Fatalf("MoveTag: %v", err)
	}
	if res.Tag != "v3.0.0-beta.1" {
		t.Errorf("res.Tag = %q", res.Tag)
	}
	if !strings.Contains(out.String(), "Moving tag v3.0.0-beta.1") {
		t.Errorf("output = %q", out.String())
	}
}
