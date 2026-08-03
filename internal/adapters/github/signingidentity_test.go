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
			"GITHUB_ACTIONS":      "true",
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

// TestResolveKeylessIdentity_GitHub_NoRef covers the workflow-ref-less job:
// SubjectID is empty (nothing exact to report) but the anchor still pins
// verification to the repository.
func TestResolveKeylessIdentity_GitHub_NoRef(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: func(k string) string {
		return map[string]string{
			"GITHUB_ACTIONS":    "true",
			"GITHUB_REPOSITORY": "acme/app",
			"GITHUB_SERVER_URL": "https://github.com",
		}[k]
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

// TestResolveKeylessIdentity_GitHub_MissingServerFailsClosed pins that the
// anchor does NOT fall back to github.com when $GITHUB_SERVER_URL is absent.
//
// The old default guessed a FORGE, which is the one thing an anchor must
// never guess: a GitHub Enterprise Server runner also sets
// $GITHUB_ACTIONS=true, so defaulting would anchor a GHES repository to
// github.com and accept certificates issued for a same-named repo on a
// different host. Describing a run may default (see context.go, where the
// worst case is a cosmetic link); deciding what a signature check accepts may
// not. Erroring leaves the operator's own --cert-identity-regexp in place.
func TestResolveKeylessIdentity_GitHub_MissingServerFailsClosed(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: func(k string) string {
		return map[string]string{
			"GITHUB_ACTIONS":    "true",
			"GITHUB_REPOSITORY": "acme/app",
		}[k]
	}}

	if _, err := p.ResolveKeylessIdentity(); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("absent $GITHUB_SERVER_URL must fail closed, not default to github.com; got %v", err)
	}
}

func TestResolveKeylessIdentity_GitHub_MissingRepo(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: func(string) string { return "" }}
	if _, err := p.ResolveKeylessIdentity(); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("missing GITHUB_REPOSITORY should be ErrUsage, got %v", err)
	}
}
