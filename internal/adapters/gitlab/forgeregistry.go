// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// ResolveForgeMavenRegistry implements provider.ForgeMavenRegistryResolver for
// the GitLab project-level Maven registry:
// <CI_API_V4_URL>/projects/<CI_PROJECT_ID>/packages/maven, authenticated with a
// Job-Token httpHeader ($CI_JOB_TOKEN).
func (p *Provider) ResolveForgeMavenRegistry() (provider.ForgeMavenRegistry, error) {
	env := p.envFunc()

	api := strings.TrimRight(strings.TrimSpace(env("CI_API_V4_URL")), "/")
	projectID := strings.TrimSpace(env("CI_PROJECT_ID"))

	if api == "" || projectID == "" {
		return provider.ForgeMavenRegistry{}, fmt.Errorf("$CI_API_V4_URL and $CI_PROJECT_ID are required for GitLab Package Registry deploy: %w", errs.ErrUsage)
	}

	return provider.ForgeMavenRegistry{
		ServerID:   "gitlab-maven",
		URL:        api + "/projects/" + projectID + "/packages/maven",
		AuthScheme: provider.MavenAuthJobTokenHeader,
		Token:      env("CI_JOB_TOKEN"),
	}, nil
}

// ResolveForgeNPMRegistry implements provider.ForgeNPMRegistryResolver for the
// GitLab project-level npm registry:
// <CI_API_V4_URL>/projects/<CI_PROJECT_ID>/packages/npm/, with a path-scoped
// _authToken from $CI_JOB_TOKEN. No scope is set (the project registry accepts
// any package name).
func (p *Provider) ResolveForgeNPMRegistry() (provider.ForgeNPMRegistry, error) {
	env := p.envFunc()

	api := strings.TrimRight(strings.TrimSpace(env("CI_API_V4_URL")), "/")
	projectID := strings.TrimSpace(env("CI_PROJECT_ID"))

	if api == "" || projectID == "" {
		return provider.ForgeNPMRegistry{}, fmt.Errorf("$CI_API_V4_URL and $CI_PROJECT_ID are required for GitLab npm deploy: %w", errs.ErrUsage)
	}

	return provider.ForgeNPMRegistry{
		Registry: api + "/projects/" + projectID + "/packages/npm/",
		Token:    env("CI_JOB_TOKEN"),
	}, nil
}
