//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package isolatedgit_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
)

func TestNewRepo_HasInitialCommit(t *testing.T) {
	r := isolatedgit.NewRepo(t)
	sha := r.HeadSHA()
	if len(sha) != 40 {
		t.Errorf("HEAD SHA looks odd: %q", sha)
	}
	out := r.Git("log", "--oneline")
	if !strings.Contains(out, "initial commit") {
		t.Errorf("log = %q, want substring %q", out, "initial commit")
	}
}

func TestAddFile_CreatesCommit(t *testing.T) {
	r := isolatedgit.NewRepo(t)
	beforeSHA := r.HeadSHA()
	afterSHA := r.AddFile("CHANGELOG.md", "# v1\n", "add changelog")
	if afterSHA == beforeSHA {
		t.Error("HEAD did not advance after AddFile")
	}
	out := r.Git("log", "-1", "--format=%s")
	if out != "add changelog" {
		t.Errorf("commit subject = %q, want %q", out, "add changelog")
	}
}

func TestAddTag_CreatesAnAnnotatedTag(t *testing.T) {
	r := isolatedgit.NewRepo(t)
	r.AddTag("v1.0.0", "Release v1.0.0")
	out := r.Git("tag", "--list")
	if out != "v1.0.0" {
		t.Errorf("tag list = %q, want %q", out, "v1.0.0")
	}
	// Annotated tag has its own object
	if got := r.Git("cat-file", "-t", "v1.0.0"); got != "tag" {
		t.Errorf("cat-file = %q, want %q", got, "tag")
	}
}

func TestAddBareRemote_PushesMain(t *testing.T) {
	r := isolatedgit.NewRepo(t)
	remote := r.AddBareRemote()

	out, err := exec.Command("git", "--git-dir="+remote, "log", "--oneline").Output()
	if err != nil {
		t.Fatalf("read remote: %v", err)
	}
	if !strings.Contains(string(out), "initial commit") {
		t.Errorf("remote log missing initial commit: %s", out)
	}
}

func TestRepos_AreIsolatedFromEachOther(t *testing.T) {
	a := isolatedgit.NewRepo(t)
	b := isolatedgit.NewRepo(t)
	if a.Dir == b.Dir {
		t.Errorf("two NewRepo() calls returned the same directory: %q", a.Dir)
	}
}

// TestAddFile_CommitsTheContentGiven reads the committed blob back. The test
// above checks only that HEAD moved and the subject matched, which a helper
// that committed an empty or stale file would satisfy.
func TestAddFile_CommitsTheContentGiven(t *testing.T) {
	r := isolatedgit.NewRepo(t)
	r.AddFile("docs/CHANGELOG.md", "# v1\n\n- first\n", "add changelog")

	if got := r.Git("show", "HEAD:docs/CHANGELOG.md"); got != "# v1\n\n- first" {
		t.Errorf("committed content = %q", got)
	}

	if got := r.Git("status", "--porcelain"); got != "" {
		t.Errorf("AddFile left the tree dirty: %q", got)
	}
}

// TestNewRepo_ConfiguresItsOwnIdentityAndSigningLocally pins the repository
// state the rest of the suite relies on: a test identity and signing switched
// off, set in THIS repository's config rather than inherited from anywhere.
func TestNewRepo_ConfiguresItsOwnIdentityAndSigningLocally(t *testing.T) {
	r := isolatedgit.NewRepo(t)

	for key, want := range map[string]string{
		"user.name":      "Test Bot",
		"user.email":     "bot@example.invalid",
		"commit.gpgsign": "false",
		"tag.gpgsign":    "false",
	} {
		if got := r.Git("config", "--local", "--get", key); got != want {
			t.Errorf("local %s = %q, want %q", key, got, want)
		}
	}

	if got := r.Git("rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Errorf("branch = %q, want main", got)
	}
}

// TestAddBareRemote_WiresOriginAndTracking checks the wiring, not only that
// the remote has a commit: origin points at the returned path, main tracks it,
// and the remote's main is the local HEAD.
func TestAddBareRemote_WiresOriginAndTracking(t *testing.T) {
	r := isolatedgit.NewRepo(t)
	r.AddCommit("second")
	remote := r.AddBareRemote()

	if r.Remote != remote {
		t.Errorf("Repo.Remote = %q, want the returned %q", r.Remote, remote)
	}

	if got := r.Git("remote", "get-url", "origin"); got != remote {
		t.Errorf("origin = %q, want %q", got, remote)
	}

	if got := r.Git("rev-parse", "--abbrev-ref", "main@{upstream}"); got != "origin/main" {
		t.Errorf("main tracks %q, want origin/main", got)
	}

	out, err := exec.Command("git", "--git-dir="+remote, "rev-parse", "main").Output() //nolint:gosec,noctx // test infra; remote is t.TempDir().
	if err != nil {
		t.Fatalf("read remote main: %v", err)
	}

	if got := strings.TrimSpace(string(out)); got != r.HeadSHA() {
		t.Errorf("remote main = %s, want local HEAD %s", got, r.HeadSHA())
	}
}

// TestRepos_DoNotShareHistory is the isolation that matters. Distinct
// directories are necessary but not sufficient: two repositories could still
// share an object store or a config. A commit in one must be absent from the
// other.
func TestRepos_DoNotShareHistory(t *testing.T) {
	a := isolatedgit.NewRepo(t)
	b := isolatedgit.NewRepo(t)

	sha := a.AddFile("only-in-a.txt", "a\n", "only in a")

	if got := b.Git("log", "--all", "--format=%s"); strings.Contains(got, "only in a") {
		t.Errorf("repository b sees a's commit: %q", got)
	}

	//nolint:gosec,noctx // test infra; b.Dir is t.TempDir().
	if err := exec.Command("git", "-C", b.Dir, "cat-file", "-e", sha).Run(); err == nil {
		t.Errorf("repository b can read a's object %s", sha)
	}
}

// TestGit_ReturnsStdoutOnly uses a command that succeeds while warning: with a
// branch and a tag of the same name, `git rev-parse` prints the SHA on stdout
// and "refname ... is ambiguous" on stderr. The helper used to return both
// streams combined, so the value handed back was not a SHA.
func TestGit_ReturnsStdoutOnly(t *testing.T) {
	r := isolatedgit.NewRepo(t)
	r.Git("branch", "dup")
	// Lightweight, so both refs name the HEAD commit; an annotated tag would
	// win the ambiguity and resolve to its own tag object.
	r.Git("tag", "dup")

	got := r.Git("rev-parse", "dup")
	if got != r.HeadSHA() {
		t.Errorf("rev-parse dup = %q, want only the SHA %s", got, r.HeadSHA())
	}
}
