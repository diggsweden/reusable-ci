// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// ResolveRegistryAuth returns the runner-injected credentials for the Forgejo
// instance's container registry, which lives at the same host as the forge
// (serverURL). Username is the runner actor; no separate secret.
//
// The token comes from the $CI_TOKEN / $FORGEJO_TOKEN / $GITEA_TOKEN /
// $GITHUB_TOKEN chain, but "which one is set" is only half the answer: the
// credential is handed over only if it was issued by serverURL. On a GitHub
// runner publishing to a Forgejo instance, $GITHUB_TOKEN is GitHub's own job
// token — it cannot authenticate here, and must not be sent here.
func (p *Provider) ResolveRegistryAuth() (provider.RegistryAuth, error) {
	// Presence needs no destination, so "none configured at all" stays the
	// first and clearest thing to report.
	cred := p.credential()
	if !cred.Present() {
		return provider.RegistryAuth{}, errs.RuntimeRequired(
			"log in to the container registry", "forgejo", []errs.EnvVar{
				{Name: "FORGEJO_TOKEN", What: "the runner-injected registry token (or GITEA_TOKEN / GITHUB_TOKEN)"},
			})
	}

	server, err := p.serverURL()
	if err != nil {
		return provider.RegistryAuth{}, err
	}

	// The registry lives at the forge's own host, so that is the audience the
	// credential must be valid for.
	token := cred.For(server)
	if token == "" {
		return provider.RegistryAuth{}, fmt.Errorf(
			"a token is set, but it was issued by a different server than %s, so it"+
				" cannot authenticate there; set $FORGEJO_TOKEN (or $GITEA_TOKEN) to a"+
				" credential for that host: %w", server, errs.ErrUsage)
	}

	return provider.RegistryAuth{
		// serverURL carries a scheme; RegistryAuth.MatchesRegistry strips it
		// before comparing, so the host lines up with the login target.
		Registry: server,
		Username: strings.TrimSpace(runcontext.Actor().Resolve(p.envFunc())),
		Token:    token,
	}, nil
}
