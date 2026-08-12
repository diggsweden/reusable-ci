// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package github implements the GitHub Actions provider port.
//
// The Provider type implements every role interface in
// internal/domain/provider — base Provider, RepoMetadataFetcher,
// TokenValidator, ReleaseCreator, ReleaseAssetUploader, SARIFUploader.
// Each role lives in its own file (context.go, metadata.go, token.go,
// release.go, sarif.go) sharing the same struct receiver. Common HTTP
// plumbing (retry transport, go-github client construction, JSON
// helpers, error classification) lives in client.go. Artifact
// downloading from workflow runs lives in artifacts.go.
package github

import (
	"net/http"
	"os"
	"sync"

	gogithub "github.com/google/go-github/v76/github"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// Well-known GitHub REST API constants. Internal to this adapter —
// other packages talk to GitHub through the provider role interfaces.
const (
	// defaultAPIBase is the GitHub REST API root used as a fallback
	// when no explicit override is supplied. GitHub Enterprise
	// deployments override this to their on-prem endpoint.
	defaultAPIBase = "https://api.github.com"

	// acceptJSONHeader is the canonical Accept header for the GitHub
	// REST API.
	acceptJSONHeader = "application/vnd.github+json"

	// apiVersionHeader pins X-GitHub-Api-Version. Bump after
	// validating the new contract against this adapter's expected
	// response shapes.
	apiVersionHeader = "2022-11-28"
)

// Provider satisfies the GitHub-side provider roles.
type Provider struct {
	// Env holds the env-var snapshot used for resolution. Empty values
	// fall back to os.Getenv at the time methods are called.
	// Tests substitute a fixed map.
	Env func(string) string

	// HTTPClient overrides http.DefaultClient. Tests inject an
	// httptest-backed client; production leaves it nil.
	HTTPClient *http.Client

	// APIBaseOverride overrides the default https://api.github.com.
	// Production normally reads $GITHUB_API_URL via Env. Tests set this
	// directly.
	APIBaseOverride string

	// ghOnce memoises the go-github client returned by releaseClient.
	// Provider methods are safe for concurrent use; the first call wins.
	ghOnce   sync.Once
	ghClient *gogithub.Client
	ghErr    error
}

// New returns a Provider that reads from os.Getenv.
func New() *Provider {
	return &Provider{Env: os.Getenv}
}

// Name reports the platform identifier.
func (p *Provider) Name() provider.ForgeAPI { return provider.ForgeGitHub }

// envFunc returns the env-getter, defaulting to os.Getenv.
func (p *Provider) envFunc() func(string) string {
	if p.Env != nil {
		return p.Env
	}

	return os.Getenv
}

// Compile-time conformance checks. github.Provider implements every
// provider role — it is the only adapter that does so today.
var (
	_ provider.Provider                = (*Provider)(nil)
	_ provider.RepoMetadataFetcher     = (*Provider)(nil)
	_ provider.TokenValidator          = (*Provider)(nil)
	_ provider.ReleaseCreator          = (*Provider)(nil)
	_ provider.ReleasePublisher        = (*Provider)(nil)
	_ provider.ReleaseAssetUploader    = (*Provider)(nil)
	_ provider.SARIFUploader           = (*Provider)(nil)
	_ provider.Describer               = (*Provider)(nil)
	_ provider.WebURLBuilder           = (*Provider)(nil)
	_ provider.CapabilityReporter      = (*Provider)(nil)
	_ provider.TokenAdviser            = (*Provider)(nil)
	_ provider.RunArtifactDownloader   = (*Provider)(nil)
	_ provider.RunArtifactUploader     = (*Provider)(nil)
	_ provider.SigningIdentityResolver = (*Provider)(nil)
	_ provider.RegistryAuthResolver    = (*Provider)(nil)
)
