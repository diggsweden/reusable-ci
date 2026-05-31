// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// ResolveForgeMavenRegistry implements provider.ForgeMavenRegistryResolver for
// the Forgejo/Gitea package registry: <server>/api/packages/<owner>/maven,
// authenticated with an Authorization: token httpHeader. Prefers the native
// $FORGEJO_* vars, falling back to the $GITHUB_* / $GITEA_TOKEN aliases.
func (p *Provider) ResolveForgeMavenRegistry() (provider.ForgeMavenRegistry, error) {
	env := p.envFunc()

	server := strings.TrimRight(firstNonEmpty(env, "FORGEJO_SERVER_URL", "GITHUB_SERVER_URL"), "/")
	owner, _, ok := strings.Cut(firstNonEmpty(env, "FORGEJO_REPOSITORY", "GITHUB_REPOSITORY"), "/")

	if server == "" || !ok || owner == "" {
		return provider.ForgeMavenRegistry{}, fmt.Errorf("$FORGEJO_SERVER_URL and $FORGEJO_REPOSITORY (owner/repo) are required for Forgejo packages deploy: %w", errs.ErrUsage)
	}

	return provider.ForgeMavenRegistry{
		ServerID:   "forgejo",
		URL:        server + "/api/packages/" + owner + "/maven",
		AuthScheme: provider.MavenAuthTokenHeader,
		Token:      firstNonEmpty(env, "FORGEJO_TOKEN", "GITEA_TOKEN", "GITHUB_TOKEN"),
	}, nil
}

// ResolveForgeNPMRegistry implements provider.ForgeNPMRegistryResolver for the
// Forgejo/Gitea npm registry: <server>/api/packages/<owner>/npm/, scoped to the
// owner, with a path-scoped _authToken.
func (p *Provider) ResolveForgeNPMRegistry() (provider.ForgeNPMRegistry, error) {
	env := p.envFunc()

	server := strings.TrimRight(firstNonEmpty(env, "FORGEJO_SERVER_URL", "GITHUB_SERVER_URL"), "/")
	owner, _, ok := strings.Cut(firstNonEmpty(env, "FORGEJO_REPOSITORY", "GITHUB_REPOSITORY"), "/")

	if server == "" || !ok || owner == "" {
		return provider.ForgeNPMRegistry{}, fmt.Errorf("$FORGEJO_SERVER_URL and $FORGEJO_REPOSITORY (owner/repo) are required for Forgejo npm deploy: %w", errs.ErrUsage)
	}

	return provider.ForgeNPMRegistry{
		Registry: server + "/api/packages/" + owner + "/npm/",
		Scope:    "@" + owner,
		Token:    firstNonEmpty(env, "FORGEJO_TOKEN", "GITEA_TOKEN", "GITHUB_TOKEN"),
	}, nil
}
