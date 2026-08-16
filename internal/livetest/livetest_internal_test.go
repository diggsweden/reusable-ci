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
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func validContract(now time.Time) (Target, contract, tokenMetadata) {
	// G101: every value here is inert; the guard only checks the token is
	// non-empty, and a fixture that looked less like a token would say less.
	target := Target{ //nolint:gosec
		Forge: provider.ForgeForgejo,
		Host:  "forgejo.compose.forgelab:8443",
		Owner: "garga",
		Token: "fake-forgejo-token",
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
		runID:          "run-live-1",
		refs:           refs,
		resourcePrefix: ResourcePrefix,
		confirmation:   confirmDestroy + "|" + identity,
		cleanupCommand: "/opt/forge-lab/scripts/revoke-targets.sh",
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

func TestValidate_Contract_Scenarios(t *testing.T) {
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		mutate  func(*Target, *contract, *tokenMetadata)
		wantErr bool
	}{
		{name: "complete_forgejo_contract"},
		{
			name:    "malformed_run_id",
			mutate:  func(_ *Target, c *contract, _ *tokenMetadata) { c.runID = "Run-Live-1" },
			wantErr: true,
		},
		{
			// A namespace that is not ours must not arm us however
			// self-consistent the rest of the run looks.
			name: "another_consumers_namespace",
			mutate: func(_ *Target, c *contract, _ *tokenMetadata) {
				c.resourcePrefix = "cl-"
				c.confirmation = confirmDestroy +
					"|run=run-live-1|targets=forgejo@https://forgejo.compose.forgelab:8443/garga#resources=cl-"
			},
			wantErr: true,
		},
		{
			// The producer no longer supplies an owner, so an endpoint the
			// operator never armed must leave the run with nothing to act on.
			name:    "no_owner_declared",
			mutate:  func(_ *Target, c *contract, _ *tokenMetadata) { c.refs = nil },
			wantErr: true,
		},
		{
			name:    "malformed_namespace",
			mutate:  func(_ *Target, c *contract, _ *tokenMetadata) { c.resourcePrefix = "rc" },
			wantErr: true,
		},
		{
			name:    "host_outside_disposable_suffix",
			mutate:  func(tg *Target, _ *contract, _ *tokenMetadata) { tg.Host = "codeberg.org" },
			wantErr: true,
		},
		{
			name:    "host_with_invalid_port",
			mutate:  func(tg *Target, _ *contract, _ *tokenMetadata) { tg.Host = "forgejo.compose.forgelab:65536" },
			wantErr: true,
		},
		{
			name:    "empty_token",
			mutate:  func(tg *Target, _ *contract, _ *tokenMetadata) { tg.Token = "" },
			wantErr: true,
		},
		{
			name:    "owner_parent_directory",
			mutate:  func(tg *Target, _ *contract, _ *tokenMetadata) { tg.Owner = ".." },
			wantErr: true,
		},
		{
			// The confirmation is the human half of the authorization: one
			// typed against a different run must not carry over to this one.
			name: "confirmation_for_a_different_run",
			mutate: func(_ *Target, c *contract, _ *tokenMetadata) {
				c.confirmation = confirmDestroy + "|run=other|targets=x"
			},
			wantErr: true,
		},
		{
			// A second armed forge changes the identity, so a confirmation
			// typed for one forge must not authorize a run spanning two.
			name: "confirmation_predates_a_second_target",
			mutate: func(_ *Target, c *contract, _ *tokenMetadata) {
				c.refs = append(c.refs, targetRef{
					forge: "gitlab", host: "gitlab.compose.forgelab", owner: "garga",
				})
			},
			wantErr: true,
		},
		{
			name:    "confirmation_missing_identity",
			mutate:  func(_ *Target, c *contract, _ *tokenMetadata) { c.confirmation = confirmDestroy },
			wantErr: true,
		},
		{
			name:    "token_not_bound_to_run",
			mutate:  func(_ *Target, _ *contract, tk *tokenMetadata) { tk.name = "lab-targets-some-other-run" },
			wantErr: true,
		},
		{
			name:    "non_expiring_token_without_revocation",
			mutate:  func(_ *Target, _ *contract, tk *tokenMetadata) { tk.revocationRequired = false },
			wantErr: true,
		},
		{
			name: "non_expiring_token_older_than_a_day",
			mutate: func(_ *Target, _ *contract, tk *tokenMetadata) {
				tk.createdAt = now.Add(-25 * time.Hour).Format(time.RFC3339)
			},
			wantErr: true,
		},
		{
			// Forgejo's API cannot attach an expiry, so one appearing is a
			// fabricated claim, not a bonus.
			name: "fabricated_forgejo_expiry",
			mutate: func(_ *Target, _ *contract, tk *tokenMetadata) {
				tk.expiresAt = now.Add(24 * time.Hour).Format(time.RFC3339)
			},
			wantErr: true,
		},
		{
			name:    "relative_cleanup_command",
			mutate:  func(_ *Target, c *contract, _ *tokenMetadata) { c.cleanupCommand = "scripts/revoke-targets.sh" },
			wantErr: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			target, contractValue, token := validContract(now)
			if testCase.mutate != nil {
				testCase.mutate(&target, &contractValue, &token)
			}

			err := validate(target, contractValue, token, now)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("validate() error = %v, wantErr %v", err, testCase.wantErr)
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

func TestRequireAccepted_RefusesUnguardedTarget(t *testing.T) {
	t.Parallel()

	// A Target built by hand has never passed the guard, so no helper may act
	// through it however complete it looks.
	recorder := &fatalRecorder{}
	requireAccepted(recorder, Target{
		Forge: provider.ForgeForgejo,
		Host:  "forgejo.compose.forgelab:8443",
		Owner: "garga",
		Token: "token",
	})

	if !recorder.failed {
		t.Fatal("requireAccepted accepted a target the guard never armed")
	}
}

func TestDeleteScratchRepo_RefusesForeignNamespace(t *testing.T) {
	t.Parallel()

	target := Target{Forge: provider.ForgeForgejo, Host: "forgejo.compose.forgelab:8443", Owner: "garga"}

	// cl- belongs to git-provider-clean. Even armed, this suite must not reach
	// outside the namespace it declared.
	if err := DeleteScratchRepo(t.Context(), target, "cl-runs-mixed"); err == nil {
		t.Fatal("DeleteScratchRepo accepted a repository outside this suite's namespace")
	}
}

// fatalRecorder is the minimum TB that records a Fatalf without stopping the
// test, so a refusal can be asserted rather than crashing the run.
type fatalRecorder struct{ failed bool }

func (f *fatalRecorder) Helper()               {}
func (f *fatalRecorder) Logf(string, ...any)   {}
func (f *fatalRecorder) Skipf(string, ...any)  { f.failed = true }
func (f *fatalRecorder) Fatalf(string, ...any) { f.failed = true }
func (f *fatalRecorder) Errorf(string, ...any) { f.failed = true }
func (f *fatalRecorder) Cleanup(func())        {}
