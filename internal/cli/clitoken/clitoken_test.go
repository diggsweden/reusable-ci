// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package clitoken_test

import (
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/clitoken"
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
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_TOKEN", "ghs_github_job_token")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("FORGEJO_SERVER_URL", "https://third-party.example")
	t.Setenv("FORGEJO_TOKEN", "")

	cred := clitoken.Resolve(tokenCmd(t))

	if got := cred.For("https://third-party.example/o/r"); got != "" {
		t.Errorf("GitHub's job token must not reach a third-party host; got %q", got)
	}

	if got := cred.For("https://github.com/o/r"); got != "ghs_github_job_token" {
		t.Errorf("the runner's token must still work at its own server; got %q", got)
	}
}

// TestResolve_ExplicitTokenWinsAndIsUnrestricted pins the operator escape
// hatch: someone who types a secret next to the command has decided where it
// goes, and cross-forge work stays possible.
func TestResolve_ExplicitTokenWinsAndIsUnrestricted(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_TOKEN", "ghs_ambient")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")

	cred := clitoken.Resolve(tokenCmd(t, "--token", "typed_by_a_human"))

	if got := cred.For("https://anywhere.example/o/r"); got != "typed_by_a_human" {
		t.Errorf("an explicit --token must win and be honoured anywhere; got %q", got)
	}
}

// TestResolveRelease_PrefersDedicatedToken pins that the release chain still
// puts least privilege first.
func TestResolveRelease_PrefersDedicatedToken(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_TOKEN", "ghs_ambient")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("RELEASE_TOKEN", "dedicated_write_token")

	cred := clitoken.ResolveRelease(tokenCmd(t))

	if got := cred.For("https://github.com/o/r"); got != "dedicated_write_token" {
		t.Errorf("dedicated write token must win; got %q", got)
	}
}
