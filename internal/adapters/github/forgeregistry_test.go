// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github_test

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestResolveForgeMavenRegistry_GitHub(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: func(k string) string {
		return map[string]string{"GITHUB_REPOSITORY": "org/app", "GITHUB_ACTOR": "ci", "GITHUB_TOKEN": "ght"}[k]
	}}

	reg, err := p.ResolveForgeMavenRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if reg.URL != "https://maven.pkg.github.com/org/app" || reg.AuthScheme != provider.MavenAuthServerPassword || reg.Username != "ci" {
		t.Errorf("registry = %+v", reg)
	}
}

func TestResolveForgeMavenRegistry_GitHub_MissingRepo(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: func(string) string { return "" }}
	if _, err := p.ResolveForgeMavenRegistry(); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("missing GITHUB_REPOSITORY should be ErrUsage, got %v", err)
	}
}

func TestResolveForgeNPMRegistry_GitHub(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: func(k string) string {
		return map[string]string{"GITHUB_REPOSITORY_OWNER": "org", "GITHUB_TOKEN": "ght"}[k]
	}}

	reg, err := p.ResolveForgeNPMRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if reg.Registry != "https://npm.pkg.github.com" || reg.Scope != "@org" || reg.Token != "ght" {
		t.Errorf("npm registry = %+v", reg)
	}
}

// TestResolveForgeMavenRegistry_ForgeNeutralNames pins the fix: the GitHub
// package registry understands the forge-neutral $REPOSITORY, so a run
// configured with it no longer resolves in `release publish` and fails here
// with "$GITHUB_REPOSITORY is required".
func TestResolveForgeMavenRegistry_ForgeNeutralNames(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: func(k string) string {
		return map[string]string{"REPOSITORY": "owner/repo", "GITHUB_TOKEN": "gt"}[k]
	}}

	reg, err := p.ResolveForgeMavenRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if reg.URL != "https://maven.pkg.github.com/owner/repo" {
		t.Errorf("registry = %+v", reg)
	}
}

// TestResolveForgeMavenRegistry_TokenIsNotCrossForge is a SECURITY boundary,
// not a style preference.
//
// The obvious "finish the migration" change here is Token:
// runcontext.Token().Resolve(env). That chain consults FORGEJO_TOKEN BEFORE
// GITHUB_TOKEN, so with both set this resolver would hand a Forgejo
// credential to github.com. A cross-forge token cannot authenticate anyway,
// so the only outcomes are auth failure or disclosure to a host the token
// was never issued for.
//
// This test fails the moment the token starts spanning forges.
func TestResolveForgeMavenRegistry_TokenIsNotCrossForge(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: func(k string) string {
		return map[string]string{
			"REPOSITORY":    "owner/repo",
			"FORGEJO_TOKEN": "forgejo-secret-for-another-host",
			"CI_TOKEN":      "neutral-secret-for-another-host",
			"GITHUB_TOKEN":  "github-token",
		}[k]
	}}

	reg, err := p.ResolveForgeMavenRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if reg.Token != "github-token" {
		t.Errorf("token sent to github.com = %q, want the $GITHUB_TOKEN value;"+
			" a non-GitHub credential must never be transmitted to github.com", reg.Token)
	}
}
