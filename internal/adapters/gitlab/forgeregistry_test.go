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

// TestResolveForgeRegistries_GitLab_MissingContext runs both resolvers through
// the same missing-input matrix. Only a missing project ID was tested, and only
// for Maven, so a resolver that checked just one of its two inputs -- or npm
// checking neither -- passed. Whitespace counts as missing: a runner variable
// set to spaces produces a URL like " /projects/42" that fails far from here.
//
// A refusal returns the zero registry, never a half-built URL a caller could
// still try to deploy to.
func TestResolveForgeRegistries_GitLab_MissingContext(t *testing.T) {
	t.Parallel()

	for name, env := range map[string]map[string]string{
		"nothing set":           {},
		"no API URL":            {"CI_PROJECT_ID": "42", "CI_JOB_TOKEN": "jt"},
		"no project ID":         {"CI_API_V4_URL": "https://gl.example.com/api/v4", "CI_JOB_TOKEN": "jt"},
		"a blank API URL":       {"CI_API_V4_URL": "  ", "CI_PROJECT_ID": "42"},
		"a blank project ID":    {"CI_API_V4_URL": "https://gl.example.com/api/v4", "CI_PROJECT_ID": " \t"},
		"an API URL of slashes": {"CI_API_V4_URL": "///", "CI_PROJECT_ID": "42"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := &gitlab.Provider{Env: func(k string) string { return env[k] }}

			maven, err := p.ResolveForgeMavenRegistry()
			if !errors.Is(err, errs.ErrUsage) || maven != (provider.ForgeMavenRegistry{}) {
				t.Errorf("maven = (%+v, %v), want the zero registry and ErrUsage", maven, err)
			}

			npm, err := p.ResolveForgeNPMRegistry()
			if !errors.Is(err, errs.ErrUsage) || npm != (provider.ForgeNPMRegistry{}) {
				t.Errorf("npm = (%+v, %v), want the zero registry and ErrUsage", npm, err)
			}
		})
	}
}

// TestResolveForgeRegistries_GitLab_EmptyTokenPassesThrough pins the token
// policy rather than leaving it implicit. Resolution does not refuse an empty
// CI_JOB_TOKEN; the registry rejects the deploy instead. That matches the
// GitHub and Forgejo resolvers, which also pass their token through as found,
// and inside a GitLab job the runner always sets the variable -- an empty one
// means the command ran outside a job, where the missing URL and project ID
// normally refuse first. Changing this belongs to all three adapters at once.
func TestResolveForgeRegistries_GitLab_EmptyTokenPassesThrough(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: func(k string) string {
		return map[string]string{"CI_API_V4_URL": "https://gl.example.com/api/v4", "CI_PROJECT_ID": "42"}[k]
	}}

	maven, err := p.ResolveForgeMavenRegistry()
	if err != nil {
		t.Fatal(err)
	}

	wantMaven := provider.ForgeMavenRegistry{
		ServerID: "gitlab-maven", URL: "https://gl.example.com/api/v4/projects/42/packages/maven",
		AuthScheme: provider.MavenAuthJobTokenHeader,
	}
	if maven != wantMaven {
		t.Errorf("maven = %+v, want %+v", maven, wantMaven)
	}

	npm, err := p.ResolveForgeNPMRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if want := (provider.ForgeNPMRegistry{Registry: "https://gl.example.com/api/v4/projects/42/packages/npm/"}); npm != want {
		t.Errorf("npm = %+v, want %+v", npm, want)
	}
}
