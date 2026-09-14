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

func TestResolveRegistryAuth_GitLab(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: func(k string) string {
		return map[string]string{
			"CI_REGISTRY":          "registry.gitlab.com",
			"CI_REGISTRY_PASSWORD": "jobtok",
		}[k]
	}}

	auth, err := p.ResolveRegistryAuth()
	if err != nil {
		t.Fatal(err)
	}

	// CI_REGISTRY_USER unset → defaults to gitlab-ci-token.
	if auth.Registry != "registry.gitlab.com" || auth.Username != "gitlab-ci-token" || auth.Token != "jobtok" {
		t.Errorf("auth = %+v", auth)
	}
}

// TestResolveRegistryAuth_GitLab_NoCreds separates the ways the runner
// environment can be incomplete.
//
// Only the everything-unset case was covered, which a guard checking just one
// of the two variables satisfies. Each is independently required: a host with
// no token and a token with no host are both unusable, and both are what a job
// that sets its registry variables by hand actually produces.
//
// The refusal must also return nothing. A RegistryAuth carrying the one value
// that was present is a partial credential a caller may still try to use.
func TestResolveRegistryAuth_GitLab_NoCreds(t *testing.T) {
	t.Parallel()

	for name, env := range map[string]map[string]string{
		"nothing set":           {},
		"no registry host":      {"CI_REGISTRY_PASSWORD": "jobtok"},
		"no token":              {"CI_REGISTRY": "registry.gitlab.com"},
		"a blank registry host": {"CI_REGISTRY": "   ", "CI_REGISTRY_PASSWORD": "jobtok"},
		"a blank token":         {"CI_REGISTRY": "registry.gitlab.com", "CI_REGISTRY_PASSWORD": "  \n "},
		"only an explicit user": {"CI_REGISTRY_USER": "deploy-bot"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := &gitlab.Provider{Env: func(k string) string { return env[k] }}

			auth, err := p.ResolveRegistryAuth()
			if !errors.Is(err, errs.ErrCIRuntimeRequired) {
				t.Fatalf("err = %v, want ErrCIRuntimeRequired", err)
			}

			if auth != (provider.RegistryAuth{}) {
				t.Errorf("a refused lookup returned the partial credential %+v", auth)
			}
		})
	}
}

// TestResolveRegistryAuth_GitLab_ExplicitUser covers the override beside the
// default. Only the default was executed, so a resolver that ignored
// CI_REGISTRY_USER and always logged in as gitlab-ci-token would have passed —
// and a project that sets a deploy user does so because the job token cannot
// reach the registry it is pushing to.
func TestResolveRegistryAuth_GitLab_ExplicitUser(t *testing.T) {
	t.Parallel()

	p := &gitlab.Provider{Env: func(k string) string {
		return map[string]string{
			"CI_REGISTRY":          "registry.gitlab.com",
			"CI_REGISTRY_USER":     "  deploy-bot  ",
			"CI_REGISTRY_PASSWORD": " jobtok ",
		}[k]
	}}

	auth, err := p.ResolveRegistryAuth()
	if err != nil {
		t.Fatal(err)
	}

	// Every field trimmed: these are assembled into a `docker login`
	// invocation, and a credential with a trailing newline is rejected by the
	// registry rather than by anything that can explain it.
	want := provider.RegistryAuth{Registry: "registry.gitlab.com", Username: "deploy-bot", Token: "jobtok"}
	if auth != want {
		t.Errorf("auth = %+v, want %+v", auth, want)
	}
}
