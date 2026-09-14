// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

// These run in the ordinary suite, with no build tag and no forge. That is
// deliberate: the guard is the thing standing between a typo and a destroyed
// server, so its refusals must be proven on every commit, not only on the rare
// occasion someone has a lab running.
//
// It touches unexported state (the pure validate function and the accepted
// flag), so it is a same-package test in an _internal_test.go file per the
// repository's carve-out convention.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// errTransportBlocked is what the blocked transport returns; tests match it by
// identity rather than by message.
var errTransportBlocked = errors.New("test transport blocked")

func validContract(now time.Time) (Target, contract, tokenMetadata) {
	// G101: every value here is inert; the guard only checks the token is
	// non-empty, and a fixture that looked less like a token would say less.
	target := Target{ //nolint:gosec
		Forge:              provider.ForgeForgejo,
		Host:               "forgejo.compose.forgelab:8443",
		Owner:              "garga",
		Token:              "fake-forgejo-token",
		CredentialUsername: "garga",
		RegistryOrigin:     "https://forgejo.compose.forgelab:8443",
	}

	// Derived, not asserted: the confirmation is the operator retyping what
	// this suite computes, so the fixture computes it the same way the guard
	// does rather than hard-coding a second copy that could drift.
	refs := []targetRef{{forge: "forgejo", host: "forgejo.compose.forgelab:8443", owner: "garga"}}

	identity, err := Identity("run-live-1", refs, ResourcePrefix)
	if err != nil {
		panic("livetest fixture: " + err.Error())
	}

	c := contract{
		runID:               "run-live-1",
		refs:                refs,
		resourcePrefix:      ResourcePrefix,
		confirmation:        confirmDestroy + "|" + identity,
		cleanupCommand:      "/opt/forge-lab/scripts/revoke-targets.sh",
		cleanupContractFile: "/tmp/run-live-1.recovery-v2.env",
	}

	token := tokenMetadata{
		id:                 "7",
		name:               "lab-targets-run-live-1",
		createdAt:          now.Add(-time.Minute).Format(time.RFC3339),
		revocationRequired: true,
		revocationRunID:    "run-live-1",
	}

	return target, c, token
}

