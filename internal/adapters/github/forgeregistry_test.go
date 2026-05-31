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
