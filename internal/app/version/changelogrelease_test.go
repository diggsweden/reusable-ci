// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type fakeChangelogReleaseRepo struct {
	cfg               map[string]string
	remoteName        string
	remoteURL         string
	status            string
	added             []string
	commit            domaingit.CommitInput
	pushedBranch      string
	tagged            string
	tagSigned         bool
	pushedTag         string
	checkedOut        string
	calls             []string
	destinationChecks int
	destinationFailAt int
}

func (f *fakeChangelogReleaseRepo) CheckOriginPushDestination(_ context.Context) error {
	f.calls = append(f.calls, "destination")

	f.destinationChecks++
	if f.destinationChecks == f.destinationFailAt {
		return errs.ErrValidation
	}

	return nil
}

func (f *fakeChangelogReleaseRepo) Config(_ context.Context, key, value string) error {
	f.calls = append(f.calls, "config:"+key)
	if f.cfg == nil {
		f.cfg = map[string]string{}
	}

	f.cfg[key] = value

	return nil
}

func (f *fakeChangelogReleaseRepo) SetRemoteURL(_ context.Context, remote, url string) error {
	f.calls = append(f.calls, "set-url")
	f.remoteName = remote
	f.remoteURL = url

	return nil
}

func (f *fakeChangelogReleaseRepo) StatusPorcelain(_ context.Context, _ string) (string, error) {
	f.calls = append(f.calls, "status")

	return f.status, nil
}

func (f *fakeChangelogReleaseRepo) AddPathspecsStrict(_ context.Context, pathspecs []string) error {
	f.calls = append(f.calls, "add")
	f.added = append([]string{}, pathspecs...)

	return nil
}

func (f *fakeChangelogReleaseRepo) Commit(_ context.Context, in domaingit.CommitInput) error {
	f.calls = append(f.calls, "commit")
	f.commit = in

	return nil
}

func (f *fakeChangelogReleaseRepo) PushBranchNoForce(_ context.Context, branch string, _ runcontext.Credential) error {
	f.calls = append(f.calls, "push-branch")
	f.pushedBranch = branch

	return nil
}

func (f *fakeChangelogReleaseRepo) Checkout(_ context.Context, ref string) error {
	f.checkedOut = ref

	return nil
}

