// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// ResolveForgeMavenRegistry implements provider.ForgeMavenRegistryResolver for
// GitHub Packages: https://maven.pkg.github.com/<owner>/<repo>, authenticated
// with a username/password <server> ($GITHUB_ACTOR / $GITHUB_TOKEN).
func (p *Provider) ResolveForgeMavenRegistry() (provider.ForgeMavenRegistry, error) {
	env := p.envFunc()

	repo := strings.TrimSpace(env("GITHUB_REPOSITORY"))
	if repo == "" {
		return provider.ForgeMavenRegistry{}, fmt.Errorf("$GITHUB_REPOSITORY is required for GitHub Packages deploy: %w", errs.ErrUsage)
	}

	return provider.ForgeMavenRegistry{
		ServerID:   "github",
		URL:        "https://maven.pkg.github.com/" + repo,
		AuthScheme: provider.MavenAuthServerPassword,
		Username:   strings.TrimSpace(env("GITHUB_ACTOR")),
		Token:      env("GITHUB_TOKEN"),
	}, nil
}

// ResolveForgeNPMRegistry implements provider.ForgeNPMRegistryResolver for the
// GitHub npm registry: https://npm.pkg.github.com, scoped to the owner, with a
// host-scoped _authToken from $GITHUB_TOKEN.
func (p *Provider) ResolveForgeNPMRegistry() (provider.ForgeNPMRegistry, error) {
	env := p.envFunc()

	owner := strings.TrimSpace(env("GITHUB_REPOSITORY_OWNER"))
	if owner == "" {
		owner, _, _ = strings.Cut(strings.TrimSpace(env("GITHUB_REPOSITORY")), "/")
	}

	if owner == "" {
		return provider.ForgeNPMRegistry{}, fmt.Errorf("$GITHUB_REPOSITORY_OWNER is required for GitHub npm deploy: %w", errs.ErrUsage)
	}

	return provider.ForgeNPMRegistry{
		Registry: "https://npm.pkg.github.com",
		Scope:    "@" + owner,
		Token:    env("GITHUB_TOKEN"),
	}, nil
}
