// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// ResolveRegistryAuth returns the runner-injected credentials for GitHub
// Container Registry (ghcr.io): the actor as username and the job's
// $GITHUB_TOKEN as the password, so `container login` needs no separately
// managed registry secret.
func (p *Provider) ResolveRegistryAuth() (provider.RegistryAuth, error) {
	env := p.envFunc()

	token := env("GITHUB_TOKEN")
	if token == "" {
		return provider.RegistryAuth{}, errs.RuntimeRequired(
			"log in to the container registry", "github", []errs.EnvVar{
				{Name: "GITHUB_TOKEN", What: "the runner-injected registry token"},
			})
	}

	return provider.RegistryAuth{
		Registry: "ghcr.io",
		Username: strings.TrimSpace(env("GITHUB_ACTOR")),
		Token:    token,
	}, nil
}
