// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package cienv

import (
	"slices"
	"testing"
)

// TestNonEmptyEnvSource_SkipsSetButEmpty is the regression guard for the
// urfave/cli empty-shadow bug: a forge-neutral var blanked to "" by a
// workflow must NOT shadow a populated fallback.
func TestNonEmptyEnvSource_SkipsSetButEmpty(t *testing.T) {
	t.Setenv("REF_NAME", "")          // neutral var set-but-empty (the trap)
	t.Setenv("GITHUB_REF_NAME", "v1") // populated fallback

	chain := RefName()

	got, ok := chain.Lookup()
	if !ok || got != "v1" {
		t.Fatalf("RefName() = %q, ok=%v; want \"v1\", true (empty REF_NAME must not shadow GITHUB_REF_NAME)", got, ok)
	}
}

// TestReleaseToken_EmptyReleaseTokenFallsThrough guards the write-token
// chain: a RELEASE_TOKEN blanked to "" (an unset `${{ secrets.* }}`) must
// fall through to the forge's ambient token, not shadow it — and the chain
// must be forge-generic (FORGEJO_TOKEN works when no GitHub token is set).
func TestNonEmptyEnvSource_FirstNonEmptyWins(t *testing.T) {
	t.Setenv("REPOSITORY", "owner/explicit")
	t.Setenv("GITHUB_REPOSITORY", "owner/runner")

	chain := Repository()

	got, ok := chain.Lookup()
	if !ok || got != "owner/explicit" {
		t.Fatalf("Repository() = %q; want explicit project-neutral var to win", got)
	}
}

func TestNonEmptyEnvSource_AllUnsetIsAbsent(t *testing.T) {
	for _, k := range []string{"REF", "GITHUB_REF"} {
		t.Setenv(k, "")
	}

	chain := Ref()
	if _, ok := chain.Lookup(); ok {
		t.Fatal("Ref() should be absent when every source is empty/unset")
	}
}

// TestServerURL_ForgeFallbacks proves ServerURL resolves on a Forgejo or
// GitHub runner, not only when the neutral CI_SERVER_URL is set — the clone
// URL that `platform checkout` builds depends on it.
func TestServerURL_ForgeFallbacks(t *testing.T) {
	t.Run("forgejo-native", func(t *testing.T) {
		t.Setenv("CI_SERVER_URL", "")
		t.Setenv("FORGEJO_SERVER_URL", "https://codeberg.org")
		t.Setenv("GITHUB_SERVER_URL", "https://github.com")

		chain := ServerURL()

		got, ok := chain.Lookup()
		if !ok || got != "https://codeberg.org" {
			t.Fatalf("ServerURL() = %q, ok=%v; want forgejo URL", got, ok)
		}
	})

	t.Run("github-native", func(t *testing.T) {
		t.Setenv("CI_SERVER_URL", "")
		t.Setenv("FORGEJO_SERVER_URL", "")
		t.Setenv("GITHUB_SERVER_URL", "https://github.com")

		chain := ServerURL()

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
	t.Run("explicit_ref_wins", func(t *testing.T) {
		t.Setenv("CHECKOUT_REF", "main")
		t.Setenv("GITHUB_SHA", "abc1234")

		chain := CheckoutRef()

		got, ok := chain.Lookup()
		if !ok || got != "main" {
			t.Fatalf("CheckoutRef() = %q, ok=%v; want CHECKOUT_REF to win", got, ok)
		}
	})

	t.Run("falls_back_to_triggering_commit", func(t *testing.T) {
		t.Setenv("CHECKOUT_REF", "")
		t.Setenv("GITHUB_SHA", "abc1234")

		chain := CheckoutRef()

		got, ok := chain.Lookup()
		if !ok || got != "abc1234" {
			t.Fatalf("CheckoutRef() = %q, ok=%v; want fallback to GITHUB_SHA", got, ok)
		}
	})
}

// TestChainsExposeEnvKeys proves the custom source still advertises its keys
// to --help / the generated reference (implements cli.EnvValueSource).
func TestChainsExposeEnvKeys(t *testing.T) {
	chain := Repository()

	keys := chain.EnvKeys()
	for _, want := range []string{"REPOSITORY", "CI_REPO", "GITHUB_REPOSITORY"} {
		if !slices.Contains(keys, want) {
			t.Errorf("EnvKeys()=%v missing %q (docs/help would drop it)", keys, want)
		}
	}
}
