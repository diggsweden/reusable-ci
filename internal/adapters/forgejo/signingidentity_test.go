// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// Forgejo reports SupportsKeyless()==false (public Fulcio does not trust a
// Forgejo issuer), but still resolves an anchored identity so a deployment
// with a trusting Fulcio + explicit --oidc-issuer can verify.
func TestResolveKeylessIdentity_Forgejo(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: func(k string) string {
		return map[string]string{
			"GITHUB_ACTIONS":     "true",
			"FORGEJO_ACTIONS":    "true",
			"FORGEJO_SERVER_URL": "https://codeberg.org",
			"FORGEJO_REPOSITORY": "acme/app",
		}[k]
	}}

	if p.SupportsKeyless() {
		t.Error("Forgejo keyless should be false out of the box")
	}

	id, err := p.ResolveKeylessIdentity()
	if err != nil {
		t.Fatal(err)
	}

	if id.OIDCIssuer != "" {
		t.Errorf("OIDCIssuer = %q, want empty (no auto-suppliable issuer)", id.OIDCIssuer)
	}

	if want := `^https://codeberg\.org/acme/app/`; id.SubjectRegexp != want {
		t.Errorf("SubjectRegexp = %q, want %q", id.SubjectRegexp, want)
	}
}

func TestResolveKeylessIdentity_Forgejo_GitHubEnvFallback(t *testing.T) {
	t.Parallel()

	// act_runner sets GITHUB_* as compat aliases for its own values, so the
	// anchor falls back to them -- but only because the runner markers below
	// say we are ON a Forgejo runner. Without them these names would mean
	// GitHub's repository, which must never anchor a Forgejo verification.
	p := &forgejo.Provider{Env: func(k string) string {
		return map[string]string{
			"GITHUB_ACTIONS":    "true",
			"FORGEJO_ACTIONS":   "true",
			"GITHUB_SERVER_URL": "https://git.example.org",
			"GITHUB_REPOSITORY": "team/svc",
		}[k]
	}}

	id, err := p.ResolveKeylessIdentity()
	if err != nil {
		t.Fatal(err)
	}

	if want := `^https://git\.example\.org/team/svc/`; id.SubjectRegexp != want {
		t.Errorf("SubjectRegexp = %q, want %q", id.SubjectRegexp, want)
	}
}

func TestResolveKeylessIdentity_Forgejo_MissingRepo(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: func(string) string { return "" }}
	if _, err := p.ResolveKeylessIdentity(); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("missing repository should be ErrUsage, got %v", err)
	}
}

// TestResolveKeylessIdentity_AnchorIgnoresOrchestrationValues pins that the
// verification anchor comes from the runner, not from the bare $REPOSITORY /
// $CI_SERVER_URL the orchestration layer computes.
//
// SubjectRegexp becomes cosign's --certificate-identity-regexp, so it decides
// which certificates are accepted. If the computed names could reach it, a
// value produced upstream of the signing boundary would widen what a
// signature check trusts — the scope escalation ADR 0002 forbids. Here they
// point at a different repository entirely, and must be ignored.
func TestResolveKeylessIdentity_AnchorIgnoresOrchestrationValues(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: func(k string) string {
		return map[string]string{
			"GITHUB_ACTIONS":  "true",
			"FORGEJO_ACTIONS": "true",
			// What the runner injected — the only admissible anchor.
			"FORGEJO_SERVER_URL": "https://codeberg.org",
			"FORGEJO_REPOSITORY": "real/repo",
			// What the orchestration layer computed. Correct for describing
			// the run; must never anchor a trust decision.
			"CI_SERVER_URL": "https://elsewhere.example",
			"REPOSITORY":    "other/repo",
			"CI_REPO":       "other/repo",
			"FORGEJO_REPO":  "other/repo",
		}[k]
	}}

	id, err := p.ResolveKeylessIdentity()
	if err != nil {
		t.Fatal(err)
	}

	if want := `^https://codeberg\.org/real/repo/`; id.SubjectRegexp != want {
		t.Errorf("SubjectRegexp = %q, want %q\n"+
			"the anchor must come from the runner-injected values, not the computed ones",
			id.SubjectRegexp, want)
	}
}

// TestResolveKeylessIdentity_GithubAliasNeedsForgejoRunner pins the other
// side of the gate: off a Forgejo runner, $GITHUB_REPOSITORY names GitHub's
// repository, so it must not anchor a Forgejo verification. Resolution fails
// instead, and keylessVerifyIdentity then leaves the operator's own
// --cert-identity-regexp in place — fail closed, not fail wrong.
func TestResolveKeylessIdentity_GithubAliasNeedsForgejoRunner(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: func(k string) string {
		return map[string]string{
			"GITHUB_ACTIONS":     "true", // a GitHub runner...
			"FORGEJO_SERVER_URL": "https://third-party.example",
			// A COMPLETE GitHub identity. Both names are set deliberately:
			// with either one missing, resolution would fail on the absent
			// value and this test would pass without ever exercising the
			// runner gate it exists to pin (mutation testing caught exactly
			// that -- deleting the gate left this test green).
			"GITHUB_SERVER_URL": "https://github.com",
			"GITHUB_REPOSITORY": "github-owner/github-repo",
		}[k]
	}}

	if _, err := p.ResolveKeylessIdentity(); err == nil {
		t.Error("want an error: $GITHUB_REPOSITORY on a GitHub runner names GitHub's" +
			" repository and must not anchor a Forgejo verification")
	}
}
