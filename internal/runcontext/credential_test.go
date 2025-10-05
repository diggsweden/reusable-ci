// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package runcontext_test

import (
	"bytes"
	"fmt"
	"log/slog"
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

// TestCredential_ForgejoNativeTokensStayOnForgejo verifies that runner-injected
// Forgejo and Gitea token names are ambient credentials, not neutral overrides.
func TestCredential_ForgejoNativeTokensStayOnForgejo(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		marker string
		token  string
	}{
		{name: "forgejo", marker: "FORGEJO_ACTIONS", token: "FORGEJO_TOKEN"}, //nolint:gosec // G101: environment variable name, not a credential.
		{name: "gitea", marker: "GITEA_ACTIONS", token: "GITEA_TOKEN"},       //nolint:gosec // G101: environment variable name, not a credential.
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			values := map[string]string{ //nolint:gosec // G101: invented values in a fixture.
				"GITHUB_ACTIONS":     "true",
				"FORGEJO_SERVER_URL": "https://code.example",
				tc.marker:            "true",
				tc.token:             "native-job-token",
			}
			cred := runcontext.Token().Resolve(env(values))

			if got := cred.For("https://code.example/o/r"); got != "native-job-token" {
				t.Errorf("native token must work at its runner's server; got %q", got)
			}

			if got := cred.For("https://mirror.example/o/r"); got != "" {
				t.Errorf("native token must not leave its runner's server; got %q", got)
			}
		})
	}
}