func (f *fakeChangelogReleaseRepo) TagExists(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (f *fakeChangelogReleaseRepo) TagSHA(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (f *fakeChangelogReleaseRepo) RemoteTagCommitIfExists(_ context.Context, repository, _ string, _ runcontext.Credential) (string, bool, error) {
	f.calls = append(f.calls, "remote-tag:"+repository)

	return "", false, nil
}

func (f *fakeChangelogReleaseRepo) RemoteTagObjectIfExists(_ context.Context, _, _ string, _ runcontext.Credential) (string, bool, error) {
	return "", false, nil
}

func (f *fakeChangelogReleaseRepo) CatFileType(_ context.Context, _ string) (string, error) {
	return "tag", nil
}

func (f *fakeChangelogReleaseRepo) VerifyConfiguredTagSignature(_ context.Context, _ string) error {
	return nil
}

func (f *fakeChangelogReleaseRepo) CreateTag(_ context.Context, tag, _ string, signed bool) error {
	f.tagged = tag
	f.tagSigned = signed

	return nil
}

func (f *fakeChangelogReleaseRepo) PushTagNoForce(_ context.Context, tag string, _ runcontext.Credential) error {
	f.pushedTag = tag

	return nil
}

func (f *fakeChangelogReleaseRepo) RevParse(_ context.Context, _ string) (string, error) {
	return "0123456789abcdef0123456789abcdef01234567", nil
}

func TestChangelogRelease_CommitsPushesTagsAndChecksOut(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CHANGELOG.md", "# v1.2.3\n")
	writeFile(t, dir, "commit-msg.txt", "chore(release): bump to v1.2.3\n")
	t.Chdir(dir)

	repo := &fakeChangelogReleaseRepo{status: " M CHANGELOG.md"}

	var out bytes.Buffer

	res, err := appversion.ChangelogRelease(context.Background(), repo, fakeoutputsink.New(t), &out, appversion.ChangelogReleaseInput{
		Tag:            "v1.2.3",
		Repository:     "itiquette/example",
		GitURL:         "ssh://git@codeberg.org:2222/itiquette/example.git",
		AuthorName:     "Itiquette Release Bot",
		AuthorEmail:    "itiquette-release-bot@pm.me",
		SigningKeyPath: "/tmp/key",
		TagSigned:      true,
	})
	if err != nil {
		t.Fatalf("ChangelogRelease: %v", err)
	}

	if repo.cfg["gpg.format"] != "ssh" || repo.cfg["user.signingkey"] != "/tmp/key" || repo.cfg["commit.gpgsign"] != "true" {
		t.Fatalf("git signing config = %#v", repo.cfg)
	}

	if repo.remoteName != "origin" || repo.remoteURL != "ssh://git@codeberg.org:2222/itiquette/example.git" {
		t.Fatalf("remote = (%q,%q)", repo.remoteName, repo.remoteURL)
	}

	if len(repo.added) != 1 || repo.added[0] != "CHANGELOG.md" {
		t.Fatalf("added = %v", repo.added)
	}

	if repo.commit.MessageFile != "commit-msg.txt" || !repo.commit.Sign || !repo.commit.Signoff || !repo.commit.NoVerify || !repo.commit.NoHooks {
		t.Fatalf("commit input = %+v", repo.commit)
	}

	if repo.pushedBranch != "main" || repo.tagged != "v1.2.3" || !repo.tagSigned || repo.pushedTag != "v1.2.3" || repo.checkedOut != "v1.2.3" {
		t.Fatalf("repo after release = %+v", repo)
	}

	if res.ReleaseSHA != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("release sha = %q", res.ReleaseSHA)
	}

	if !bytes.Contains(out.Bytes(), []byte("CHANGELOG.md committed")) {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestChangelogRelease_UnchangedSkipsCommitButTags(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CHANGELOG.md", "# unchanged\n")
	t.Chdir(dir)

	repo := &fakeChangelogReleaseRepo{}

	_, err := appversion.ChangelogRelease(context.Background(), repo, nil, &bytes.Buffer{}, appversion.ChangelogReleaseInput{
		Tag:            "v1.2.3",
		Repository:     "itiquette/example",
		GitURL:         "ssh://git@codeberg.org/itiquette/example.git",
		AuthorName:     "Itiquette Release Bot",
		AuthorEmail:    "itiquette-release-bot@pm.me",
		SigningKeyPath: "/tmp/key",
		TagSigned:      false,
	})
	if err != nil {
		t.Fatalf("ChangelogRelease: %v", err)
	}

	if repo.commit.MessageFile != "" || repo.pushedBranch != "" {
		t.Fatalf("unchanged changelog should not commit/push branch: %+v", repo)
	}

	if repo.tagged != "v1.2.3" || repo.tagSigned {
		t.Fatalf("tag creation = (%q,%v), want unsigned v1.2.3", repo.tagged, repo.tagSigned)
	}
}

func TestChangelogRelease_DryRunSkipsGitMutationsAndNarrates(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CHANGELOG.md", "# v1.2.3\n")
	writeFile(t, dir, "commit-msg.txt", "chore(release): bump to v1.2.3\n")
	t.Chdir(dir)

	repo := &fakeChangelogReleaseRepo{status: " M CHANGELOG.md"}

	var out bytes.Buffer

	// No SigningKeyPath: the signing key only serves the skipped push, so
	// a dry-run preview may run without one.
	res, err := appversion.ChangelogRelease(context.Background(), repo, fakeoutputsink.New(t), &out, appversion.ChangelogReleaseInput{
		Tag:         "v1.2.3",
		Repository:  "itiquette/example",
		GitURL:      "ssh://git@codeberg.org/itiquette/example.git",
		AuthorName:  "Itiquette Release Bot",
		AuthorEmail: "itiquette-release-bot@pm.me",
		TagSigned:   true,
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("ChangelogRelease: %v", err)
	}

	// (a) No git mutation is invoked: config, remote, stage, commit,
	// branch push, tag create/push, checkout all skipped.
	if len(repo.cfg) != 0 || repo.remoteURL != "" {
		t.Errorf("dry-run must not touch git config/remote: cfg=%v remote=%q", repo.cfg, repo.remoteURL)
	}

	if len(repo.added) != 0 || repo.commit.MessageFile != "" || repo.pushedBranch != "" {
		t.Errorf("dry-run must not stage/commit/push: %+v", repo)
	}

	if repo.tagged != "" || repo.pushedTag != "" || repo.checkedOut != "" {
		t.Errorf("dry-run must not tag/checkout: %+v", repo)
	}

	// (b) Each skipped mutation is narrated.
	for _, want := range []string{
		"[dry-run] skipping git signing config and SSH remote setup (commit and push are skipped)",
		"[dry-run] would stage CHANGELOG.md",
		"[dry-run] would create the signed changelog commit from commit-msg.txt",
		"[dry-run] would push main to origin (no force)",
		"[dry-run] would create signed tag v1.2.3 at HEAD",
		"[dry-run] would push tag v1.2.3 to origin (no force)",
		"[dry-run] would checkout v1.2.3",
	} {
		if !bytes.Contains(out.Bytes(), []byte(want)) {
			t.Errorf("output %q missing narration %q", out.String(), want)
		}
	}

	if res.ReleaseSHA != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("release sha = %q, want the HEAD the tag would point at", res.ReleaseSHA)
	}
}

// TestChangelogRelease_DryRunStillFailsValidation proves the preview keeps
// the full validation surface: file preconditions and tag shape still fail.
func TestChangelogRelease_DryRunStillFailsValidation(t *testing.T) {
	t.Run("missing commit message file", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "CHANGELOG.md", "# changed\n")
		t.Chdir(dir)

		_, err := appversion.ChangelogRelease(context.Background(), &fakeChangelogReleaseRepo{status: " M CHANGELOG.md"}, nil, &bytes.Buffer{}, appversion.ChangelogReleaseInput{
			Tag:         "v1.2.3",
			Repository:  "itiquette/example",
			GitURL:      "ssh://git@codeberg.org/itiquette/example.git",
			AuthorName:  "Itiquette Release Bot",
			AuthorEmail: "itiquette-release-bot@pm.me",
			DryRun:      true,
		})
		if !errors.Is(err, errs.ErrMissingInput) {
			t.Fatalf("err = %v, want ErrMissingInput", err)
		}
	})

	t.Run("non-stable tag", func(t *testing.T) {
		t.Parallel()

		_, err := appversion.ChangelogRelease(context.Background(), &fakeChangelogReleaseRepo{}, nil, &bytes.Buffer{}, appversion.ChangelogReleaseInput{
			Tag:         "v1.2.3-rc1",
			Repository:  "itiquette/example",
			GitURL:      "ssh://git@codeberg.org/itiquette/example.git",
			AuthorName:  "Itiquette Release Bot",
			AuthorEmail: "itiquette-release-bot@pm.me",
			DryRun:      true,
		})
		if !errors.Is(err, errs.ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
	})
}

func TestChangelogRelease_RequiresCommitMessageWhenChangelogChanged(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "CHANGELOG.md", "# changed\n")
	t.Chdir(dir)

	_, err := appversion.ChangelogRelease(context.Background(), &fakeChangelogReleaseRepo{status: " M CHANGELOG.md"}, nil, &bytes.Buffer{}, appversion.ChangelogReleaseInput{
		Tag:            "v1.2.3",
		Repository:     "itiquette/example",
		GitURL:         "ssh://git@codeberg.org/itiquette/example.git",
		AuthorName:     "Itiquette Release Bot",
		AuthorEmail:    "itiquette-release-bot@pm.me",
		SigningKeyPath: "/tmp/key",
	})
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("err = %v, want ErrMissingInput", err)
	}
}

