// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import (
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// ResolveRegistryAuth returns the runner-injected credentials for the GitLab
// Container Registry. GitLab exposes them directly for `docker login`:
// $CI_REGISTRY (host), $CI_REGISTRY_USER (defaults to "gitlab-ci-token"), and
// $CI_REGISTRY_PASSWORD (the per-job token). No separately managed secret.
func (p *Provider) ResolveRegistryAuth() (provider.RegistryAuth, error) {
	env := p.envFunc()

	registry := strings.TrimSpace(env("CI_REGISTRY"))
	token := env("CI_REGISTRY_PASSWORD")

	if registry == "" || token == "" {
		return provider.RegistryAuth{}, errs.RuntimeRequired(
			"log in to the container registry", "gitlab", []errs.EnvVar{
				{Name: "CI_REGISTRY", What: "the GitLab registry host"},
				{Name: "CI_REGISTRY_PASSWORD", What: "the runner-injected registry token"},
			})
	}

	user := strings.TrimSpace(env("CI_REGISTRY_USER"))
	if user == "" {
		user = "gitlab-ci-token"
	}

	return provider.RegistryAuth{
		Registry: registry,
		Username: user,
		Token:    token,
	}, nil
}
