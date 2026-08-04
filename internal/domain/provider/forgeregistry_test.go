// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provider_test

import (
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

func TestForgeMavenRegistry_AltDeploymentRepository(t *testing.T) {
	t.Parallel()

	reg := provider.ForgeMavenRegistry{ServerID: "gitlab-maven", URL: "https://gl/api/v4/projects/1/packages/maven"}
	if got, want := reg.AltDeploymentRepository(), "gitlab-maven::default::https://gl/api/v4/projects/1/packages/maven"; got != want {
		t.Errorf("AltDeploymentRepository() = %q, want %q", got, want)
	}
}

func TestForgeMavenRegistry_RenderSettingsXML_PerScheme(t *testing.T) {
	t.Parallel()

	// Header-based (GitLab): httpHeaders, no <password>.
	gl := provider.ForgeMavenRegistry{ServerID: "gitlab-maven", AuthScheme: provider.MavenAuthJobTokenHeader, Token: "secret-jt"}
	xml := gl.RenderSettingsXML()

	for _, want := range []string{"<id>gitlab-maven</id>", "Job-Token", "secret-jt", "httpHeaders"} {
		if !strings.Contains(xml, want) {
			t.Errorf("gitlab settings.xml missing %q:\n%s", want, xml)
		}
	}

	if strings.Contains(xml, "<password>") {
		t.Errorf("header auth must not render <password>:\n%s", xml)
	}

	// Token header (Forgejo): Authorization: token <tok>.
	fj := provider.ForgeMavenRegistry{ServerID: "forgejo", AuthScheme: provider.MavenAuthTokenHeader, Token: "ft"}
	if x := fj.RenderSettingsXML(); !strings.Contains(x, "Authorization") || !strings.Contains(x, "token ft") {
		t.Errorf("forgejo settings.xml should carry Authorization: token:\n%s", x)
	}

	// Server password (GitHub): username/password.
	gh := provider.ForgeMavenRegistry{ServerID: "github", AuthScheme: provider.MavenAuthServerPassword, Username: "ci", Token: "ght"}
	if x := gh.RenderSettingsXML(); !strings.Contains(x, "<username>ci</username>") || !strings.Contains(x, "<password>ght</password>") {
		t.Errorf("github settings.xml should carry username/password:\n%s", x)
	}
}

// A token with XML metacharacters must be escaped, not break the document.
func TestForgeMavenRegistry_RenderSettingsXML_EscapesToken(t *testing.T) {
	t.Parallel()

	reg := provider.ForgeMavenRegistry{ServerID: "gitlab-maven", AuthScheme: provider.MavenAuthJobTokenHeader, Token: "a<b>&c"}
	if x := reg.RenderSettingsXML(); strings.Contains(x, "a<b>&c") {
		t.Errorf("token must be XML-escaped:\n%s", x)
	}
}

// A token with a CR/LF must not inject a second .npmrc line.
func TestForgeNPMRegistry_RenderNPMRC_StripsNewlinesFromToken(t *testing.T) {
	t.Parallel()

	reg := provider.ForgeNPMRegistry{Registry: "https://r/", Token: "good\n//evil/:_authToken=x"} //nolint:gosec // G101 false positive: synthetic newline-injection test fixture, not a real credential.

	rc := reg.RenderNPMRC()

	// Assert the injection is absent rather than counting lines. A total-line
	// count is a proxy for this and a brittle one: it failed when an unrelated
	// (and deprecated) line was removed from the template, which says nothing
	// about whether a token can smuggle in a second registry.
	for _, line := range strings.Split(strings.TrimRight(rc, "\n"), "\n") {
		if strings.HasPrefix(line, "//evil/") {
			t.Errorf("a newline in the token injected a second registry line:\n%s", rc)
		}
	}

	if !strings.Contains(rc, "//r/:_authToken=good//evil/:_authToken=x") {
		t.Errorf("the token should survive flattened onto one line:\n%s", rc)
	}
}

func TestForgeNPMRegistry_RenderNPMRC(t *testing.T) {
	t.Parallel()

	// GitLab: path-scoped _authToken, no scope, registry= line.
	gl := provider.ForgeNPMRegistry{Registry: "https://gl/api/v4/projects/1/packages/npm/", Token: "jt"}
	rc := gl.RenderNPMRC()

	for _, want := range []string{"//gl/api/v4/projects/1/packages/npm/:_authToken=jt", "registry=https://gl/api/v4/projects/1/packages/npm/"} {
		if !strings.Contains(rc, want) {
			t.Errorf("gitlab .npmrc missing %q:\n%s", want, rc)
		}
	}

	// GitHub: scoped @owner:registry, host-level authToken.
	gh := provider.ForgeNPMRegistry{Registry: "https://npm.pkg.github.com", Scope: "@org", Token: "ght"}
	rc = gh.RenderNPMRC()

	for _, want := range []string{"//npm.pkg.github.com/:_authToken=ght", "@org:registry=https://npm.pkg.github.com"} {
		if !strings.Contains(rc, want) {
			t.Errorf("github .npmrc missing %q:\n%s", want, rc)
		}
	}
}
