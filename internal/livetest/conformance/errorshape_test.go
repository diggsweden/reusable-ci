// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// PAR-UX-4: the same failure means the same thing on every forge.
//
// Error text is the least disciplined surface in any tool — it is written per
// adapter, in the moment, by whoever was closest to the API — so it is where
// forges drift apart first and where nothing was comparing them.
//
// The load-bearing assertion here is the exit code, not the wording. A caller
// scripts against exit codes: a refused credential that exits "wrong input" on
// one forge and "try again later" on another is a pipeline that retries forever
// against exactly one of them. Wording is checked only for the properties a
// reader needs — that something was said, that it names what failed, and that it
// is not a stack trace — because demanding identical sentences across forges
// would forbid an adapter from saying anything specific, which is the opposite
// of useful.

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

func TestErrors_SameFailureClass_ExitsTheSameOnEveryForge(t *testing.T) {
	// Each case is a failure every forge can produce, driven through the same
	// verb, so a difference in outcome is a difference in the adapter rather
	// than in what was asked of it.
	cases := []struct {
		name string

		// args builds the invocation for one forge. The bad ingredient differs
		// per case; everything else is held constant.
		args func(t *testing.T, target livetest.Target, repo string) []string
	}{
		{
			name: "refused credential",
			args: func(t *testing.T, target livetest.Target, repo string) []string {
				t.Helper()

				return []string{
					"validate", "auth", "token",
					"--token-file", writeToken(t, "gpl-0000000000000000000000000000000000000000000000000000000000000bad"),
					"--repository", livetest.RepoSlug(target, repo),
				}
			},
		},
		{
			name: "repository that does not exist",
			args: func(t *testing.T, target livetest.Target, _ string) []string {
				t.Helper()

				return []string{
					"validate", "auth", "token",
					"--token-file", writeToken(t, target.Token),
					"--repository", target.Owner + "/rc-absent-repository-par-ux-4",
				}
			},
		},
	}

	kinds := forgesClaiming(t, alwaysValidatesTokens, "token validation")

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			exits := map[provider.ForgeAPI]int{}

			for _, kind := range kinds {
				target := livetest.Accept(t, kind)
				repo := livetest.NewScratchRepo(t, target, "errshape")

				run := livetest.CLI(t, target, repo, testCase.args(t, target, repo)...)

				if run.ExitCode == 0 {
					t.Fatalf("%s: %s succeeded", kind, testCase.name)
				}

				exits[kind] = run.ExitCode
				assertReadableFailure(t, kind, testCase.name, run)
			}

			// The parity claim. Reported separately from the per-forge checks
			// because this is the one a consumer's pipeline actually depends on.
			reference := kinds[0]
			for _, kind := range kinds[1:] {
				if exits[kind] != exits[reference] {
					t.Errorf("%s: %s exits %d on %s but %d on %s — a caller cannot handle this failure the same way on both",
						testCase.name, testCase.name, exits[kind], kind, exits[reference], reference)
				}
			}
		})
	}
}

// assertReadableFailure checks the properties a person needs from a failure,
// without prescribing the sentence: something was said, on the right stream, it
// names the thing that failed, and it is not an internal crash.
func assertReadableFailure(t *testing.T, kind provider.ForgeAPI, scenario string, run livetest.Run) {
	t.Helper()

	if strings.TrimSpace(run.Stderr) == "" {
		t.Errorf("%s: %s failed silently on stderr, leaving nothing to act on", kind, scenario)

		return
	}

	// An internal error means the failure was never classified: the user is
	// shown our stack instead of their problem.
	lower := strings.ToLower(run.Stderr)
	for _, leak := range []string{"panic:", "goroutine ", "runtime error"} {
		if strings.Contains(lower, leak) {
			t.Errorf("%s: %s surfaced an internal crash (%q)\nstderr: %s", kind, scenario, leak, run.Stderr)
		}
	}

	if run.ExitCode == int(errs.ExitCodeSoftware) {
		t.Errorf("%s: %s exits %d (internal error), so a caller is told to file a bug for their own input\nstderr: %s",
			kind, scenario, run.ExitCode, run.Stderr)
	}

	// Something identifying has to appear, or the reader cannot tell which of
	// several inputs was wrong.
	named := false
	for _, subject := range []string{"token", "repository", "repo", "permission", "auth", "not found", string(kind)} {
		if strings.Contains(lower, subject) {
			named = true

			break
		}
	}

	if !named {
		t.Errorf("%s: %s names neither the credential, the repository nor the forge\nstderr: %s",
			kind, scenario, run.Stderr)
	}
}
