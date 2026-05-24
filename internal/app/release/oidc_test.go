// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release_test

import (
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

func TestDefaultOIDCIssuer_GitHub(t *testing.T) {
	got := apprelease.DefaultOIDCIssuer(provider.PlatformGitHub)
	want := "https://token.actions.githubusercontent.com"

	if got != want {
		t.Errorf("github OIDC issuer = %q, want %q", got, want)
	}
}

func TestDefaultOIDCIssuer_GitLabSaaS(t *testing.T) {
	t.Setenv("CI_SERVER_URL", "")

	got := apprelease.DefaultOIDCIssuer(provider.PlatformGitLab)
	if got != "https://gitlab.com" {
		t.Errorf("gitlab SaaS OIDC issuer = %q, want https://gitlab.com", got)
	}
}

func TestDefaultOIDCIssuer_GitLabSelfHosted(t *testing.T) {
	t.Setenv("CI_SERVER_URL", "https://gitlab.diggsweden.internal")

	got := apprelease.DefaultOIDCIssuer(provider.PlatformGitLab)
	if got != "https://gitlab.diggsweden.internal" {
		t.Errorf("self-hosted GitLab OIDC issuer = %q, want CI_SERVER_URL value", got)
	}
}

func TestDefaultOIDCIssuer_LocalIsEmpty(t *testing.T) {
	// Local invocations cannot infer an OIDC issuer; the caller
	// must supply --oidc-issuer explicitly. Empty result is the
	// signal for that.
	got := apprelease.DefaultOIDCIssuer(provider.PlatformLocal)
	if got != "" {
		t.Errorf("local OIDC issuer = %q, want empty", got)
	}
}