// TestValidate_RejectsEachUnsafeContractForItsOwnReason mutates one field of a
// valid live-test contract at a time and requires the rule that guards that
// field to be the one that objects.
//
// The stakes are why the reason matters: validate decides whether a run is
// allowed to mutate a real forge. A mutation rejected by some other rule leaves
// the guard it was written for free to rot unnoticed.
func TestValidate_RejectsEachUnsafeContractForItsOwnReason(t *testing.T) {
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		mutate func(*Target, *contract, *tokenMetadata)
		// wantErrContains identifies the rule that must reject the mutation.
		// Every failure here wraps errs.ErrValidation, so identity cannot tell
		// them apart, and several mutations are caught by more than one rule:
		// a malformed run ID also breaks the confirmation identity that embeds
		// it. Without this, deleting the run-ID format check left the row named
		// for it still passing.
		wantErrContains string
	}{
		{name: "complete_forgejo_contract"},
		{
			name:            "malformed_run_id",
			wantErrContains: "does not match",
			mutate:          func(_ *Target, c *contract, _ *tokenMetadata) { c.runID = "Run-Live-1" },
		},
		{
			// A namespace that is not ours must not arm us however
			// self-consistent the rest of the run looks.
			name:            "another_consumers_namespace",
			wantErrContains: "but this suite owns",
			mutate: func(_ *Target, c *contract, _ *tokenMetadata) {
				c.resourcePrefix = "cl-"
				c.confirmation = confirmDestroy +
					"|run=run-live-1|targets=forgejo@https://forgejo.compose.forgelab:8443/garga#resources=cl-"
			},
		},
		{
			// The producer no longer supplies an owner, so an endpoint the
			// operator never armed must leave the run with nothing to act on.
			name:            "no_owner_declared",
			wantErrContains: "has a declared",
			mutate:          func(_ *Target, c *contract, _ *tokenMetadata) { c.refs = nil },
		},
		{
			name:            "malformed_namespace",
			wantErrContains: "is not a namespace",
			mutate:          func(_ *Target, c *contract, _ *tokenMetadata) { c.resourcePrefix = "rc" },
		},
		{
			name:            "host_outside_disposable_suffix",
			wantErrContains: "is not a disposable lab forge",
			mutate:          func(tg *Target, _ *contract, _ *tokenMetadata) { tg.Host = "codeberg.org" },
		},
		{
			name:            "host_with_invalid_port",
			wantErrContains: "has an invalid port",
			mutate:          func(tg *Target, _ *contract, _ *tokenMetadata) { tg.Host = "forgejo.compose.forgelab:65536" },
		},
		{
			name:            "empty_token",
			wantErrContains: "token is empty",
			mutate:          func(tg *Target, _ *contract, _ *tokenMetadata) { tg.Token = "" },
		},
		{
			name:            "owner_parent_directory",
			wantErrContains: "is not a resource owner",
			mutate:          func(tg *Target, _ *contract, _ *tokenMetadata) { tg.Owner = ".." },
		},
		{
			// The confirmation is the human half of the authorization: one
			// typed against a different run must not carry over to this one.
			name:            "confirmation_for_a_different_run",
			wantErrContains: "must equal",
			mutate: func(_ *Target, c *contract, _ *tokenMetadata) {
				c.confirmation = confirmDestroy + "|run=other|targets=x"
			},
		},
		{
			// A second armed forge changes the identity, so a confirmation
			// typed for one forge must not authorize a run spanning two.
			name:            "confirmation_predates_a_second_target",
			wantErrContains: "must equal",
			mutate: func(_ *Target, c *contract, _ *tokenMetadata) {
				c.refs = append(c.refs, targetRef{
					forge: "gitlab", host: "gitlab.compose.forgelab", owner: "garga",
				})
			},
		},
		{
			name:            "confirmation_missing_identity",
			wantErrContains: "must equal",
			mutate:          func(_ *Target, c *contract, _ *tokenMetadata) { c.confirmation = confirmDestroy },
		},
		{
			name:            "token_not_bound_to_run",
			wantErrContains: "is not bound to run",
			mutate:          func(_ *Target, _ *contract, tk *tokenMetadata) { tk.name = "lab-targets-some-other-run" },
		},
		{
			name:            "non_expiring_token_without_revocation",
			wantErrContains: "requires run-bound revocation metadata",
			mutate:          func(_ *Target, _ *contract, tk *tokenMetadata) { tk.revocationRequired = false },
		},
		{
			name:            "non_expiring_token_older_than_a_day",
			wantErrContains: "is older than 24 hours",
			mutate: func(_ *Target, _ *contract, tk *tokenMetadata) {
				tk.createdAt = now.Add(-25 * time.Hour).Format(time.RFC3339)
			},
		},
		{
			// Forgejo's API cannot attach an expiry, so one appearing is a
			// fabricated claim, not a bonus.
			name:            "fabricated_forgejo_expiry",
			wantErrContains: "has no native token expiry",
			mutate: func(_ *Target, _ *contract, tk *tokenMetadata) {
				tk.expiresAt = now.Add(24 * time.Hour).Format(time.RFC3339)
			},
		},
		{
			name:            "relative_cleanup_command",
			wantErrContains: "must be an absolute path",
			mutate:          func(_ *Target, c *contract, _ *tokenMetadata) { c.cleanupCommand = "scripts/revoke-targets.sh" },
		},
		{
			name:            "relative_cleanup_contract_file",
			wantErrContains: "contract_file must be an absolute path",
			mutate: func(_ *Target, c *contract, _ *tokenMetadata) {
				c.cleanupContractFile = "run-live-1.recovery-v2.env"
			},
		},
		{
			name:            "foreign_oci_authority",
			wantErrContains: "not the accepted local relationship",
			mutate: func(tg *Target, _ *contract, _ *tokenMetadata) {
				tg.RegistryOrigin = "https://registry.attacker.example"
			},
		},
		{
			name:            "foreign_fulcio_authority",
			wantErrContains: "not on the selected forge road",
			mutate: func(tg *Target, _ *contract, _ *tokenMetadata) {
				tg.FulcioURL = "https://fulcio.k3s.forgelab:8443"
				tg.OIDCIssuer = "https://forgejo.compose.forgelab:8443/api/actions"
			},
		},
		{
			name:            "derived_instead_of_mapped_issuer",
			wantErrContains: "OIDC issuer must equal",
			mutate: func(tg *Target, _ *contract, _ *tokenMetadata) {
				tg.FulcioURL = "https://fulcio.compose.forgelab:8443"
				tg.OIDCIssuer = "https://forgejo.compose.forgelab:8443"
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			target, contractValue, token := validContract(now)
			if testCase.mutate != nil {
				testCase.mutate(&target, &contractValue, &token)
			}

			err := validate(target, contractValue, token, now)

			if testCase.wantErrContains == "" {
				if err != nil {
					t.Fatalf("validate() rejected a valid contract: %v", err)
				}

				return
			}

			if err == nil {
				t.Fatalf("validate() accepted a contract it must reject")
			}

			if !strings.Contains(err.Error(), testCase.wantErrContains) {
				t.Fatalf("validate() rejected the contract, but not for the reason this row is about:\n got: %v\nwant it to mention: %q",
					err, testCase.wantErrContains)
			}
		})
	}
}

func TestValidateDisposableHost_RejectsRealForges(t *testing.T) {
	t.Parallel()

	for _, host := range []string{
		"codeberg.org",
		"gitlab.com",
		"github.com",
		"forgejo.compose.forgelab.evil.example",
		"",
		"not a host",
		"forgejo.compose.forgelab:0",
	} {
		if err := validateDisposableHost(host); err == nil {
			t.Errorf("validateDisposableHost(%q) accepted a host this tier must not mutate", host)
		}
	}

	for _, host := range []string{
		"forgejo.compose.forgelab:8443",
		"gitlab.k3s.forgelab",
		"gitea.compose.forgelab:8443",
	} {
		if err := validateDisposableHost(host); err != nil {
			t.Errorf("validateDisposableHost(%q) = %v, want accepted", host, err)
		}
	}
}

