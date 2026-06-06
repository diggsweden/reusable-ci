//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package isolatedgit_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/testutil/isolatedgit"
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

func TestAddTag(t *testing.T) {
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
