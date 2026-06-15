//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adaptergit "github.com/diggsweden/reusable-ci/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/testutil/isolatedgit"
)

func TestInitWithObjectFormat(t *testing.T) {
	ctx := context.Background()

	t.Run("sha1", func(t *testing.T) {
		dir := t.TempDir()
		r := &adaptergit.Repo{Dir: dir}
		if err := r.InitWithObjectFormat(ctx, "sha1"); err != nil {
			t.Fatal(err)
		}
		if got := gitIn(t, dir, "rev-parse", "--show-object-format"); got != "sha1" {
			t.Errorf("object format = %q, want sha1", got)
		}
	})

	t.Run("sha256", func(t *testing.T) {
		dir := t.TempDir()
		r := &adaptergit.Repo{Dir: dir}
		if err := r.InitWithObjectFormat(ctx, "sha256"); err != nil {
			t.Fatal(err)
		}
		if got := gitIn(t, dir, "rev-parse", "--show-object-format"); got != "sha256" {
			t.Errorf("object format = %q, want sha256", got)
		}
	})

	t.Run("invalid is rejected before git runs", func(t *testing.T) {
		dir := t.TempDir()
		r := &adaptergit.Repo{Dir: dir}
		err := r.InitWithObjectFormat(ctx, "sha512")
		if !errors.Is(err, errs.ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
		if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil {
			t.Error("git init should not have run for an invalid object format")
		}
	})
}

func TestRemoteAdd(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	r := &adaptergit.Repo{Dir: dir}
	if err := r.InitWithObjectFormat(ctx, "sha1"); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoteAdd(ctx, "origin", "https://codeberg.org/owner/repo.git"); err != nil {
		t.Fatal(err)
	}
	if got := gitIn(t, dir, "remote", "get-url", "origin"); got != "https://codeberg.org/owner/repo.git" {
		t.Errorf("origin url = %q", got)
	}
}

// TestFetchAndCheckoutDetach_Local exercises the advertised-ref fetch
// paths (tag and branch) end to end against a local source repo, then
// detaches onto each — no network, no auth.
func TestFetchAndCheckoutDetach_Local(t *testing.T) {
	ctx := context.Background()

	src := isolatedgit.NewRepo(t)
	mainSHA := src.AddCommit("first")
	src.AddTag("v1.0.0", "release one")
	src.Git("checkout", "-b", "feature")
	featureSHA := src.AddCommit("second")
	src.Git("checkout", "-")

	target := t.TempDir()
	r := &adaptergit.Repo{Dir: target}
	if err := r.InitWithObjectFormat(ctx, "sha1"); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoteAdd(ctx, "origin", src.Dir); err != nil {
		t.Fatal(err)
	}

	// Tag path.
	if err := r.Fetch(ctx, src.Dir, []string{"+refs/tags/v1.0.0:refs/tags/v1.0.0"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckoutDetach(ctx, "refs/tags/v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if got := gitIn(t, target, "rev-parse", "HEAD"); got != mainSHA {
		t.Errorf("after tag checkout HEAD = %q, want %q", got, mainSHA)
	}

	// Branch path.
	if err := r.Fetch(ctx, src.Dir, []string{"+refs/heads/feature:refs/remotes/origin/feature"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckoutDetach(ctx, "refs/remotes/origin/feature"); err != nil {
		t.Fatal(err)
	}
	if got := gitIn(t, target, "rev-parse", "HEAD"); got != featureSHA {
		t.Errorf("after branch checkout HEAD = %q, want %q", got, featureSHA)
	}
}

// TestSparseCheckout_Local verifies cone-mode sparse-checkout materializes
// only the requested subtree in the working tree, against a real local repo.
func TestSparseCheckout_Local(t *testing.T) {
	ctx := context.Background()

	src := isolatedgit.NewRepo(t)
	src.AddFile("keep/file.txt", "k", "add keep")
	src.AddFile("drop/file.txt", "d", "add drop")
	src.AddTag("snap", "snapshot")

	target := t.TempDir()
	r := &adaptergit.Repo{Dir: target}

	for _, step := range []func() error{
		func() error { return r.InitWithObjectFormat(ctx, "sha1") },
		func() error { return r.RemoteAdd(ctx, "origin", src.Dir) },
		func() error { return r.EnablePartialClone(ctx) },
		func() error { return r.SparseInit(ctx, true) },
		func() error { return r.SparseSet(ctx, []string{"keep"}) },
		func() error { return r.Fetch(ctx, src.Dir, []string{"+refs/tags/snap:refs/tags/snap"}, "") },
		func() error { return r.CheckoutDetach(ctx, "refs/tags/snap") },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}

	// Partial-clone config applied (a file:// server ignores the filter and
	// fetches fully — harmless; the working-tree result below is the contract).
	if got := strings.TrimSpace(gitIn(t, target, "config", "--get", "remote.origin.partialclonefilter")); got != "blob:none" {
		t.Errorf("partialclonefilter = %q, want blob:none", got)
	}

	if _, err := os.Stat(filepath.Join(target, "keep", "file.txt")); err != nil {
		t.Errorf("sparse cone path keep/ should be present: %v", err)
	}

	if _, err := os.Stat(filepath.Join(target, "drop", "file.txt")); !os.IsNotExist(err) {
		t.Errorf("drop/ should be excluded by the sparse cone, stat err = %v", err)
	}
}

// TestFetchTags_Local verifies the default fetch omits tags (--no-tags) and
// FetchTags brings them in, against a real local repo.
func TestFetchTags_Local(t *testing.T) {
	ctx := context.Background()

	src := isolatedgit.NewRepo(t)
	src.AddTag("v9.9.9", "release")
	src.Git("checkout", "-b", "work")
	src.AddCommit("second")

	target := t.TempDir()
	r := &adaptergit.Repo{Dir: target}

	if err := r.InitWithObjectFormat(ctx, "sha1"); err != nil {
		t.Fatal(err)
	}

	if err := r.RemoteAdd(ctx, "origin", src.Dir); err != nil {
		t.Fatal(err)
	}

	// Default fetch is --no-tags: the branch's commits arrive, the tag ref
	// does not.
	if err := r.Fetch(ctx, src.Dir, []string{"+refs/heads/work:refs/remotes/origin/work"}, ""); err != nil {
		t.Fatal(err)
	}

	if tags := gitIn(t, target, "tag"); strings.Contains(tags, "v9.9.9") {
		t.Fatalf("tag present after --no-tags fetch: %q", tags)
	}

	if err := r.FetchTags(ctx, src.Dir, ""); err != nil {
		t.Fatal(err)
	}

	if tags := gitIn(t, target, "tag"); !strings.Contains(tags, "v9.9.9") {
		t.Errorf("tag missing after FetchTags: %q", tags)
	}
}

// TestFetch_AuthHeaderNotPersisted is the credential-free guarantee: an
// AuthHeader is passed for the fetch, but nothing about it may survive in
// .git/config. The local file remote ignores the http header, so the
// fetch still succeeds; the assertion is purely about non-persistence.
func TestFetch_AuthHeaderNotPersisted(t *testing.T) {
	ctx := context.Background()

	src := isolatedgit.NewRepo(t)
	src.AddCommit("first")
	src.AddTag("v1.0.0", "release one")

	target := t.TempDir()
	r := &adaptergit.Repo{Dir: target}
	if err := r.InitWithObjectFormat(ctx, "sha1"); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoteAdd(ctx, "origin", src.Dir); err != nil {
		t.Fatal(err)
	}
	if err := r.Fetch(ctx, "https://codeberg.org/owner/repo.git", []string{"+refs/tags/v1.0.0:refs/tags/v1.0.0"}, "secret-token"); err != nil {
		t.Fatal(err)
	}

	cfg, err := os.ReadFile(filepath.Join(target, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"extraheader", "Authorization", "secret-token", "x-access-token"} {
		if strings.Contains(string(cfg), leak) {
			t.Errorf(".git/config leaked %q — the auth header must stay transient", leak)
		}
	}
}

// gitIn runs git in dir and returns trimmed stdout, failing the test on error.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	r := &adaptergit.Repo{Dir: dir}
	out, err := r.Run(context.Background(), args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}

	return out
}
