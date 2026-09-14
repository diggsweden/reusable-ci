//go:build integration

// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// The adapter resolves refs in-process via go-git for speed, standing in for
// `git rev-parse` / `git cat-file -t`. These tests pin the part of that
// substitution that is easy to get wrong and silent when wrong: an ANNOTATED
// tag is its own object, and only an explicit peel suffix asks for the commit
// underneath it. go-git's ResolveRevision always peels, so the fast path once
// disagreed with the command it replaces — reporting an annotated tag as its
// commit, which made `release validate-tag` refuse every annotated release tag
// as "not an annotated tag object" and made `version tag-release` compare a
// local commit against a remote tag object, so its documented exact-rerun
// tolerance could never be reached.
//
// Each case asserts the adapter agrees with REAL GIT rather than with a
// hardcoded expectation. That is the contract worth pinning — "matches git" —
// and unlike a literal it cannot drift as go-git changes.

package git_test

import (
	"context"
	"testing"

	adaptergit "github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/isolatedgit"
)

func TestRevParse_AgreesWithGitOnTagRefs(t *testing.T) {
	ctx := context.Background()
	ig := isolatedgit.NewRepo(t)
	repo := &adaptergit.Repo{Dir: ig.Dir}

	// A second commit gives the tagged commit a parent to resolve.
	ig.AddCommit("release bump")
	ig.AddTag("v1.2.3", "annotated release tag")
	ig.Git("tag", "lightweight-1.2.3")

	// The distinction the whole test rests on: an annotated tag object is NOT
	// the commit. If the fixture ever stopped producing an annotated tag, every
	// assertion below would still pass while checking nothing.
	tagObject := ig.Git("rev-parse", "refs/tags/v1.2.3")
	if commit := ig.Git("rev-parse", "refs/tags/v1.2.3^{commit}"); tagObject == commit {
		t.Fatalf("fixture is degenerate: v1.2.3 is not an annotated tag (object == commit == %s)", commit)
	}

	for _, ref := range []string{
		"refs/tags/v1.2.3",            // full ref path
		"v1.2.3",                      // short name — both spellings must agree
		"refs/tags/v1.2.3^{commit}",   // explicit peel
		"v1.2.3^{commit}",             //
		"refs/tags/v1.2.3^{commit}^",  // the parent of the tagged commit
		"refs/tags/lightweight-1.2.3", // lightweight: the ref IS the commit
		"lightweight-1.2.3",
		"HEAD",
	} {
		t.Run(ref, func(t *testing.T) {
			want := ig.Git("rev-parse", ref)

			got, err := repo.RevParse(ctx, ref)
			if err != nil {
				t.Fatalf("RevParse(%q): %v", ref, err)
			}

			if got != want {
				t.Errorf("RevParse(%q) = %s, git says %s", ref, got, want)
			}
		})
	}
}

func TestCatFileType_AgreesWithGitOnTagRefs(t *testing.T) {
	ctx := context.Background()
	ig := isolatedgit.NewRepo(t)
	repo := &adaptergit.Repo{Dir: ig.Dir}

	ig.AddTag("v1.2.3", "annotated release tag")
	ig.Git("tag", "lightweight-1.2.3")

	for _, ref := range []string{
		// Both spellings must report "tag". Callers legitimately use either,
		// and the full-ref spelling is the one that used to report "commit".
		"refs/tags/v1.2.3",
		"v1.2.3",
		"refs/tags/lightweight-1.2.3",
		"lightweight-1.2.3",
	} {
		t.Run(ref, func(t *testing.T) {
			want := ig.Git("cat-file", "-t", ref)

			got, err := repo.CatFileType(ctx, ref)
			if err != nil {
				t.Fatalf("CatFileType(%q): %v", ref, err)
			}

			if got != want {
				t.Errorf("CatFileType(%q) = %q, git says %q", ref, got, want)
			}
		})
	}
}
