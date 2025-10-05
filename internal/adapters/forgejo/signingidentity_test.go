// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// Forgejo splits the two keyless claims: it can mint an id-token, so it signs
// against a Fulcio that trusts the instance, but no public Fulcio does, so it
// is not keyless out of the box and a run must supply --oidc-issuer. The
// resolved identity stays anchored either way.
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

	if !p.SupportsKeyless() {
		t.Error("Forgejo mints OIDC id-tokens, so the signing-identity role must support keyless")
	}

	if p.Capabilities().PublicFulcioTrusted {
		t.Error("no public Fulcio trusts a Forgejo issuer, so keyless must not be the default")
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

	id, err := p.ResolveKeylessIdentity()
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage: $GITHUB_REPOSITORY on a GitHub runner names"+
			" GitHub's repository and must not anchor a Forgejo verification", err)
	}

	// Fail closed: nothing partially resolved may reach cosign's
	// --certificate-identity-regexp.
	if id.SubjectRegexp != "" || id.SubjectID != "" {
		t.Errorf("a refused resolution still returned an anchor: %+v", id)
	}
}

// TestResolveKeylessIdentity_EachMissingPrerequisiteRefusesOnItsOwn removes
// exactly one prerequisite at a time.
//
// The existing refusals drop the repository, or the runner marker, from an
// otherwise-sparse environment — so a case can be satisfied by a guard other
// than the one it names, and the missing server URL has no case at all. Each
// guard decides what a signature is anchored to: without the runner marker the
// attested names resolve to the HOST runner's repository, which is a real
// repository and the wrong one; without the server or the repository there is
// no subject to anchor at all. A guard that stopped firing would produce a
// signature verifiable against something the operator never chose.
func TestResolveKeylessIdentity_EachMissingPrerequisiteRefusesOnItsOwn(t *testing.T) {
	t.Parallel()

	complete := map[string]string{
		"GITHUB_ACTIONS":     "true",
		"FORGEJO_ACTIONS":    "true",
		"FORGEJO_SERVER_URL": "https://codeberg.org",
		"FORGEJO_REPOSITORY": "acme/app",
	}

	for _, tc := range []struct {
		name, drop, wantIn string
	}{
		{name: "no Forgejo runner marker", drop: "FORGEJO_ACTIONS", wantIn: "Forgejo runner"},
		{name: "no runner-provided repository", drop: "FORGEJO_REPOSITORY", wantIn: "repository"},
		{name: "no runner-provided server URL", drop: "FORGEJO_SERVER_URL", wantIn: "server URL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := make(map[string]string, len(complete))
			for k, v := range complete {
				if k != tc.drop {
					env[k] = v
				}
			}

			p := &forgejo.Provider{Env: func(k string) string { return env[k] }}

			id, err := p.ResolveKeylessIdentity()
			if !errors.Is(err, errs.ErrUsage) {
				t.Fatalf("dropping %s: err = %v, want ErrUsage", tc.drop, err)
			}

			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("dropping %s: err = %v, want it to name the missing prerequisite (%q)", tc.drop, err, tc.wantIn)
			}

			if id != (provider.KeylessIdentity{}) {
				t.Errorf("dropping %s: a partial identity was returned: %+v", tc.drop, id)
			}
		})
	}

	// The control: with everything present it resolves, so the cases above
	// cannot be satisfied by a resolver that always refuses.
	p := &forgejo.Provider{Env: func(k string) string { return complete[k] }}
	if _, err := p.ResolveKeylessIdentity(); err != nil {
		t.Fatalf("a complete environment was refused: %v", err)
	}
}

// TestResolveKeylessIdentity_PinsEveryIdentityField compares the whole
// identity, not the two fields the success test happens to read.
//
// Each field is a separate trust decision — which issuer is accepted, which
// audience the token must carry, which subject the certificate must name — and
// the audience and subject ID were never asserted. A wrong audience means the
// token is accepted by something it was not minted for.
func TestResolveKeylessIdentity_PinsEveryIdentityField(t *testing.T) {
	t.Parallel()

	p := &forgejo.Provider{Env: func(k string) string {
		// act_runner sets the GitHub names as compatibility aliases, so a
		// Forgejo runner is recognised by both markers being present.
		return map[string]string{
			"GITHUB_ACTIONS":     "true",
			"FORGEJO_ACTIONS":    "true",
			"FORGEJO_SERVER_URL": "https://codeberg.org",
			"FORGEJO_REPOSITORY": "acme/app",
		}[k]
	}}

	got, err := p.ResolveKeylessIdentity()
	if err != nil {
		t.Fatal(err)
	}

	want := provider.KeylessIdentity{
		OIDCIssuer:    "",
		TokenAudience: provider.KeylessAudience,
		SubjectID:     "https://codeberg.org/acme/app",
		SubjectRegexp: `^https://codeberg\.org/acme/app/`,
	}

	if got != want {
		t.Errorf("identity =\n  %+v\nwant\n  %+v", got, want)
	}
}
