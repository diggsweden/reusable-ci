//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	gocrypto "github.com/ProtonMail/go-crypto/openpgp"

	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
)

func newRepo(t *testing.T) (*adaptergit.Repo, *isolatedgit.Repo) {
	t.Helper()
	ig := isolatedgit.NewRepo(t)
	return &adaptergit.Repo{Dir: ig.Dir}, ig
}

func TestRunStdin_PipesInputToGit(t *testing.T) {
	r := adaptergit.New()
	out, err := r.RunStdin(context.Background(), "hello", "hash-object", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	// The blob sha of exactly "hello". A length check would also pass if
	// stdin never reached git and it hashed an empty object.
	if want := "b6fc4c620b67d95f953a5c1c1230aaab5db5a1b0"; out != want {
		t.Errorf("hash-object --stdin = %q, want %q (the blob sha of %q)", out, want, "hello")
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

func TestAddPathspecsAndHasStagedChanges_ReflectTheIndex(t *testing.T) {
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

func TestCommitAndCommitInfo_RoundTripAuthorMessageAndSignoff(t *testing.T) {
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

// TestCreateTag_RefusesToMoveAnExistingTag pins the create-once rule. The
// adapter never passes -f, so re-tagging is an error rather than a silent
// move: a released tag that changes what it points at invalidates every
// signature and attestation already made against it.
//
// Extracted from the helper smoke test below, where it was one assertion among
// a dozen and any earlier failure would have skipped it.
func TestCreateTag_RefusesToMoveAnExistingTag(t *testing.T) {
	r, _ := newRepo(t)
	ctx := context.Background()

	if err := r.CreateTag(ctx, "v1.1.0", "HEAD", false); err != nil {
		t.Fatal(err)
	}

	if err := r.CreateTag(ctx, "v1.1.0", "HEAD", false); err == nil {
		t.Error("CreateTag of an existing tag must error; a tag is never moved")
	}
}

func TestRefTagAndPushHelpers_AgreeAfterATagAndPush(t *testing.T) {
	r, ig := newRepo(t)
	ctx := context.Background()

	first := ig.HeadSHA()
	ig.AddTag("v1.0.0", "release")
	second := ig.AddCommit("second commit")

	if err := r.CreateTag(ctx, "v1.1.0", "HEAD", false); err != nil {
		t.Fatal(err)
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
	// Unpatterned, so the peeled line is listed too: the annotated tag
	// object's own sha says nothing about which commit was published, and
	// only the "^{}" entry does.
	remoteTag := ig.Git("ls-remote", "--tags", "origin")
	if !strings.Contains(remoteTag, second+"\trefs/tags/v1.1.0^{}") {
		t.Errorf("remote tag does not peel to the tagged commit %q:\n%s", second, remoteTag)
	}

	if strings.Contains(remoteTag, first+"\trefs/tags/v1.1.0") {
		t.Errorf("remote v1.1.0 names the first commit:\n%s", remoteTag)
	}
}

// TestRemoteURL_ReturnsOriginsConfiguredURL returns origin's configured URL, which Push scopes its
// transient auth header to.
func TestRemoteURL_ReturnsOriginsConfiguredURL(t *testing.T) {
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
	ig.Git("tag", "v1.0.0-alias")

	tags, err := r.TagsPointingAt(context.Background(), sha)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"v1.0.0", "v1.0.0-alias"} {
		if !slices.Contains(tags, want) {
			t.Errorf("missing tag %q in %v", want, tags)
		}
	}
}

func TestTagsPointingAt_IncludesAnnotatedReleaseRequestTag(t *testing.T) {
	r, ig := newRepo(t)
	sha := ig.HeadSHA()
	ig.AddTag("release-request/v1.2.3", "authorise v1.2.3")

	tags, err := r.TagsPointingAt(context.Background(), sha)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tags, "release-request/v1.2.3") {
		t.Fatalf("annotated release request missing from %v", tags)
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

func TestTaggerInfo_ReturnsTheTaggerAndDate(t *testing.T) {
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

// validPublicArmor is a well-formed public keyring, so the verification
// outcome under test is reached instead of the keyring parse refusing first.
func validPublicArmor(t *testing.T) []byte {
	t.Helper()

	entity, err := gocrypto.NewEntity("Release Bot", "ci", "bot@example.invalid", nil)
	if err != nil {
		t.Fatalf("NewEntity: %v", err)
	}

	return publicArmor(t, entity)
}

// TestVerifyTagSignature_UnsignedTagReturnsNotOk: an annotated but unsigned
// tag verifies as not ok without an error. These two cases used a junk keyring
// and had been failing on the keyring refusal since keyrings were validated
// first, so neither outcome was being tested.
func TestVerifyTagSignature_UnsignedTagReturnsNotOk(t *testing.T) {
	r, ig := newRepo(t)
	ig.AddTag("v1.0.0", "annotated, unsigned")

	signer, fingerprint, ok, err := r.VerifyTagSignature(context.Background(), "v1.0.0", validPublicArmor(t))
	if err != nil {
		t.Fatal(err)
	}

	if ok || signer != "" || fingerprint != "" {
		t.Errorf("unsigned tag verified as (%v, %q, %q), want not ok", ok, signer, fingerprint)
	}
}

func TestVerifyTagSignature_NonExistentTagErrors(t *testing.T) {
	r, _ := newRepo(t)

	signer, fingerprint, ok, err := r.VerifyTagSignature(context.Background(), "nope", validPublicArmor(t))
	if !errors.Is(err, errs.ErrValidation) || ok || signer != "" || fingerprint != "" {
		t.Errorf("got (%v, %q, %q, %v), want not ok with ErrValidation for a tag that does not exist", ok, signer, fingerprint, err)
	}
}

// TestVerifyTagSignature_MalformedKeyringIsMalformedInput keeps the refusal
// the two tests above used to trip over by accident, as its own case.
func TestVerifyTagSignature_MalformedKeyringIsMalformedInput(t *testing.T) {
	r, ig := newRepo(t)
	ig.AddTag("v1.0.0", "annotated, unsigned")

	signer, fingerprint, ok, err := r.VerifyTagSignature(context.Background(), "v1.0.0", []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\n\njunk\n-----END PGP PUBLIC KEY BLOCK-----"))
	if !errors.Is(err, errs.ErrMalformedInput) || ok || signer != "" || fingerprint != "" {
		t.Errorf("got (%v, %q, %q, %v), want not ok with ErrMalformedInput", ok, signer, fingerprint, err)
	}
}

// TestTagMetadata_ExactMessagesTaggersAndTagSets pins the values the tag
// readers return, which the tests above only probed with Contains and
// non-empty checks: a multi-line annotation must come back line for line, the
// tagger as name and address with a date taken from the fixed committer date
// (a lightweight tag has neither), and the tag lists must hold exactly the
// matching names -- a pattern excludes other tags, and a tag on another commit
// is not "pointing at" this one.
func TestTagMetadata_ExactMessagesTaggersAndTagSets(t *testing.T) {
	r, ig := newRepo(t)
	ctx := context.Background()

	old := ig.HeadSHA()
	ig.Git("tag", "v0.9.0")

	head := ig.AddCommit("second")

	t.Setenv("GIT_COMMITTER_DATE", "2026-03-04T05:06:07+02:00")
	ig.AddTag("v1.0.0", "Release 1.0.0\n\nfirst paragraph line\n  indented line")
	ig.AddTag("release-request/v1.0.0", "authorise")
	ig.Git("tag", "v1.0.0-alias")

	msg, err := r.TagMessage(ctx, "v1.0.0")
	if err != nil || msg != "Release 1.0.0\n\nfirst paragraph line\n  indented line" {
		t.Errorf("message = %q (err %v)", msg, err)
	}

	info, err := r.TaggerInfo(ctx, "v1.0.0")
	if err != nil || info.Tagger != "Test Bot <bot@example.invalid>" || info.Date != "2026-03-04 05:06:07 +0200" {
		t.Errorf("tagger = %+v (err %v)", info, err)
	}

	if light, lightErr := r.TaggerInfo(ctx, "v1.0.0-alias"); lightErr != nil || light != (domaingit.TaggerInfo{}) {
		t.Errorf("lightweight tagger = %+v (err %v), want empty", light, lightErr)
	}

	if tags, listErr := r.ListTags(ctx, "v1*"); listErr != nil || !slices.Equal(tags, []string{"v1.0.0", "v1.0.0-alias"}) {
		t.Errorf("ListTags(v1*) = %q (err %v)", tags, listErr)
	}

	pointing, err := r.TagsPointingAt(ctx, head)
	if err != nil {
		t.Fatal(err)
	}

	slices.Sort(pointing)

	if want := []string{"release-request/v1.0.0", "v1.0.0", "v1.0.0-alias"}; !slices.Equal(pointing, want) {
		t.Errorf("TagsPointingAt(head) = %q, want %q", pointing, want)
	}

	if older, olderErr := r.TagsPointingAt(ctx, old); olderErr != nil || !slices.Equal(older, []string{"v0.9.0"}) {
		t.Errorf("TagsPointingAt(old) = %q (err %v)", older, olderErr)
	}
}
