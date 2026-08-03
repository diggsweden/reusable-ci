// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// ResolveRegistryAuth returns the runner-injected credentials for the Forgejo
// instance's container registry, which lives at the same host as the forge
// (serverURL). Username is the runner actor; the token is the standard
// $FORGEJO_TOKEN / $GITEA_TOKEN / $GITHUB_TOKEN cascade. No separate secret.
func (p *Provider) ResolveRegistryAuth() (provider.RegistryAuth, error) {
	token := p.token()
	if token == "" {
		return provider.RegistryAuth{}, errs.RuntimeRequired(
			"log in to the container registry", "forgejo", []errs.EnvVar{
				{Name: "FORGEJO_TOKEN", What: "the runner-injected registry token (or GITEA_TOKEN / GITHUB_TOKEN)"},
			})
	}

	server, err := p.serverURL()
	if err != nil {
		return provider.RegistryAuth{}, err
	}

	return provider.RegistryAuth{
		// serverURL carries a scheme; RegistryAuth.MatchesRegistry strips it
		// before comparing, so the host lines up with the login target.
		Registry: server,
		Username: strings.TrimSpace(firstNonEmpty(p.envFunc(), "FORGEJO_ACTOR", "GITHUB_ACTOR")),
		Token:    token,
	}, nil
}
