// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package runcontext_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// TestCredential_GitHubTokenNeverLeavesGitHub is the regression guard for a
// PROVEN disclosure: on a GitHub runner, a job checking out from a
// third-party Forgejo with $FORGEJO_TOKEN unset resolved --server-url to the
// Forgejo host and --token to $GITHUB_TOKEN, then sent GitHub's job token
// there as HTTP Basic auth. The two values came from independent chains and
// nothing required them to agree.
func TestCredential_GitHubTokenNeverLeavesGitHub(t *testing.T) {
	t.Parallel()

	e := env(map[string]string{ //nolint:gosec // G101: invented values in a fixture; the real secrets are names only.
		"GITHUB_ACTIONS":     "true",
		"GITHUB_TOKEN":       "ghs_github_job_token",
		"GITHUB_SERVER_URL":  "https://github.com",
		"FORGEJO_SERVER_URL": "https://third-party.example",
		"FORGEJO_TOKEN":      "", // an unset ${{ secrets.X }} interpolates to ""
	})

	cred := runcontext.Token().Resolve(e)

	if got := cred.For("https://third-party.example/o/r"); got != "" {
		t.Errorf("GitHub's job token must not be sent to a third-party host; got %q", got)
	}

	if got := cred.For("https://github.com/o/r"); got != "ghs_github_job_token" {
		t.Errorf("the token must still work at the server that issued it; got %q", got)
	}
}

// TestCredential_DeliberateTokenCrossesForges pins the other side: a name the
// current runner does NOT inject was set on purpose, and restricting it would
// break the legitimate cross-forge job while protecting nothing.
func TestCredential_DeliberateTokenCrossesForges(t *testing.T) {
	t.Parallel()

	e := env(map[string]string{ //nolint:gosec // G101: invented values in a fixture; the real secrets are names only.
		"GITHUB_ACTIONS":     "true", // a GitHub runner...
		"GITHUB_TOKEN":       "ghs_github_job_token",
		"GITHUB_SERVER_URL":  "https://github.com",
		"FORGEJO_SERVER_URL": "https://codeberg.org",
		"FORGEJO_TOKEN":      "forgejo_pat",
	})

	// FORGEJO_TOKEN precedes GITHUB_TOKEN in the chain, so it wins and is
	// unrestricted -- GitHub never sets it, so whoever did meant it.
	if got := runcontext.Token().Resolve(e).For("https://codeberg.org/o/r"); got != "forgejo_pat" {
		t.Errorf("a deliberately-set token must reach its forge; got %q", got)
	}
}

// TestCredential_ActRunnerGitHubAliasIsItsOwn pins that Forgejo's act_runner,
// which exposes the job token under the GitHub-compatible name, still works:
// there $GITHUB_TOKEN IS the Forgejo credential, and the attested server URL
// is the Forgejo one, so they agree.
func TestCredential_ActRunnerGitHubAliasIsItsOwn(t *testing.T) {
	t.Parallel()

	e := env(map[string]string{ //nolint:gosec // G101: invented values in a fixture; the real secrets are names only.
		"GITHUB_ACTIONS":     "true",
		"FORGEJO_ACTIONS":    "true",
		"GITHUB_TOKEN":       "forgejo_job_token",
		"FORGEJO_SERVER_URL": "https://codeberg.org",
	})

	if got := runcontext.Token().Resolve(e).For("https://codeberg.org/o/r"); got != "forgejo_job_token" {
		t.Errorf("act_runner's GitHub-aliased job token must work at its own server; got %q", got)
	}
}

// TestCredential_OriginIsComparedNotPrefixed is the sibling-host guard, the
// same class of mistake AnchorIdentity avoids: a prefix or substring test
// would accept an attacker-controlled host that merely starts with the
// audience.
func TestCredential_OriginIsComparedNotPrefixed(t *testing.T) {
	t.Parallel()

	e := env(map[string]string{ //nolint:gosec // G101: invented values in a fixture; the real secrets are names only.
		"GITHUB_ACTIONS":    "true",
		"GITHUB_TOKEN":      "ghs_secret",
		"GITHUB_SERVER_URL": "https://github.com",
	})
	cred := runcontext.Token().Resolve(e)

	for _, dest := range []string{
		"https://github.com.evil.example/o/r",
		"https://evil.example/?x=https://github.com",
		"http://github.com/o/r", // scheme downgrade
		"https://github.com:8443/o/r",
		"git@github.com:o/r.git", // not an absolute URL
		"",
	} {
		if got := cred.For(dest); got != "" {
			t.Errorf("must not send the credential to %q; got %q", dest, got)
		}
	}
}

