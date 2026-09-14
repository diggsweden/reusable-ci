// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package clitoken_test

import (
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/clitoken"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
)

// tokenCmd builds a parsed command with just a --token flag, matching how the
// real commands declare it: no env Sources.
func tokenCmd(t *testing.T, args ...string) *cli.Command {
	t.Helper()

	cmd := &cli.Command{Flags: []cli.Flag{&cli.StringFlag{Name: "token"}}}
	if err := cmd.Run(t.Context(), append([]string{"x"}, args...)); err != nil {
		t.Fatal(err)
	}

	return cmd
}

// TestResolve_ProvenLeakIsClosed is the end-to-end regression guard for a
// disclosure that was real, not theoretical.
//
// On a GitHub runner, `platform checkout` resolved --server-url and --token
// from two INDEPENDENT chains. A workflow checking out from a third-party
// Forgejo set $FORGEJO_SERVER_URL, and $FORGEJO_TOKEN was unset (an unset
// `${{ secrets.X }}` interpolates to "", and empty means absent, so it fell
// through) -- so the destination became the Forgejo host while the token fell
// through to $GITHUB_TOKEN. Git then sent GitHub's job token to that host as
// HTTP Basic auth.
//
// Nothing in a pair of strings required the two to agree. Now the credential
// carries the server that issued it, and the checkout is anonymous instead.
func TestResolve_ProvenLeakIsClosed(t *testing.T) {
	testenv.New(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_TOKEN", "ghs_github_job_token")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("FORGEJO_SERVER_URL", "https://third-party.example")
	t.Setenv("FORGEJO_TOKEN", "")

	cred := clitoken.Resolve(tokenCmd(t))

	if got := cred.For("https://third-party.example/o/r"); got != "" {
		t.Error("GitHub's job token must not reach a third-party host")
	}

	if got := cred.For("https://github.com/o/r"); got != "ghs_github_job_token" {
		t.Error("the runner's token must still work at its own server")
	}
}

// TestResolve_ExplicitTokenWinsAndIsUnrestricted pins the operator escape
// hatch: someone who types a secret next to the command has decided where it
// goes, and cross-forge work stays possible.
func TestResolve_ExplicitTokenWinsAndIsUnrestricted(t *testing.T) {
	testenv.New(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_TOKEN", "ghs_ambient")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")

	cred := clitoken.Resolve(tokenCmd(t, "--token", "typed_by_a_human"))

	if got := cred.For("https://anywhere.example/o/r"); got != "typed_by_a_human" {
		t.Error("an explicit --token must win and be honoured anywhere")
	}
}

// TestResolveRelease_PrefersDedicatedToken verifies release-token precedence.
// Token scope remains the operator's responsibility.
func TestResolveRelease_PrefersDedicatedToken(t *testing.T) {
	testenv.New(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_TOKEN", "ghs_ambient")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("RELEASE_TOKEN", "dedicated_write_token")

	cred := clitoken.ResolveRelease(tokenCmd(t))

	if got := cred.For("https://github.com/o/r"); got != "dedicated_write_token" {
		t.Error("dedicated write token must win")
	}
}

func TestTokenTests_IsolateAmbientSources(t *testing.T) {
	t.Setenv("CI_TOKEN", "synthetic-ambient-canary")
	t.Setenv("RELEASE_TOKEN", "synthetic-release-canary")
	t.Run("implicit", TestResolve_ProvenLeakIsClosed)
	t.Run("explicit", TestResolve_ExplicitTokenWinsAndIsUnrestricted)
	t.Run("dedicated", TestResolveRelease_PrefersDedicatedToken)
}

// TestResolve_LineEndingPolicyDiffersBySource pins an asymmetry that is
// deliberate and was written down in runcontext but never checked at the CLI
// boundary where operators meet it.
//
// An AMBIENT token is trimmed of trailing CR/LF. Those values arrive from a
// runner or a secret store, and a stored secret picking up a trailing newline
// is the single most common way a credential arrives subtly wrong — sent
// verbatim it produces an authentication failure that looks like a bad token
// rather than a formatting problem.
//
// An EXPLICIT --token is taken verbatim. It is not the same case: the operator
// typed this value, so the CLI has no business deciding which of its bytes were
// meant. Trimming it would also make --token and the ambient chain silently
// disagree about what the credential IS.
//
// Both halves are asserted here, together, because each one only makes sense
// against the other — and because a change that unified them would look like a
// simplification and would quietly alter what gets sent over the wire.
func TestResolve_LineEndingPolicyDiffersBySource(t *testing.T) {
	t.Run("an ambient token is trimmed", func(t *testing.T) {
		for _, tc := range []struct{ name, stored, want string }{
			{name: "unix newline", stored: "ghs_tok\n", want: "ghs_tok"},
			{name: "windows newline", stored: "ghs_tok\r\n", want: "ghs_tok"},
			{name: "bare carriage return", stored: "ghs_tok\r", want: "ghs_tok"},
			{name: "several trailing newlines", stored: "ghs_tok\n\n", want: "ghs_tok"},
			{name: "no trailing newline", stored: "ghs_tok", want: "ghs_tok"},
			// Only TRAILING line endings come off; a token is opaque
			// otherwise, and trimming more would corrupt a valid one.
			{name: "leading space is preserved", stored: " ghs_tok\n", want: " ghs_tok"},
			{name: "trailing space is preserved", stored: "ghs_tok \n", want: "ghs_tok "},
		} {
			t.Run(tc.name, func(t *testing.T) {
				testenv.New(t)
				t.Setenv("GITHUB_ACTIONS", "true")
				t.Setenv("GITHUB_SERVER_URL", "https://github.com")
				t.Setenv("GITHUB_TOKEN", tc.stored)

				got := clitoken.Resolve(tokenCmd(t)).For("https://github.com/o/r")
				if got != tc.want {
					t.Errorf("ambient token = %q, want %q", got, tc.want)
				}
			})
		}
	})

	t.Run("an explicit --token is verbatim", func(t *testing.T) {
		testenv.New(t)

		const typed = "typed_by_a_human\n"

		got := clitoken.Resolve(tokenCmd(t, "--token", typed)).For("https://anywhere.example/o/r")
		if got != typed {
			t.Errorf("explicit token = %q, want it unchanged (%q)", got, typed)
		}
	})

	t.Run("the release chain trims its ambient token too", func(t *testing.T) {
		testenv.New(t)
		t.Setenv("GITHUB_ACTIONS", "true")
		t.Setenv("GITHUB_SERVER_URL", "https://github.com")
		t.Setenv("RELEASE_TOKEN", "dedicated_write_token\r\n")

		got := clitoken.ResolveRelease(tokenCmd(t)).For("https://github.com/o/r")
		if got != "dedicated_write_token" {
			t.Errorf("release token = %q, want it trimmed", got)
		}
	})
}
