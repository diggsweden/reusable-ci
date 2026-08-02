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

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingit "github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type fakeChangelogReleaseRepo struct {
	cfg          map[string]string
	remoteName   string
	remoteURL    string
	status       string
	added        []string
	commit       domaingit.CommitInput
	pushedBranch string
	tagged       string
	tagSigned    bool
	pushedTag    string
	checkedOut   string
}

func (f *fakeChangelogReleaseRepo) Config(_ context.Context, key, value string) error {
	if f.cfg == nil {
		f.cfg = map[string]string{}
	}

	f.cfg[key] = value

	return nil
}

func (f *fakeChangelogReleaseRepo) SetRemoteURL(_ context.Context, remote, url string) error {
	f.remoteName = remote
	f.remoteURL = url

	return nil
}

func (f *fakeChangelogReleaseRepo) StatusPorcelain(_ context.Context, _ string) (string, error) {
	return f.status, nil
}

func (f *fakeChangelogReleaseRepo) AddPathspecsStrict(_ context.Context, pathspecs []string) error {
	f.added = append([]string{}, pathspecs...)

	return nil
}

func (f *fakeChangelogReleaseRepo) Commit(_ context.Context, in domaingit.CommitInput) error {
	f.commit = in

	return nil
}

func (f *fakeChangelogReleaseRepo) PushBranchNoForce(_ context.Context, branch, _ string) error {
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

func (f *fakeChangelogReleaseRepo) RemoteTagExists(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

func (f *fakeChangelogReleaseRepo) CreateTag(_ context.Context, tag, _ string, signed bool) error {
	f.tagged = tag
	f.tagSigned = signed

	return nil
}

func (f *fakeChangelogReleaseRepo) PushTagNoForce(_ context.Context, tag, _ string) error {
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
		SigningKeyPath: "/tmp/key",
		TagSigned:      true,
	})
	if err != nil {
		t.Fatalf("ChangelogRelease: %v", err)
	}

	if repo.cfg["gpg.format"] != "ssh" || repo.cfg["user.signingkey"] != "/tmp/key" || repo.cfg["commit.gpgsign"] != "true" {
		t.Fatalf("git signing config = %#v", repo.cfg)
	}

	if repo.remoteName != "origin" || repo.remoteURL != "git@codeberg.org:itiquette/example.git" {
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
		Tag:        "v1.2.3",
		Repository: "itiquette/example",
		TagSigned:  true,
		DryRun:     true,
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
			Tag:        "v1.2.3",
			Repository: "itiquette/example",
			DryRun:     true,
		})
		if !errors.Is(err, errs.ErrMissingInput) {
			t.Fatalf("err = %v, want ErrMissingInput", err)
		}
	})

	t.Run("non-stable tag", func(t *testing.T) {
		t.Parallel()

		_, err := appversion.ChangelogRelease(context.Background(), &fakeChangelogReleaseRepo{}, nil, &bytes.Buffer{}, appversion.ChangelogReleaseInput{
			Tag:        "v1.2.3-rc1",
			Repository: "itiquette/example",
			DryRun:     true,
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
		SigningKeyPath: "/tmp/key",
	})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func writeFile(t *testing.T, dir, rel, body string) {
	t.Helper()

	path := filepath.Join(dir, rel)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
