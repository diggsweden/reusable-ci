// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// ResolveForgeMavenRegistry implements provider.ForgeMavenRegistryResolver for
// GitHub Packages: https://maven.pkg.github.com/<owner>/<repo>, authenticated
// with a username/password <server> ($GITHUB_ACTOR / $GITHUB_TOKEN).
//
// The repository comes from the shared chain, so a workflow exporting the
// forge-neutral $REPOSITORY is understood here as it is at every flag. The
// TOKEN deliberately does not: see the note on the Token field below.
func (p *Provider) ResolveForgeMavenRegistry() (provider.ForgeMavenRegistry, error) {
	env := p.envFunc()

	repo := strings.TrimSpace(runcontext.Repository().Resolve(env))
	if repo == "" {
		return provider.ForgeMavenRegistry{}, fmt.Errorf(
			"a repository (owner/repo) is required for GitHub Packages deploy (set one of %s): %w",
			runcontext.Repository(), errs.ErrUsage)
	}

	return provider.ForgeMavenRegistry{
		ServerID:   "github",
		URL:        "https://maven.pkg.github.com/" + repo,
		AuthScheme: provider.MavenAuthServerPassword,
		Username:   strings.TrimSpace(env("GITHUB_ACTOR")),
		// $GITHUB_TOKEN is read by NAME on purpose. Do not "finish the
		// migration" by switching this to runcontext.Token().
		//
		// That chain spans forges (CI_TOKEN, FORGEJO_TOKEN, GITEA_TOKEN,
		// GITHUB_TOKEN) and consults FORGEJO_TOKEN BEFORE GITHUB_TOKEN, so a
		// job that also targets a Forgejo instance would send its Forgejo
		// credential to github.com. A cross-forge token cannot authenticate
		// anyway, so such a chain has exactly two outcomes: auth failure, or
		// a credential disclosed to a host it was never issued for.
		//
		// Non-secret run context (repository, server, run id) spans forges
		// freely -- that is the point of neutrality. Credentials must not.
		Token: env("GITHUB_TOKEN"),
	}, nil
}

// ResolveForgeNPMRegistry implements provider.ForgeNPMRegistryResolver for the
// GitHub npm registry: https://npm.pkg.github.com, scoped to the owner, with a
// host-scoped _authToken from $GITHUB_TOKEN.
func (p *Provider) ResolveForgeNPMRegistry() (provider.ForgeNPMRegistry, error) {
	env := p.envFunc()

	owner := strings.TrimSpace(runcontext.RepositoryOwner().Resolve(env))
	if owner == "" {
		owner, _, _ = strings.Cut(strings.TrimSpace(runcontext.Repository().Resolve(env)), "/")
	}

	if owner == "" {
		return provider.ForgeNPMRegistry{}, fmt.Errorf(
			"an owner is required for GitHub npm deploy (set one of %s, or %s): %w",
			runcontext.RepositoryOwner(), runcontext.Repository(), errs.ErrUsage)
	}

	return provider.ForgeNPMRegistry{
		Registry: "https://npm.pkg.github.com",
		Scope:    "@" + owner,
		Token:    env("GITHUB_TOKEN"), // by name, deliberately -- see ResolveForgeMavenRegistry.
	}, nil
}
