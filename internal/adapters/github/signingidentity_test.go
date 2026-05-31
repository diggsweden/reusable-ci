// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github_test

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestResolveKeylessIdentity_GitHub(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: func(k string) string {
		return map[string]string{
			"GITHUB_REPOSITORY":   "acme/app",
			"GITHUB_SERVER_URL":   "https://github.com",
			"GITHUB_WORKFLOW_REF": "acme/app/.github/workflows/release.yml@refs/tags/v1.2.3",
		}[k]
	}}

	if !p.SupportsKeyless() {
		t.Fatal("GitHub should support keyless")
	}

	id, err := p.ResolveKeylessIdentity()
	if err != nil {
		t.Fatal(err)
	}

	if id.OIDCIssuer != "https://token.actions.githubusercontent.com" {
		t.Errorf("OIDCIssuer = %q", id.OIDCIssuer)
	}

	if id.TokenAudience != "sigstore" {
		t.Errorf("TokenAudience = %q", id.TokenAudience)
	}

	if want := "https://github.com/acme/app/.github/workflows/release.yml@refs/tags/v1.2.3"; id.SubjectID != want {
		t.Errorf("SubjectID = %q, want %q", id.SubjectID, want)
	}

	if want := `^https://github\.com/acme/app/`; id.SubjectRegexp != want {
		t.Errorf("SubjectRegexp = %q, want %q", id.SubjectRegexp, want)
	}
}

func TestResolveKeylessIdentity_GitHub_DefaultServerAndNoRef(t *testing.T) {
	t.Parallel()

	// GITHUB_SERVER_URL absent → defaults to github.com; no workflow ref →
	// SubjectID empty but SubjectRegexp still anchored.
	p := &github.Provider{Env: func(k string) string {
		return map[string]string{"GITHUB_REPOSITORY": "acme/app"}[k]
	}}

	id, err := p.ResolveKeylessIdentity()
	if err != nil {
		t.Fatal(err)
	}

	if id.SubjectID != "" {
		t.Errorf("SubjectID = %q, want empty", id.SubjectID)
	}

	if want := `^https://github\.com/acme/app/`; id.SubjectRegexp != want {
		t.Errorf("SubjectRegexp = %q, want %q", id.SubjectRegexp, want)
	}
}

func TestResolveKeylessIdentity_GitHub_MissingRepo(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: func(string) string { return "" }}
	if _, err := p.ResolveKeylessIdentity(); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("missing GITHUB_REPOSITORY should be ErrUsage, got %v", err)
	}
}