func TestRegistryHost_UsesDeclaredOrigin(t *testing.T) {
	t.Parallel()

	target := Target{
		Forge:          provider.ForgeGitLab,
		Host:           "gitlab.compose.forgelab:8443",
		RegistryOrigin: "https://registry.gitlab.compose.forgelab:8443",
	}

	host, err := RegistryHost(target)
	if err != nil {
		t.Fatal(err)
	}

	if host != "registry.gitlab.compose.forgelab:8443" {
		t.Fatalf("RegistryHost() = %q", host)
	}

	target.RegistryOrigin = ""
	if _, err := RegistryHost(target); err == nil {
		t.Fatal("RegistryHost derived an undeclared registry")
	}
}

func TestRequireAccepted_RefusesUnguardedTarget(t *testing.T) {
	t.Parallel()

	// A Target built by hand has never passed the guard, so no helper may act
	// through it however complete it looks.
	recorder := &fatalRecorder{}
	downstream := false

	defer func() {
		failure, ok := recover().(acceptanceFailure)
		if !ok || string(failure) != "livetest: refusing to act through a target the destructive guard never accepted" {
			t.Errorf("refusal = %q, want nonreturning Fatalf acceptance refusal", failure)
		}

		if downstream || recorder.failed || recorder.skipped {
			t.Errorf("guard returned or used Errorf/Skipf: downstream=%t recorder=%+v", downstream, recorder)
		}
	}()

	requireAccepted(stopOnFatalTB{TB: recorder}, Target{
		Forge: provider.ForgeForgejo,
		Host:  "forgejo.compose.forgelab:8443",
		Owner: "garga",
		Token: "token",
	})

	downstream = true
}

func TestDeleteScratchRepo_RefusesForeignNamespace(t *testing.T) {
	t.Parallel()

	target := Target{Forge: provider.ForgeForgejo, Host: "forgejo.compose.forgelab:8443", Owner: "garga", accepted: true}

	// cl- belongs to git-provider-clean. Even armed, this suite must not reach
	// outside the namespace it declared.
	if err := DeleteScratchRepo(t.Context(), target, "cl-runs-mixed"); !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("namespace refusal = %v, want namespace-specific ErrValidation", err)
	}
}

// These entrypoints must refuse before even listing remote resources. A blocked
// dialer makes a missing guard observable without ever opening a socket.
func TestScratchMutators_RejectUnacceptedTargetsBeforeTransport(t *testing.T) {
	original := http.DefaultTransport

	var calls atomic.Int32

	http.DefaultTransport = &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			calls.Add(1)

			return nil, errTransportBlocked
		},
	}

	t.Cleanup(func() { http.DefaultTransport = original })
	t.Setenv("RC_LIVE_TIMEOUT_SCRATCH", "20ms")

	for _, forge := range []provider.ForgeAPI{provider.ForgeForgejo, provider.ForgeGitLab} {
		for _, operation := range []string{"delete", "unique"} {
			t.Run(string(forge)+"/"+operation, func(t *testing.T) {
				calls.Store(0)

				target := Target{
					Forge: forge, Host: "unapproved.example.invalid", Owner: "test-owner",
					Token: "synthetic-test-token", ForgeAuthorities: []string{"https://unapproved.example.invalid"},
				}

				defer func() {
					if got := calls.Load(); got != 0 {
						t.Errorf("unaccepted target reached transport %d time(s)", got)
					}
				}()

				if operation == "delete" {
					ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
					defer cancel()

					err := DeleteScratchRepo(ctx, target, "rc-test")
					if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "unaccepted") {
						t.Fatalf("unaccepted cleanup = %v, want acceptance-specific ErrValidation", err)
					}

					return
				}

				defer func() {
					failure, ok := recover().(acceptanceFailure)
					if !ok || !strings.Contains(string(failure), "never accepted") {
						t.Errorf("unique scratch refusal = %q, want fatal acceptance guard", failure)
					}
				}()

				NewScratchRepoUnique(stopOnFatalTB{TB: t}, target, "test")
			})
		}
	}
}

type acceptanceFailure string

type stopOnFatalTB struct{ TB }

func (tb stopOnFatalTB) Fatalf(format string, args ...any) {
	panic(acceptanceFailure(fmt.Sprintf(format, args...)))
}

// fatalRecorder is the minimum TB that records a Fatalf without stopping the
// test, so a refusal can be asserted rather than crashing the run.
type fatalRecorder struct {
	failed  bool
	fatal   bool
	skipped bool
}

func (f *fatalRecorder) Helper()               {}
func (f *fatalRecorder) Logf(string, ...any)   {}
func (f *fatalRecorder) Skipf(string, ...any)  { f.failed, f.skipped = true, true }
func (f *fatalRecorder) Fatalf(string, ...any) { f.failed, f.fatal = true, true }
func (f *fatalRecorder) Errorf(string, ...any) { f.failed = true }
func (f *fatalRecorder) Cleanup(func())        {}
