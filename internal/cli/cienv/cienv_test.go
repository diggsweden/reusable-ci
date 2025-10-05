// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cienv_test

import (
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
)

// TestNonEmptyEnvSource_SkipsSetButEmpty is the regression guard for the
// urfave/cli empty-shadow bug: a forge-neutral var blanked to "" by a
// workflow must NOT shadow a populated fallback.
func TestNonEmptyEnvSource_SkipsSetButEmpty(t *testing.T) {
	testenv.New(t)
	t.Setenv("REF_NAME", "")          // neutral var set-but-empty (the trap)
	t.Setenv("GITHUB_REF_NAME", "v1") // populated fallback

	chain := cienv.RefName()

	got, ok := chain.Lookup()
	if !ok || got != "v1" {
		t.Fatalf("RefName() = %q, ok=%v; want \"v1\", true (empty REF_NAME must not shadow GITHUB_REF_NAME)", got, ok)
	}
}

// TestNonEmptyEnvSource_FirstNonEmptyWins: the project-neutral variable is
// consulted before the runner's own, so a workflow can name the repository
// explicitly and have that beat whatever the runner injected.
func TestNonEmptyEnvSource_FirstNonEmptyWins(t *testing.T) {
	testenv.New(t)
	t.Setenv("REPOSITORY", "owner/explicit")
	t.Setenv("GITHUB_REPOSITORY", "owner/runner")

	chain := cienv.Repository()

	got, ok := chain.Lookup()
	if !ok || got != "owner/explicit" {
		t.Fatalf("Repository() = %q; want explicit project-neutral var to win", got)
	}
}

func TestNonEmptyEnvSource_AllUnsetIsAbsent(t *testing.T) {
	testenv.New(t)

	for _, k := range []string{"REF", "GITHUB_REF"} {
		t.Setenv(k, "")
	}

	chain := cienv.Ref()
	if _, ok := chain.Lookup(); ok {
		t.Fatal("Ref() should be absent when every source is empty/unset")
	}
}

// TestServerURL_ForgeFallbacks proves ServerURL resolves on a Forgejo or
// GitHub runner, not only when the neutral CI_SERVER_URL is set — the clone
// URL that `platform checkout` builds depends on it.
func TestServerURL_ForgeFallbacks(t *testing.T) {
	testenv.New(t)
	t.Run("forgejo-native", func(t *testing.T) {
		t.Setenv("CI_SERVER_URL", "")
		t.Setenv("FORGEJO_SERVER_URL", "https://codeberg.org")
		t.Setenv("GITHUB_SERVER_URL", "https://github.com")

		chain := cienv.ServerURL()

		got, ok := chain.Lookup()
		if !ok || got != "https://codeberg.org" {
			t.Fatalf("ServerURL() = %q, ok=%v; want forgejo URL", got, ok)
		}
	})

	t.Run("github-native", func(t *testing.T) {
		t.Setenv("CI_SERVER_URL", "")
		t.Setenv("FORGEJO_SERVER_URL", "")
		t.Setenv("GITHUB_SERVER_URL", "https://github.com")

		chain := cienv.ServerURL()

		got, ok := chain.Lookup()
		if !ok || got != "https://github.com" {
			t.Fatalf("ServerURL() = %q, ok=%v; want github URL", got, ok)
		}
	})
}

// TestCheckoutRef_ExplicitOverrideWinsOverCommit proves a workflow can check
// out a branch by setting CHECKOUT_REF, while the default stays the triggering
// commit — what lets `platform checkout` replace `actions/checkout` with
// `ref: <branch>`.
func TestCheckoutRef_ExplicitOverrideWinsOverCommit(t *testing.T) {
	testenv.New(t)
	t.Run("explicit_ref_wins", func(t *testing.T) {
		t.Setenv("CHECKOUT_REF", "main")
		t.Setenv("GITHUB_SHA", "abc1234")

		chain := cienv.CheckoutRef()

		got, ok := chain.Lookup()
		if !ok || got != "main" {
			t.Fatalf("CheckoutRef() = %q, ok=%v; want CHECKOUT_REF to win", got, ok)
		}
	})

	t.Run("falls_back_to_triggering_commit", func(t *testing.T) {
		t.Setenv("CHECKOUT_REF", "")
		t.Setenv("GITHUB_SHA", "abc1234")

		chain := cienv.CheckoutRef()

		got, ok := chain.Lookup()
		if !ok || got != "abc1234" {
			t.Fatalf("CheckoutRef() = %q, ok=%v; want fallback to GITHUB_SHA", got, ok)
		}
	})
}

// TestChains_ExposeEnvKeys proves the custom source still advertises its keys
// to --help / the generated reference (implements cli.EnvValueSource).
func TestChains_ExposeEnvKeys(t *testing.T) {
	chain := cienv.Repository()

	keys := chain.EnvKeys()
	for _, want := range []string{"REPOSITORY", "CI_REPO", "GITHUB_REPOSITORY"} {
		if !slices.Contains(keys, want) {
			t.Errorf("EnvKeys()=%v missing %q (docs/help would drop it)", keys, want)
		}
	}
}

// TestChains_RepositoryPrecedenceIsPinnedLiterally keeps
// TestAccessors_BindTheirOwnersCompleteOrderedChain from being a tautology.
//
// That test already compares every accessor's chain against its owning
// runcontext variable, in order — which is the parity guard, and it is a real
// one: reversing the chain or dropping a fallback fails it. What it cannot
// catch is a reordering INSIDE runcontext, because both sides derive from the
// same list and would move together. Order here is precedence, so one chain is
// written out literally as the anchor.
//
// The membership-only test next to it named three of these five keys, which is
// how the two Forgejo names came to be unpinned.
func TestChains_RepositoryPrecedenceIsPinnedLiterally(t *testing.T) {
	// Orchestrated name first, then the forge-neutral one, then the
	// forge-specific ones. The membership-only test this replaces named
	// three of these five, so the two Forgejo names could have been dropped
	// or reordered without anything noticing.
	want := []string{"REPOSITORY", "CI_REPO", "FORGEJO_REPOSITORY", "FORGEJO_REPO", "GITHUB_REPOSITORY"}
	chain := cienv.Repository()

	if got := chain.EnvKeys(); !slices.Equal(got, want) {
		t.Errorf("Repository EnvKeys() = %v, want %v (order is precedence)", got, want)
	}
}
