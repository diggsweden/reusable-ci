// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github_test

import (
	"errors"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestResolveForgeMavenRegistry_GitHub(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: func(k string) string {
		return map[string]string{
			"GITHUB_REPOSITORY": "org/app",
			"GITHUB_ACTOR":      "ci",
			"GITHUB_TOKEN":      "ght",
			"GITHUB_SERVER_URL": "https://github.com",
		}[k]
	}}

	reg, err := p.ResolveForgeMavenRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if reg.URL != "https://maven.pkg.github.com/org/app" || reg.AuthScheme != provider.MavenAuthServerPassword ||
		reg.Username != "ci" || reg.Token != "ght" {
		t.Errorf("registry = %+v", reg)
	}
}

func TestResolveForgeMavenRegistry_GitHub_MissingRepo(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: func(k string) string {
		return map[string]string{"GITHUB_SERVER_URL": "https://github.com"}[k]
	}}
	if _, err := p.ResolveForgeMavenRegistry(); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("missing GITHUB_REPOSITORY should be ErrUsage, got %v", err)
	}
}

func TestResolveForgeNPMRegistry_GitHub(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: func(k string) string {
		return map[string]string{
			"GITHUB_REPOSITORY_OWNER": "org",
			"GITHUB_TOKEN":            "ght",
			"GITHUB_SERVER_URL":       "https://github.com",
		}[k]
	}}

	reg, err := p.ResolveForgeNPMRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if reg.Registry != "https://npm.pkg.github.com" || reg.Scope != "@org" || reg.Token != "ght" {
		t.Errorf("npm registry = %+v", reg)
	}
}

func TestResolveForgeRegistries_GHESRefusesPublicEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		resolve func(*github.Provider) error
	}{
		{name: "maven", resolve: func(p *github.Provider) error {
			_, err := p.ResolveForgeMavenRegistry()

			return err
		}},
		{name: "npm", resolve: func(p *github.Provider) error {
			_, err := p.ResolveForgeNPMRegistry()

			return err
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			tokenRead := false
			p := &github.Provider{Env: func(k string) string {
				if k == "GITHUB_TOKEN" {
					tokenRead = true
				}

				return map[string]string{
					"GITHUB_REPOSITORY":       "org/app",
					"GITHUB_REPOSITORY_OWNER": "org",
					"GITHUB_TOKEN":            "ghes-token",
					"GITHUB_SERVER_URL":       "https://github.acme.example",
				}[k]
			}}

			if err := test.resolve(p); !errors.Is(err, errs.ErrUnsupported) {
				t.Errorf("GHES registry resolution should be ErrUnsupported, got %v", err)
			}

			if tokenRead {
				t.Error("GITHUB_TOKEN was read before GHES registry resolution was refused")
			}
		})
	}
}

func TestResolveForgeMavenRegistry_MissingServerRefusesPublicEndpoint(t *testing.T) {
	t.Parallel()

	tokenRead := false
	p := &github.Provider{Env: func(k string) string {
		if k == "GITHUB_TOKEN" {
			tokenRead = true
		}

		return map[string]string{
			"GITHUB_REPOSITORY": "org/app",
			"GITHUB_ACTOR":      "ci",
			"GITHUB_TOKEN":      "token",
		}[k]
	}}

	if _, err := p.ResolveForgeMavenRegistry(); !errors.Is(err, errs.ErrUsage) {
		t.Errorf("missing GITHUB_SERVER_URL should be ErrUsage, got %v", err)
	}

	if tokenRead {
		t.Error("GITHUB_TOKEN was read before the missing server URL was refused")
	}
}

// TestResolveForgeMavenRegistry_ForgeNeutralNames pins the fix: the GitHub
// package registry understands the forge-neutral $REPOSITORY, so a run
// configured with it no longer resolves in `release publish` and fails here
// with "$GITHUB_REPOSITORY is required".
func TestResolveForgeMavenRegistry_ForgeNeutralNames(t *testing.T) {
	t.Parallel()

	p := &github.Provider{Env: func(k string) string {
		return map[string]string{
			"REPOSITORY":        "owner/repo",
			"GITHUB_TOKEN":      "gt",
			"GITHUB_SERVER_URL": "https://github.com",
		}[k]
	}}

	reg, err := p.ResolveForgeMavenRegistry()
	if err != nil {
		t.Fatal(err)
	}

	if reg.URL != "https://maven.pkg.github.com/owner/repo" {
		t.Errorf("registry = %+v", reg)
	}
}

// TestResolveForgeRegistries_TokenIsNotCrossForge is a SECURITY boundary,
// not a style preference.
//
// The obvious "finish the migration" change here is Token:
// runcontext.Token().Resolve(env). That chain consults FORGEJO_TOKEN BEFORE
// GITHUB_TOKEN, so with both set the Maven or npm resolver would hand a Forgejo
// credential to github.com. A cross-forge token cannot authenticate anyway,
// so the only outcomes are auth failure or disclosure to a host the token
// was never issued for.
//
// This test fails the moment the token starts spanning forges.
func TestResolveForgeRegistries_TokenIsNotCrossForge(t *testing.T) {
	t.Parallel()

	resolvers := map[string]func(*github.Provider) (string, error){
		"maven": func(p *github.Provider) (string, error) {
			reg, err := p.ResolveForgeMavenRegistry()

			return reg.Token, err
		},
		"npm": func(p *github.Provider) (string, error) {
			reg, err := p.ResolveForgeNPMRegistry()

			return reg.Token, err
		},
	}

	// Without $GITHUB_TOKEN the token is empty rather than the next credential
	// along: an empty token fails authentication, a borrowed one discloses it.
	for githubToken, want := range map[string]string{"github-token": "github-token", "": ""} {
		for name, resolve := range resolvers {
			t.Run(name+" github token "+want, func(t *testing.T) {
				t.Parallel()

				env := map[string]string{
					"REPOSITORY":        "owner/repo",
					"FORGEJO_TOKEN":     "forgejo-secret-for-another-host",
					"GITEA_TOKEN":       "gitea-secret-for-another-host",
					"CI_TOKEN":          "neutral-secret-for-another-host",
					"GITHUB_SERVER_URL": "https://github.com",
				}
				if githubToken != "" {
					env["GITHUB_TOKEN"] = githubToken
				}

				got, err := resolve(&github.Provider{Env: func(k string) string { return env[k] }})
				if err != nil {
					t.Fatal(err)
				}

				if got != want {
					t.Errorf("token sent to github.com = %q, want %q;"+
						" a non-GitHub credential must never be transmitted to github.com", got, want)
				}
			})
		}
	}
}
