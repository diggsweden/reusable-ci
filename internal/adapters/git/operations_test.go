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

	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/version"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
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

	// CreateTag is create-once at the given ref (HEAD here).
	if err := r.CreateTag(ctx, "v1.1.0", "HEAD", false); err != nil {
		t.Fatal(err)
	}

	// Creating the same tag again must error — no -f, a tag is never moved.
	if err := r.CreateTag(ctx, "v1.1.0", "HEAD", false); err == nil {
		t.Fatal("CreateTag of an existing tag must error (create-once)")
	}

	if got, err := r.TagExists(ctx, "v1.1.0"); err != nil || !got {
		t.Fatalf("TagExists(v1.1.0) = %v, err=%v, want true", got, err)
	}
	if got, err := r.TagExists(ctx, "v9.9.9"); err != nil || got {
		t.Fatalf("TagExists(v9.9.9) = %v, err=%v, want false", got, err)
	}

	if got, err := r.RevParse(ctx, "HEAD"); err != nil || got != second {
		t.Fatalf("RevParse HEAD = %q, err=%v, want %q", got, err, second)
	}
	if got, err := r.DescribeLatestTag(ctx); err != nil || got != "v1.1.0" {
		t.Fatalf("DescribeLatestTag = %q, err=%v", got, err)
	}
	if got, err := r.TagSHA(ctx, "v1.1.0"); err != nil || got != second {
		t.Fatalf("TagSHA = %q, err=%v, want %q", got, err, second)
	}
	if got, err := r.ShortSHA(ctx, "HEAD", 7); err != nil || got != second[:7] {
		t.Fatalf("ShortSHA = %q, err=%v, want %q", got, err, second[:7])
	}
	tags, err := r.ListTags(ctx, "v1.*")
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 {
		t.Fatalf("ListTags = %v, want 2 tags (v1.0.0, v1.1.0)", tags)
	}
	if tags, err := r.ListTags(ctx, "missing*"); err != nil || len(tags) != 0 {
		t.Fatalf("ListTags missing = %v, err=%v", tags, err)
	}

	ig.AddBareRemote()
	if err := r.Push(ctx, "HEAD", "main", true, runcontext.Credential{}); err != nil {
		t.Fatal(err)
	}
	if err := r.PushTagNoForce(ctx, "v1.1.0", runcontext.Credential{}); err != nil {
		t.Fatal(err)
	}
	remoteTag := ig.Git("ls-remote", "--tags", "origin", "v1.1.0")
	if remoteTag == "" || strings.Contains(remoteTag, first) {
		t.Errorf("remote tag output = %q", remoteTag)
	}
}

// TestRemoteURL returns origin's configured URL, which Push scopes its
// transient auth header to.
func TestRemoteURL(t *testing.T) {
	ctx := context.Background()

	r, ig := newRepo(t)
	remote := ig.AddBareRemote()

	got, err := r.RemoteURL(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if got != remote {
		t.Errorf("RemoteURL = %q, want %q", got, remote)
	}
}

// TestPush_TokenAuthNotPersisted is the write-side credential-free guarantee:
// a token authenticates the push (sent as a transient header scoped to
// origin), but nothing about it may survive in .git/config — the same promise
// the fetch side makes. The local bare remote ignores the HTTP header, so both
// pushes still succeed; the assertion is purely about non-persistence.
func TestPush_TokenAuthNotPersisted(t *testing.T) {
	ctx := context.Background()

	r, ig := newRepo(t)
	ig.AddBareRemote()
	ig.AddCommit("second")
	ig.AddTag("v2.0.0", "two")

	if err := r.Push(ctx, "HEAD", "main", true, runcontext.OperatorCredential("secret-token")); err != nil {
		t.Fatalf("branch push with token: %v", err)
	}

	if err := r.PushTagNoForce(ctx, "v2.0.0", runcontext.OperatorCredential("secret-token")); err != nil {
		t.Fatalf("tag push with token: %v", err)
	}

	cfg, err := os.ReadFile(filepath.Join(ig.Dir, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}

	for _, leak := range []string{"extraheader", "Authorization", "secret-token", "x-access-token"} {
		if strings.Contains(string(cfg), leak) {
			t.Errorf(".git/config leaked %q — the push auth header must stay transient", leak)
		}
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

// go-git cannot open sha256 repositories ("does not support extension:
// objectformat"), so IsAncestor must fall back to `git merge-base
// --is-ancestor` — the v0.8.6 same-version release recovery broke on
// this against a live sha256 consumer repo.
func TestIsAncestor_SHA256RepoFallsBackToSubprocess(t *testing.T) {
	ctx := context.Background()
	testenv.New(t)
	dir := t.TempDir()
	r := &adaptergit.Repo{Dir: dir}
	if err := r.InitWithObjectFormat(ctx, "sha256"); err != nil {
		t.Fatal(err)
	}
	mustRun := func(args ...string) string {
		t.Helper()
		out, err := r.Run(ctx, args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return out
	}
	mustRun("config", "user.name", "Test Bot")
	mustRun("config", "user.email", "bot@example.invalid")
	mustRun("config", "commit.gpgsign", "false")
	mustRun("commit", "--allow-empty", "-m", "first commit")
	a := mustRun("rev-parse", "HEAD")
	mustRun("commit", "--allow-empty", "-m", "second commit")
	b := mustRun("rev-parse", "HEAD")

	ok, err := r.IsAncestor(ctx, a, b)
	if err != nil {
		t.Fatalf("IsAncestor on sha256 repo: %v", err)
	}
	if !ok {
		t.Error("first commit should be ancestor of second")
	}
	ok, err = r.IsAncestor(ctx, b, a)
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
	info, err := r.TaggerInfo(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(info.Tagger, "Test Bot") {
		t.Errorf("tagger = %q (no Test Bot)", info.Tagger)
	}
	if info.Date == "" {
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

// TestAddPathspecs_StagesPresentDespiteMissing is the regression guard for
// the batched-add bug: `git add -- a b c` aborts the whole invocation when
// any one pathspec matches nothing, staging none of the others. A real
// Kotlin-DSL Gradle project hits this on every version bump, because
// version.FilePattern(Gradle) names files that project does not have.
//
// The pre-existing AddPathspecs test passed while the bug shipped because
// it only ever called with all-missing or all-present pathspecs. The mix
// is the case that matters.
func TestAddPathspecs_StagesPresentDespiteMissing(t *testing.T) {
	r, ig := newRepo(t)
	ctx := context.Background()

	// A Kotlin-DSL-only worktree: no build.gradle, no gradle.properties.
	for _, name := range []string{"CHANGELOG.md", "build.gradle.kts", "settings.gradle.kts"} {
		if err := os.WriteFile(filepath.Join(ig.Dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The real production pattern, not a hand-picked one.
	r.AddPathspecs(ctx, strings.Fields(version.FilePattern(projecttype.Gradle)))

	staged := ig.Git("diff", "--cached", "--name-only")
	for _, want := range []string{"CHANGELOG.md", "build.gradle.kts", "settings.gradle.kts"} {
		if !strings.Contains(staged, want) {
			t.Errorf("staged = %q, want it to contain %q", staged, want)
		}
	}
}
