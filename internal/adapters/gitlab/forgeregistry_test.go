// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab_test

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestResolveForgeMavenRegistry_GitLab(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: func(k string) string {
		return map[string]string{"CI_API_V4_URL": "https://gl.example.com/api/v4/", "CI_PROJECT_ID": "42", "CI_JOB_TOKEN": "jt"}[k]
	}}

	reg, err := p.ResolveForgeMavenRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if reg.URL != "https://gl.example.com/api/v4/projects/42/packages/maven" || reg.AuthScheme != provider.MavenAuthJobTokenHeader || reg.Token != "jt" {
		t.Errorf("registry = %+v", reg)
	}
}

func TestResolveForgeMavenRegistry_GitLab_MissingProjectID(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: func(k string) string { return map[string]string{"CI_API_V4_URL": "https://gl/api/v4"}[k] }}
	if _, err := p.ResolveForgeMavenRegistry(); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("missing CI_PROJECT_ID should be ErrUsage, got %v", err)
	}
}

func TestResolveForgeNPMRegistry_GitLab(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: func(k string) string {
		return map[string]string{"CI_API_V4_URL": "https://gl/api/v4", "CI_PROJECT_ID": "7", "CI_JOB_TOKEN": "jt"}[k]
	}}

	reg, err := p.ResolveForgeNPMRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if reg.Registry != "https://gl/api/v4/projects/7/packages/npm/" || reg.Token != "jt" || reg.Scope != "" {
		t.Errorf("npm registry = %+v", reg)
	}
}
