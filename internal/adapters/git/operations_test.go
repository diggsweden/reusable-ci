//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adaptergit "github.com/diggsweden/reusable-ci/internal/adapters/git"
	domaingit "github.com/diggsweden/reusable-ci/internal/domain/git"
	"github.com/diggsweden/reusable-ci/internal/testutil/isolatedgit"
)

func newRepo(t *testing.T) (*adaptergit.Repo, *isolatedgit.Repo) {
	t.Helper()
	ig := isolatedgit.NewRepo(t)
	return &adaptergit.Repo{Dir: ig.Dir}, ig
}

func TestNewAndRunStdin(t *testing.T) {
	r := adaptergit.New()
	out, err := r.RunStdin(context.Background(), "hello", "hash-object", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 40 {
		t.Errorf("hash length = %d, want 40 (%q)", len(out), out)
	}
}

func TestConfig_WritesLocalConfig(t *testing.T) {
	r, ig := newRepo(t)
	if err := r.Config(context.Background(), "ci.test-key", "test-value"); err != nil {
		t.Fatal(err)
	}
	if got := ig.Git("config", "ci.test-key"); got != "test-value" {
		t.Errorf("config = %q, want test-value", got)
	}
}

func TestAddPathspecsAndHasStagedChanges(t *testing.T) {
	r, ig := newRepo(t)
	ctx := context.Background()

	staged, err := r.HasStagedChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if staged {
		t.Fatal("expected clean index")
	}

	r.AddPathspecs(ctx, []string{"missing-file"})
	if err := os.WriteFile(filepath.Join(ig.Dir, "tracked.txt"), []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.AddPathspecs(ctx, []string{"tracked.txt"})

	staged, err = r.HasStagedChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !staged {
		t.Fatal("expected staged changes")
	}
}

func TestCommitAndCommitInfo(t *testing.T) {
	r, ig := newRepo(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(ig.Dir, "release.txt"), []byte("1.2.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.AddPathspecs(ctx, []string{"release.txt"})
	if err := r.Commit(ctx, domaingit.CommitInput{
		Message:     "release: update version",
		AuthorName:  "Release Bot",
		AuthorEmail: "release@example.invalid",
		Signoff:     true,
	}); err != nil {
		t.Fatal(err)
	}

	info, err := r.CommitInfo(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if info.Author != "Release Bot <release@example.invalid>" {
		t.Errorf("author = %q", info.Author)
	}
	if info.Message != "release: update version" {
		t.Errorf("message = %q", info.Message)
	}
	if !strings.Contains(info.Body, "Signed-off-by: Test Bot <bot@example.invalid>") {
		t.Errorf("commit body missing signoff:\n%s", info.Body)
	}
}

func TestRefTagAndPushHelpers(t *testing.T) {
	r, ig := newRepo(t)
	ctx := context.Background()

	first := ig.HeadSHA()
	ig.AddTag("v1.0.0", "release")
	second := ig.AddCommit("second commit")
	if err := r.MoveTag(ctx, "v1.0.0", false); err != nil {
		t.Fatal(err)
	}

	if got, err := r.RevParse(ctx, "HEAD"); err != nil || got != second {
		t.Fatalf("RevParse HEAD = %q, err=%v, want %q", got, err, second)
	}
	if got, err := r.DescribeLatestTag(ctx); err != nil || got != "v1.0.0" {
		t.Fatalf("DescribeLatestTag = %q, err=%v", got, err)
	}
	if got, err := r.TagSHA(ctx, "v1.0.0"); err != nil || got != second {
		t.Fatalf("TagSHA = %q, err=%v, want %q", got, err, second)
	}
	if got, err := r.ShortSHA(ctx, "HEAD", 7); err != nil || got != second[:7] {
		t.Fatalf("ShortSHA = %q, err=%v, want %q", got, err, second[:7])
	}
	tags, err := r.ListTags(ctx, "v1.*")
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0] != "v1.0.0" {
		t.Fatalf("ListTags = %v, want [v1.0.0]", tags)
	}
	if tags, err := r.ListTags(ctx, "missing*"); err != nil || len(tags) != 0 {
		t.Fatalf("ListTags missing = %v, err=%v", tags, err)
	}

	ig.AddBareRemote()
	if err := r.Push(ctx, "HEAD", "main", true); err != nil {
		t.Fatal(err)
	}
	if err := r.PushTag(ctx, "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	remoteTag := ig.Git("ls-remote", "--tags", "origin", "v1.0.0")
	if remoteTag == "" || strings.Contains(remoteTag, first) {
		t.Errorf("remote tag output = %q", remoteTag)
	}
}

func TestTagsPointingAt_FindsCollision(t *testing.T) {
	r, ig := newRepo(t)
	sha := ig.HeadSHA()
	ig.AddTag("v1.0.0", "release v1.0.0")
	ig.AddTag("v1.0.0-alias", "alias")

	tags, err := r.TagsPointingAt(context.Background(), sha)
	if err != nil {
		t.Fatal(err)
	}
	wantSet := map[string]bool{"v1.0.0": false, "v1.0.0-alias": false}
	for _, tag := range tags {
		if _, ok := wantSet[tag]; ok {
			wantSet[tag] = true
		}
	}
	for tag, found := range wantSet {
		if !found {
			t.Errorf("missing tag %q in %v", tag, tags)
		}
	}
}

func TestTagsPointingAt_EmptyWhenNothingMatches(t *testing.T) {
	r, ig := newRepo(t)
	tags, err := r.TagsPointingAt(context.Background(), ig.HeadSHA())
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 {
		t.Errorf("expected empty, got %v", tags)
	}
}

func TestIsAncestor_True(t *testing.T) {
	r, ig := newRepo(t)
	a := ig.HeadSHA()
	b := ig.AddCommit("second commit")
	ok, err := r.IsAncestor(context.Background(), a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("first commit should be ancestor of second")
	}
}

func TestIsAncestor_False(t *testing.T) {
	r, ig := newRepo(t)
	a := ig.HeadSHA()
	b := ig.AddCommit("second commit")
	ok, err := r.IsAncestor(context.Background(), b, a)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("second commit should not be ancestor of first")
	}
}

func TestCatFileType_AnnotatedVsLightweight(t *testing.T) {
	r, ig := newRepo(t)
	ig.AddTag("v1.0.0", "annotated") // annotated
	ig.Git("tag", "v1.0.0-light")    // lightweight (no -a/-m)

	ctx := context.Background()
	annotated, err := r.CatFileType(ctx, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if annotated != "tag" {
		t.Errorf("annotated cat-file -t = %q, want tag", annotated)
	}
	light, err := r.CatFileType(ctx, "v1.0.0-light")
	if err != nil {
		t.Fatal(err)
	}
	if light != "commit" {
		t.Errorf("lightweight cat-file -t = %q, want commit", light)
	}
}

func TestCatFileTag_BodyContainsMessage(t *testing.T) {
	r, ig := newRepo(t)
	ig.AddTag("v1.0.0", "release annotation message")
	body, err := r.CatFileTag(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "release annotation message") {
		t.Errorf("body missing message:\n%s", body)
	}
}

func TestTaggerInfo(t *testing.T) {
	r, ig := newRepo(t)
	ig.AddTag("v1.0.0", "release")
	tagger, date, err := r.TaggerInfo(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tagger, "Test Bot") {
		t.Errorf("tagger = %q (no Test Bot)", tagger)
	}
	if date == "" {
		t.Error("expected non-empty tag date")
	}
}

func TestTagMessage_StripsLeadingTagColumn(t *testing.T) {
	r, ig := newRepo(t)
	ig.AddTag("v1.0.0", "first line\nsecond line")
	msg, err := r.TagMessage(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "first line") {
		t.Errorf("missing first line in: %q", msg)
	}
	// `git tag -l -n999` may collapse newlines into a single line; we just
	// verify the leading "<tag>" column is gone.
	if strings.HasPrefix(msg, "v1.0.0 ") {
		t.Errorf("tag column not stripped: %q", msg)
	}
}

func TestVerifyTagSignature_NoArmorReturnsNotOk(t *testing.T) {
	r, ig := newRepo(t)
	ig.AddTag("v1.0.0", "annotated, unsigned")
	signer, fingerprint, ok, err := r.VerifyTagSignature(context.Background(), "v1.0.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ok || signer != "" || fingerprint != "" {
		t.Errorf("expected (ok=false, signer=\"\", fp=\"\") with no armor, got (%v, %q, %q)", ok, signer, fingerprint)
	}
}

func TestVerifyTagSignature_UnsignedTagReturnsNotOk(t *testing.T) {
	r, ig := newRepo(t)
	ig.AddTag("v1.0.0", "annotated, unsigned")
	armor := []byte(`-----BEGIN PGP PUBLIC KEY BLOCK-----

xj0EZGZGZBYJKwYBBAHaRw8BAQdAv/` + `/junk
-----END PGP PUBLIC KEY BLOCK-----`)
	_, _, ok, err := r.VerifyTagSignature(context.Background(), "v1.0.0", armor)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Errorf("unsigned tag should not verify; got ok=true")
	}
}

func TestVerifyTagSignature_NonExistentTagErrors(t *testing.T) {
	r, _ := newRepo(t)
	_, _, _, err := r.VerifyTagSignature(context.Background(), "nope", []byte("anything"))
	if err == nil {
		t.Error("expected error for missing tag")
	}
}
