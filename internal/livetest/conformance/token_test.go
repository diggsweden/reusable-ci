// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// PAR-TOK-*: what a forge says about a credential, and what the user is told
// when it is wrong.
//
// This is the first thing every adopter hits and the least forgiving place to
// differ: a token that is refused must be refused the same way on every forge,
// and the refusal must be distinguishable from the forge being down. Getting
// that wrong is not cosmetic — it decides whether CI retries.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

// alwaysValidatesTokens: every live forge implements TokenValidator, so the
// scenarios below run everywhere rather than being capability-gated. The
// predicate is spelled out so the iterator still reports what it selected.
func alwaysValidatesTokens(provider.Capabilities) bool { return true }

// PAR-TOK-1: the run's own credential validates against the repository it owns.
// The control: if this fails, nothing else in the tier means anything.
func TestToken_RunCredential_Validates(t *testing.T) {
	for _, kind := range forgesClaiming(t, alwaysValidatesTokens, "token validation") {
		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			repo := livetest.NewScratchRepo(t, target, "token-validate")

			forge := livetest.Provider(t, target, repo)

			validator, ok := forge.(provider.TokenValidator)
			if !ok {
				t.Fatalf("%s does not implement TokenValidator", kind)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			if err := validator.ValidateToken(ctx, target.Token, livetest.RepoSlug(target, repo)); err != nil {
				t.Fatalf("%s rejected its own run credential: %v", kind, err)
			}
		})
	}
}

// PAR-TOK-2: a refused credential is classified as refused, not as the forge
// being unavailable.
//
// This is the regression guard for a real defect. errs.FromHTTPStatus used to
// return nil for every 4xx it did not name, and each adapter then substituted
// ErrDependencyUnavailable — so "your token is not valid" reached the caller as
// "the dependency is down", exited 69, and told a retry loop to try again. The
// distinction only exists against a real server: an httptest fake answers
// whatever the fake's author expected.
func TestToken_RejectedCredential_IsPermissionDeniedNotUnavailable(t *testing.T) {
	for _, kind := range forgesClaiming(t, alwaysValidatesTokens, "token validation") {
		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			repo := livetest.NewScratchRepo(t, target, "token-reject")

			// A syntactically plausible credential the forge has never issued.
			// Not empty and not malformed: the point is to reach the server and
			// be told no, rather than to fail a local shape check.
			rejected := target
			rejected.Token = "gpl-00000000000000000000000000000000000000000000000000000000deadbeef"

			forge := livetest.Provider(t, rejected, repo)

			validator, ok := forge.(provider.TokenValidator)
			if !ok {
				t.Fatalf("%s does not implement TokenValidator", kind)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			err := validator.ValidateToken(ctx, rejected.Token, livetest.RepoSlug(target, repo))
			if err == nil {
				t.Fatalf("%s accepted a credential it never issued", kind)
			}

			if errors.Is(err, errs.ErrDependencyUnavailable) {
				t.Errorf("%s classified a refused credential as the dependency being unavailable, which tells CI to retry: %v",
					kind, err)
			}

			// Whatever the forge's status code, the class must be one the exit
			// ladder maps to "your input was wrong", never to "try later".
			if code := errs.ExitCodeFromError(err); code == errs.ExitCodeUnavailable {
				t.Errorf("%s exits %v for a refused credential; a caller cannot tell it apart from an outage",
					kind, code)
			}
		})
	}
}

// PAR-TOK-3: the same refusal, seen the way a user sees it.
//
// PAR-TOK-2 asserts the classification; this asserts that the classification
// survives the whole way out to a process exit and a message. They are separate
// because a correct sentinel that the CLI swallows is still a broken product.
func TestToken_RejectedCredential_FailsTheCommandWithAReason(t *testing.T) {
	for _, kind := range forgesClaiming(t, alwaysValidatesTokens, "token validation") {
		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			repo := livetest.NewScratchRepo(t, target, "token-cli")

			tokenFile := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(tokenFile, []byte("gpl-0000000000000000000000000000000000000000000000000000000000000bad\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			run := livetest.CLI(t, target, repo,
				"validate", "auth", "tokens",
				"--token-file", tokenFile,
				"--repository", livetest.RepoSlug(target, repo),
			)

			if run.ExitCode == 0 {
				t.Fatalf("%s: validating a credential the forge never issued exited 0\nstdout: %s", kind, run.Stdout)
			}

			if run.ExitCode == int(errs.ExitCodeUnavailable) {
				t.Errorf("%s: exits %d (unavailable) for a refused credential, so CI would retry a permanent failure\nstderr: %s",
					kind, run.ExitCode, run.Stderr)
			}

			// A refusal has to say something a reader can act on. Naming the
			// token or the permission is the minimum; a bare non-zero exit
			// sends someone to the source.
			lower := strings.ToLower(run.Stderr)
			if !strings.Contains(lower, "token") && !strings.Contains(lower, "permission") &&
				!strings.Contains(lower, "denied") && !strings.Contains(lower, "auth") {
				t.Errorf("%s: refusal names neither the credential nor the permission\nstderr: %s", kind, run.Stderr)
			}
		})
	}
}

// PAR-TOK-4: bot permissions report the access the run actually has.
//
// The run's credential owns its scratch repository, so every probe the role
// makes must come back reachable. A forge that reports false here while the
// suite is demonstrably able to create and delete that repository is
// contradicting itself.
func TestToken_BotPermissions_ReflectRealAccess(t *testing.T) {
	for _, kind := range forgesClaiming(t, alwaysValidatesTokens, "token validation") {
		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			repo := livetest.NewScratchRepo(t, target, "token-perms")

			forge := livetest.Provider(t, target, repo)

			validator, ok := forge.(provider.TokenValidator)
			if !ok {
				t.Fatalf("%s does not implement TokenValidator", kind)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			permissions, err := validator.ValidateBotPermissions(ctx, livetest.RepoSlug(target, repo))
			if err != nil {
				t.Fatalf("%s bot-permission probe failed against a repository it owns: %v", kind, err)
			}

			if permissions == nil {
				t.Fatalf("%s reported no permissions and no error", kind)
			}

			for name, reachable := range map[string]bool{
				"user":     permissions.UserAccessible,
				"repo":     permissions.RepoAccessible,
				"branches": permissions.BranchesAccessible,
			} {
				if !reachable {
					t.Errorf("%s reports %s unreachable for a repository this run just created", kind, name)
				}
			}
		})
	}
}