// TestCredential_CITokenIsAnOrchestratedOverride verifies that CI_TOKEN keeps
// its deliberate cross-forge semantics even on a Forgejo runner.
func TestCredential_CITokenIsAnOrchestratedOverride(t *testing.T) {
	t.Parallel()

	e := env(map[string]string{ //nolint:gosec // G101: invented values in a fixture.
		"GITHUB_ACTIONS":     "true",
		"FORGEJO_ACTIONS":    "true",
		"FORGEJO_SERVER_URL": "https://code.example",
		"CI_TOKEN":           "orchestrated-token",
		"FORGEJO_TOKEN":      "native-job-token",
	})

	if got := runcontext.Token().Resolve(e).For("https://mirror.example/o/r"); got != "orchestrated-token" {
		t.Errorf("orchestrated CI_TOKEN must retain its operator-selected audience; got %q", got)
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

// TestCredential_Redacts covers ordinary formatting, including private fields
// where fmt cannot invoke Credential's methods. Explicit reflection is excluded.
func TestCredential_Redacts(t *testing.T) {
	t.Parallel()

	secret := "ghs_super_secret_value"

	bound := runcontext.Token().Resolve(env(map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_SERVER_URL": "https://github.com", "GITHUB_TOKEN": secret}))
	for _, cred := range []runcontext.Credential{runcontext.OperatorCredential(secret), bound} {
		if !cred.Present() || cred.For("https://github.com/allowed") != secret {
			t.Fatal("secret not retained: vacuous redaction control")
		}

		copied := cred
		for _, shape := range []any{cred, &cred, struct{ C runcontext.Credential }{cred}, struct{ c runcontext.Credential }{cred}, struct{ nested any }{struct{ c runcontext.Credential }{cred}}, []runcontext.Credential{cred}, map[string]any{"credential": struct{ c runcontext.Credential }{cred}}} {
			for _, format := range []string{"%v", "%+v", "%#v"} {
				rendered := fmt.Sprintf(format, shape)
				if rendered == "" || strings.Contains(rendered, secret) {
					t.Errorf("format=%s leaked or empty: %s", format, rendered)
				}
			}

			var out bytes.Buffer
			slog.New(slog.NewTextHandler(&out, nil)).Info("diagnostic", "credential", shape)

			if strings.Contains(out.String(), secret) || !strings.Contains(out.String(), "diagnostic") {
				t.Errorf("log leaked or lost diagnostic: %s", &out)
			}
		}

		if copied.For("https://github.com/allowed") != secret {
			t.Fatal("formatting changed credential")
		}
	}

	for _, cred := range []runcontext.Credential{{}, runcontext.OperatorCredential("")} {
		if cred.Present() || cred.For("https://github.com") != "" || cred.String() != "<no credential>" {
			t.Fatal("empty credential semantics changed")
		}
	}
}

func TestCredential_EffectiveOriginPolicy(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		issuer, destination string
		allowed             bool
	}{
		{"https://github.com", "https://github.com:443/o/r", true},
		{"https://github.com:443", "https://GITHUB.COM/o/r", true},
		{"http://github.com", "http://github.com:80", true},
		{"http://github.com:80", "http://github.com", true},
		{"https://[::1]", "https://[::1]:443/path", true},
		{"https://github.com:8443", "https://github.com:8443/path", true},
		{"https://github.com", "http://github.com:80", false},
		{"https://github.com", "http://github.com:443", false},
		{"http://github.com", "https://github.com:80", false},
		{"https://github.com", "https://github.com:8443", false},
		{"https://github.com", "https://github.com.evil.invalid:443", false},
		{"https://github.com", "https://someone@github.com", false},
		{"https://someone@github.com", "https://github.com", false},
		{"https://github.com", "https://github.com:bad", false},
		{"https://github.com:bad", "https://github.com", false},
		{"https://github.com", "https://github.com:65536", false},
		{"https://github.com", "https://github.com:", false},
		{"https://github.com", "//github.com", false},
		{"//github.com", "https://github.com", false},
		{"https://github.com", "https:///path", false},
		{"https:///path", "https://github.com", false},
		{"ssh://github.com", "ssh://github.com", false},
	} {
		cred := runcontext.Token().Resolve(env(map[string]string{"GITHUB_ACTIONS": "true", "GITHUB_SERVER_URL": tc.issuer, "GITHUB_TOKEN": "synthetic-origin-token"}))
		got := cred.For(tc.destination)

		want := ""
		if tc.allowed {
			want = "synthetic-origin-token"
		}

		if got != want {
			t.Errorf("issuer=%q destination=%q got=%q want=%q", tc.issuer, tc.destination, got, want)
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

// TestCredential_EmptyReleaseTokenFallsThrough verifies that an unset
// `${{ secrets.RELEASE_TOKEN }}` does not shadow a populated fallback.
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

// TestCredential_DedicatedReleaseTokenWins verifies precedence and explicit
// cross-forge use. The operator, not this resolver, is responsible for limiting
// the dedicated token's scope.
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

// TestCredential_ActRunnerGitHubAliasIsDeniedElsewhere is the denial half of
// TestCredential_ActRunnerGitHubAliasIsItsOwn.
//
// Every other token in this file is pinned from both sides: it works at its own
// server and is withheld everywhere else. The act_runner alias had only the
// positive half, and it is the case that most needs the other one. The token
// arrives under the name GITHUB_TOKEN while the runner is Forgejo, so anything
// that decided the audience from the variable's NAME rather than from the
// runner it is executing on would conclude the audience is github.com — and
// hand a Forgejo job token to GitHub, to whoever operates the destination.
func TestCredential_ActRunnerGitHubAliasIsDeniedElsewhere(t *testing.T) {
	t.Parallel()

	e := env(map[string]string{ //nolint:gosec // G101: invented values in a fixture; the real secrets are names only.
		"GITHUB_ACTIONS":     "true",
		"FORGEJO_ACTIONS":    "true",
		"GITHUB_TOKEN":       "forgejo_job_token",
		"FORGEJO_SERVER_URL": "https://codeberg.org",
	})

	resolved := runcontext.Token().Resolve(e)

	for _, destination := range []string{
		"https://github.com/o/r",
		"https://api.github.com/repos/o/r",
		// A host that merely starts with the audience: the sibling-host
		// case, checked here too so the alias is not the one path where a
		// prefix comparison would slip through.
		"https://codeberg.org.evil.example/o/r",
		"https://gitlab.com/o/r",
	} {
		if got := resolved.For(destination); got != "" {
			t.Errorf("act_runner's job token was sent to %s: got %q, want it withheld", destination, got)
		}
	}

	// Control: the same resolution still works where it belongs, so a
	// blanket denial cannot satisfy the loop above.
	if got := resolved.For("https://codeberg.org/o/r"); got != "forgejo_job_token" {
		t.Errorf("the token stopped working at its own server; got %q", got)
	}
}