// TestCredential_FailsClosedWithoutAttestedServer pins that an ambient token
// on a runner exposing no attested server URL is usable nowhere, rather than
// being sent somewhere guessed.
func TestCredential_FailsClosedWithoutAttestedServer(t *testing.T) {
	t.Parallel()

	e := env(map[string]string{ //nolint:gosec // G101: invented values in a fixture; the real secrets are names only.
		"GITHUB_ACTIONS": "true",
		"GITHUB_TOKEN":   "ghs_secret",
		// no GITHUB_SERVER_URL
	})

	cred := runcontext.Token().Resolve(e)
	if got := cred.For("https://github.com/o/r"); got != "" {
		t.Errorf("an ambient token with no attested server must be usable nowhere; got %q", got)
	}

	if !cred.Present() {
		t.Error("Present should still report the secret exists, without revealing it")
	}
}

// TestCredential_Redacts pins that a Credential cannot leak through a log
// line, a %v, or a struct dump.
func TestCredential_Redacts(t *testing.T) {
	t.Parallel()

	secret := "ghs_super_secret_value"
	cred := runcontext.OperatorCredential(secret)

	for _, rendered := range []string{
		cred.String(),
		fmt.Sprintf("%v", cred),
		fmt.Sprintf("%#v", cred),
		fmt.Sprintf("%v", struct{ C runcontext.Credential }{cred}),
	} {
		if strings.Contains(rendered, secret) {
			t.Errorf("Credential rendered its secret: %q", rendered)
		}
	}
}

// TestCredential_OperatorCredentialIsUnrestricted pins the escape hatch: an
// operator who types a token next to a command has decided where it goes.
func TestCredential_OperatorCredentialIsUnrestricted(t *testing.T) {
	t.Parallel()

	cred := runcontext.OperatorCredential("typed_by_a_human")
	if got := cred.For("https://anywhere.example/o/r"); got != "typed_by_a_human" {
		t.Errorf("an explicit --token must be honoured; got %q", got)
	}
}

// TestCredential_EmptyReleaseTokenFallsThrough moved here from cienv with the
// behaviour it pins. An unset `${{ secrets.RELEASE_TOKEN }}` interpolates to
// "" and must not shadow a populated fallback -- the same empty-means-absent
// trap that let the original disclosure through.
func TestCredential_EmptyReleaseTokenFallsThrough(t *testing.T) {
	t.Parallel()

	e := env(map[string]string{ //nolint:gosec // G101: invented values in a fixture; the real secrets are names only.
		"RELEASE_TOKEN":      "", // unset secret resolved to "" (the trap)
		"CI_TOKEN":           "",
		"FORGEJO_TOKEN":      "forgejo-tok", // forge-generic fallback
		"GITHUB_TOKEN":       "",
		"FORGEJO_SERVER_URL": "https://codeberg.org",
	})

	if got := runcontext.ReleaseToken().Resolve(e).For("https://codeberg.org/o/r"); got != "forgejo-tok" {
		t.Errorf("empty RELEASE_TOKEN must fall through; got %q", got)
	}
}

// TestCredential_DedicatedReleaseTokenWins pins least privilege: a dedicated
// write token beats the forge's ambient one, and is unrestricted because a
// human configured it deliberately -- which is what makes pushing a release
// to a mirror on another forge still possible.
func TestCredential_DedicatedReleaseTokenWins(t *testing.T) {
	t.Parallel()

	e := env(map[string]string{ //nolint:gosec // G101: invented values in a fixture; the real secrets are names only.
		"GITHUB_ACTIONS":    "true",
		"RELEASE_TOKEN":     "release-tok",
		"GITHUB_TOKEN":      "github-tok",
		"GITHUB_SERVER_URL": "https://github.com",
	})

	if got := runcontext.ReleaseToken().Resolve(e).For("https://github.com/o/r"); got != "release-tok" {
		t.Errorf("dedicated write token must win; got %q", got)
	}

	if got := runcontext.ReleaseToken().Resolve(e).For("https://mirror.example/o/r"); got != "release-tok" {
		t.Errorf("a deliberately-configured release token must still reach a mirror; got %q", got)
	}
}
