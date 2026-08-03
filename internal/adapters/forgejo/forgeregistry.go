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

// packageRegistry is the shared shape of every Forgejo/Gitea package
// registry: <server>/api/packages/<owner>/…, authenticated with the forge
// token. Maven and npm differ only in the URL suffix and how they carry the
// token, so both resolvers derive from this.
type packageRegistry struct {
	server string
	owner  string
	token  string
}

// resolveRegistry resolves the run context a package registry URL is built
// from. kind names the ecosystem for the error message ("packages", "npm").
//
// Names and precedence come from runcontext, the same source the CLI flags
// bind to, so a workflow that exports the forge-neutral $REPOSITORY or
// $CI_SERVER_URL is understood here exactly as it is everywhere else. This
// adapter previously resolved from its own inline list, which is how it
// came to ignore the neutral names that `release publish` honoured.
func (p *Provider) resolveRegistry(kind string) (packageRegistry, error) {
	env := p.envFunc()

	server := strings.TrimRight(runcontext.ServerURL().Resolve(env), "/")
	if server == "" {
		return packageRegistry{}, fmt.Errorf(
			"a server url is required for Forgejo %s deploy (set one of %s): %w",
			kind, runcontext.ServerURL(), errs.ErrUsage)
	}

	owner, _, ok := strings.Cut(runcontext.Repository().Resolve(env), "/")
	if !ok || owner == "" {
		return packageRegistry{}, fmt.Errorf(
			"a repository (owner/repo) is required for Forgejo %s deploy (set one of %s): %w",
			kind, runcontext.Repository(), errs.ErrUsage)
	}

	return packageRegistry{server: server, owner: owner, token: runcontext.Token().Resolve(env)}, nil
}

// url builds the registry URL for an ecosystem's path suffix.
func (r packageRegistry) url(suffix string) string {
	return r.server + "/api/packages/" + r.owner + "/" + suffix
}

// ResolveForgeMavenRegistry implements provider.ForgeMavenRegistryResolver for
// the Forgejo/Gitea package registry: <server>/api/packages/<owner>/maven,
// authenticated with an Authorization: token httpHeader.
func (p *Provider) ResolveForgeMavenRegistry() (provider.ForgeMavenRegistry, error) {
	reg, err := p.resolveRegistry("packages")
	if err != nil {
		return provider.ForgeMavenRegistry{}, err
	}

	return provider.ForgeMavenRegistry{
		ServerID:   "forgejo",
		URL:        reg.url("maven"),
		AuthScheme: provider.MavenAuthTokenHeader,
		Token:      reg.token,
	}, nil
}

// ResolveForgeNPMRegistry implements provider.ForgeNPMRegistryResolver for the
// Forgejo/Gitea npm registry: <server>/api/packages/<owner>/npm/, scoped to the
// owner, with a path-scoped _authToken.
func (p *Provider) ResolveForgeNPMRegistry() (provider.ForgeNPMRegistry, error) {
	reg, err := p.resolveRegistry("npm")
	if err != nil {
		return provider.ForgeNPMRegistry{}, err
	}

	return provider.ForgeNPMRegistry{
		Registry: reg.url("npm/"),
		Scope:    "@" + reg.owner,
		Token:    reg.token,
	}, nil
}
