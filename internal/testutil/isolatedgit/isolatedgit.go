// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package isolatedgit creates a throwaway git repo in t.TempDir() with a
// known committer identity, plus helpers to add commits/tags and an
// optional bare remote. Replaces the bats `common_setup_with_isolated_git`
// + `init_remote_repo` helpers.
//
// Each call to NewRepo isolates HOME and the global gitconfig so the test's
// commits never leak into the developer's environment.
package isolatedgit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
)

// Repo represents an isolated git working tree under t.TempDir().
type Repo struct {
	t      *testing.T
	Dir    string // working tree
	Home   string // isolated HOME (commits use this gitconfig)
	Remote string // bare remote dir, empty until AddBareRemote() is called
}

// NewRepo creates a fresh repo with a single empty initial commit on the
// `main` branch. Committer is set to "Test Bot <bot@example.invalid>".
//
// The full environment (HOME, XDG, GPG, SSH, locale, colour) is scrubbed
// via testutil/isolatedenv before any git command runs, so the test
// can't read the developer's ~/.gitconfig or trigger their SSH agent.
func NewRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()

	// Scrub the broader env first (HOME / XDG / GPG / SSH / locale /
	// colour). Returns the new $HOME so we can repoint the gitconfig.
	env := testenv.New(t)
	home := env.Home

	r := &Repo{t: t, Dir: dir, Home: home} //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	// Repoint the global gitconfig at $HOME/.gitconfig (isolatedenv
	// blackholes it to /dev/null by default; tests that want repo-local
	// `git config` only — most of ours — can keep that. But some tests
	// expect a writable global file so signing keys can be configured).
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))

	r.gitInWorkdir("init", "-q", "-b", "main")
	r.gitInWorkdir("config", "user.name", "Test Bot")
	r.gitInWorkdir("config", "user.email", "bot@example.invalid")
	r.gitInWorkdir("config", "commit.gpgsign", "false")
	r.gitInWorkdir("config", "tag.gpgsign", "false")

	r.AddCommit("initial commit")

	return r
}

// Git runs `git <args...>` inside the repo's working tree and returns stdout.
// On failure the test fatals with a clear error.
func (r *Repo) Git(args ...string) string {
	r.t.Helper()

	return r.gitInWorkdir(args...)
}

// AddCommit creates a commit (allow-empty) with the given message and returns the SHA.
func (r *Repo) AddCommit(msg string) string {
	r.t.Helper()
	r.gitInWorkdir("commit", "--allow-empty", "-q", "-m", msg)

	return r.gitInWorkdir("rev-parse", "HEAD")
}

// AddFile writes a file at relPath, stages it, and creates a commit.
func (r *Repo) AddFile(relPath, contents, commitMsg string) string {
	r.t.Helper()

	full := filepath.Join(r.Dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil { //nolint:gosec // test infra; r.Dir is t.TempDir().
		r.t.Fatalf("mkdir %q: %v", filepath.Dir(full), err)
	}

	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil { //nolint:gosec // test infra; full is r.Dir-rooted.
		r.t.Fatalf("write %q: %v", full, err)
	}

	r.gitInWorkdir("add", relPath)
	r.gitInWorkdir("commit", "-q", "-m", commitMsg)

	return r.gitInWorkdir("rev-parse", "HEAD")
}

// AddTag creates an annotated tag pointing at HEAD with the given message.
func (r *Repo) AddTag(name, msg string) {
	r.t.Helper()
	r.gitInWorkdir("tag", "-a", name, "-m", msg)
}

// AddBareRemote creates a bare remote in t.TempDir(), registers it as `origin`,
// and pushes the current main branch.
func (r *Repo) AddBareRemote() string {
	r.t.Helper()
	remote := r.t.TempDir()

	//nolint:gosec,noctx // test infra; remote is t.TempDir().
	cmd := exec.Command("git", "init", "--bare", "-q", "-b", "main", remote)
	if out, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("init bare remote: %v\n%s", err, out)
	}

	r.gitInWorkdir("remote", "add", "origin", remote)
	r.gitInWorkdir("push", "-q", "-u", "origin", "main")
	r.Remote = remote

	return remote
}

// HeadSHA returns the current HEAD commit SHA.
func (r *Repo) HeadSHA() string {
	return r.gitInWorkdir("rev-parse", "HEAD")
}

func (r *Repo) gitInWorkdir(args ...string) string {
	r.t.Helper()

	//nolint:gosec,noctx // test infra; args are caller-supplied within tests.
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s\nin %s\noutput:\n%s\nerr: %v",
			strings.Join(args, " "), r.Dir, out, err)
	}

	return strings.TrimRight(string(out), "\n")
}
