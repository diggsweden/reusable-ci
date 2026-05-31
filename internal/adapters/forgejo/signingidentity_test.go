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

	// The Forgejo runner also sets GITHUB_* vars; the resolver falls back to them.
	p := &forgejo.Provider{Env: func(k string) string {
		return map[string]string{
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
