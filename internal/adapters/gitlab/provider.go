// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package gitlab implements the GitLab CI provider port.
//
// The Provider type implements the always-available base + the
// RepoMetadataFetcher, TokenValidator, ReleaseCreator, and ReleaseAssetUploader
// roles. Each role lives in its own file (context.go, metadata.go, token.go,
// release.go) sharing the same struct receiver. Common HTTP plumbing
// (retry transport, JSON helpers) lives in client.go.
//
// Deliberately unimplemented:
//
//   - SARIFUploader — GitLab CI consumes the JSON SAST report format
//     (gl-sast-report.json) that trivy/opengrep emit directly.
//
// CLI surfaces that require those capabilities gate on platform first.
package gitlab

import (
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// defaultAPIBase is the GitLab REST API root used as a fallback when
// no explicit override is supplied. On-prem GitLab deployments set
// CI_SERVER_URL.
const defaultAPIBase = "https://gitlab.com"

// Provider satisfies the GitLab-side provider roles.
type Provider struct {
	// Env holds the env-var snapshot used for resolution. Empty values
	// fall back to os.Getenv at the time methods are called. Tests
	// substitute a fixed map.
	Env func(string) string

	// HTTPClient overrides http.DefaultClient. Tests inject an
	// httptest-backed client; production leaves it nil.
	HTTPClient *http.Client

	// APIBaseOverride overrides the default https://gitlab.com.
	// Production reads CI_SERVER_URL via Env. Tests set this directly.
	APIBaseOverride string
}

// New returns a Provider that reads from os.Getenv.
func New() *Provider {
	return &Provider{Env: os.Getenv}
}

// Name reports the platform identifier.
func (p *Provider) Name() provider.Platform { return provider.PlatformGitLab }

// envFunc returns the env-getter, defaulting to os.Getenv.
func (p *Provider) envFunc() func(string) string {
	if p.Env != nil {
		return p.Env
	}

	return os.Getenv
}

// Compile-time conformance checks. SARIFUploader is intentionally absent — see package doc.
var (
	_ provider.Provider                = (*Provider)(nil)
	_ provider.RepoMetadataFetcher     = (*Provider)(nil)
	_ provider.TokenValidator          = (*Provider)(nil)
	_ provider.ReleaseCreator          = (*Provider)(nil)
	_ provider.ReleasePublisher        = (*Provider)(nil)
	_ provider.ReleaseAssetUploader    = (*Provider)(nil)
	_ provider.Describer               = (*Provider)(nil)
	_ provider.WebURLBuilder           = (*Provider)(nil)
	_ provider.CapabilityReporter      = (*Provider)(nil)
	_ provider.SigningIdentityResolver = (*Provider)(nil)
	_ provider.RegistryAuthResolver    = (*Provider)(nil)
	_ provider.TagDeleter              = (*Provider)(nil)
	_ provider.ContainerPackageLister  = (*Provider)(nil)
)

// apiContext resolves the API root and auth headers every GitLab call needs:
// an explicit override (tests, and the live tier's lab instance) wins, then
// $CI_SERVER_URL, then gitlab.com; the token is $GITLAB_TOKEN falling back to
// the pipeline-scoped $CI_JOB_TOKEN. Single-sourced because six call sites had
// grown their own identical copy, and a divergence here is an adapter that
// authenticates against the wrong instance.
func (p *Provider) apiContext() (string, map[string]string) {
	get := p.envFunc()

	apiBase := p.APIBaseOverride
	if apiBase == "" {
		apiBase = get("CI_SERVER_URL")
	}

	if apiBase == "" {
		apiBase = defaultAPIBase
	}

	token := get("GITLAB_TOKEN")
	if token == "" {
		token = get("CI_JOB_TOKEN")
	}

	return apiBase, map[string]string{"PRIVATE-TOKEN": token}
}

// projectEndpoint builds the /api/v4/projects/<url-encoded path> root that
// every project-scoped GitLab endpoint hangs off.
func projectEndpoint(apiBase, repo string) string {
	return strings.TrimRight(apiBase, "/") + "/api/v4/projects/" + url.PathEscape(repo)
}