func TestChangelogRelease_RequiresStableTag(t *testing.T) {
	t.Parallel()

	_, err := appversion.ChangelogRelease(context.Background(), &fakeChangelogReleaseRepo{}, nil, &bytes.Buffer{}, appversion.ChangelogReleaseInput{
		Tag:            "v1.2.3-rc1",
		Repository:     "itiquette/example",
		GitURL:         "ssh://git@codeberg.org/itiquette/example.git",
		AuthorName:     "Itiquette Release Bot",
		AuthorEmail:    "itiquette-release-bot@pm.me",
		SigningKeyPath: "/tmp/key",
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestChangelogBinding_RejectsUnsupportedRemoteAndBranchBeforeEffects(t *testing.T) {
	t.Parallel()

	for _, dry := range []bool{false, true} {
		for _, bad := range []string{"upstream", "--mirror", "+main", "+main:other", "main:other"} {
			in := appversion.ChangelogReleaseInput{Tag: "v1.2.3", Repository: "owner/repo", GitURL: "ssh://git@forge.example/owner/repo.git", AuthorName: "Fixture", AuthorEmail: "fixture@example.invalid", SigningKeyPath: "/fixture-key", DryRun: dry}
			if bad == "upstream" {
				in.RemoteName = bad
			} else {
				in.Branch = bad
			}

			repo := &fakeChangelogReleaseRepo{}
			sink := fakeoutputsink.New(t)

			var out bytes.Buffer

			require.ErrorIs(t, appversion.ChangelogReleasePreflight(in), errs.ErrUsage)
			res, err := appversion.ChangelogRelease(t.Context(), repo, sink, &out, in)
			require.ErrorIs(t, err, errs.ErrUsage)
			require.Nil(t, res)
			require.Empty(t, repo.calls)
			require.Empty(t, out.String())
			require.Empty(t, sink.Keys())
		}
	}
}

func TestChangelogBinding_DestinationCheckedBeforeEffectsAndAfterSetup(t *testing.T) {
	for _, failAt := range []int{1, 2, 0} {
		dir := t.TempDir()
		t.Chdir(dir)
		writeFile(t, dir, "CHANGELOG.md", "# changed\n")
		writeFile(t, dir, "commit-msg.txt", "chore(release): bump to v1.2.3\n")

		in := appversion.ChangelogReleaseInput{Tag: "v1.2.3", Repository: "owner/repo", GitURL: "ssh://git@forge.example/owner/repo.git", AuthorName: "Fixture", AuthorEmail: "fixture@example.invalid", SigningKeyPath: "/fixture-key", Token: runcontext.OperatorCredential("fixture-token")}
		repo := &fakeChangelogReleaseRepo{status: " M CHANGELOG.md", destinationFailAt: failAt}
		sink := fakeoutputsink.New(t)

		var out bytes.Buffer

		res, err := appversion.ChangelogRelease(t.Context(), repo, sink, &out, in)

		want := []string{"destination"}
		if failAt != 1 {
			want = append(want, "status", "remote-tag:"+in.GitURL, "config:user.name", "config:user.email", "config:gpg.format", "config:user.signingkey", "config:commit.gpgsign", "set-url", "destination")
			// No local config rollback is promised after the second refusal.
			require.Equal(t, in.GitURL, repo.remoteURL)
			require.Equal(t, in.AuthorName, repo.cfg["user.name"])
		} else {
			require.Empty(t, repo.cfg)
			require.Empty(t, repo.remoteURL)
		}

		if failAt != 0 {
			require.ErrorIs(t, err, errs.ErrValidation)
			require.Nil(t, res)
			require.Empty(t, repo.added)
			require.Empty(t, repo.commit.MessageFile)
			require.Empty(t, repo.pushedBranch)
			require.Empty(t, repo.tagged)
			require.Empty(t, sink.Keys())
			require.Empty(t, out.String())
		} else {
			require.NoError(t, err)
			require.NotNil(t, res)

			want = append(want, "add", "commit", "push-branch", "destination", "remote-tag:origin")

			require.Equal(t, "main", repo.pushedBranch)
		}

		require.Equal(t, want, repo.calls)
	}
}

func TestParseChangelogReleaseGitURL_AcceptsOnlyPasswordlessSSHGitURLs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		rawURL     string
		repository string
		wantHost   string
		wantPort   int
		wantErr    bool
	}{
		{name: "default port", rawURL: "ssh://git@git.example.test/owner/repository.git", repository: "owner/repository", wantHost: "git.example.test", wantPort: 22},
		{name: "explicit port", rawURL: "ssh://git@git.example.test:2223/owner/repository.git", repository: "owner/repository", wantHost: "git.example.test", wantPort: 2223},
		{name: "scp form", rawURL: "git@git.example.test:owner/repository.git", repository: "owner/repository", wantErr: true},
		{name: "wrong scheme", rawURL: "https://git@git.example.test/owner/repository.git", repository: "owner/repository", wantErr: true},
		{name: "wrong user", rawURL: "ssh://deploy@git.example.test/owner/repository.git", repository: "owner/repository", wantErr: true},
		{name: "password", rawURL: "ssh://git:secret@git.example.test/owner/repository.git", repository: "owner/repository", wantErr: true}, //nolint:gosec // Synthetic password-bearing URL verifies rejection.
		{name: "missing hostname", rawURL: "ssh://git@/owner/repository.git", repository: "owner/repository", wantErr: true},
		{name: "empty explicit port", rawURL: "ssh://git@git.example.test:/owner/repository.git", repository: "owner/repository", wantErr: true},
		{name: "nonnumeric port", rawURL: "ssh://git@git.example.test:ssh/owner/repository.git", repository: "owner/repository", wantErr: true},
		{name: "port zero", rawURL: "ssh://git@git.example.test:0/owner/repository.git", repository: "owner/repository", wantErr: true},
		{name: "port too large", rawURL: "ssh://git@git.example.test:65536/owner/repository.git", repository: "owner/repository", wantErr: true},
		{name: "query", rawURL: "ssh://git@git.example.test/owner/repository.git?ref=main", repository: "owner/repository", wantErr: true},
		{name: "fragment", rawURL: "ssh://git@git.example.test/owner/repository.git#main", repository: "owner/repository", wantErr: true},
		{name: "wrong path", rawURL: "ssh://git@git.example.test/owner/other.git", repository: "owner/repository", wantErr: true},
		{name: "encoded path", rawURL: "ssh://git@git.example.test/owner/repos%69tory.git", repository: "owner/repository", wantErr: true},
		{name: "ambiguous repository", rawURL: "ssh://git@git.example.test/owner/team/repository.git", repository: "owner/team/repository", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := appversion.ParseChangelogReleaseGitURL(tc.rawURL, tc.repository)
			if tc.wantErr {
				if !errors.Is(err, errs.ErrValidation) {
					t.Fatalf("err = %v, want ErrValidation", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("ParseChangelogReleaseGitURL: %v", err)
			}

			if got.Host != tc.wantHost || got.Port != tc.wantPort {
				t.Errorf("endpoint = %+v, want host %q port %d", got, tc.wantHost, tc.wantPort)
			}
		})
	}
}

func writeFile(t *testing.T, dir, rel, body string) {
	t.Helper()

	path := filepath.Join(dir, rel)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
