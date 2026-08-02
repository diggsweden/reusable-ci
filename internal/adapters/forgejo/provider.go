// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package forgejo implements the Forgejo Actions provider port.
//
// Forgejo Actions sets GITHUB_ACTIONS=true but is its own runner dialect
// (see provider.RunnerForgejo) — not a GitHub clone. This adapter is the
// *forge API* half: it speaks the Gitea /api/v1 REST surface, which
// Forgejo keeps API-compatible, through the official Gitea Go SDK
// (code.gitea.io/sdk/gitea) — the same "use the forge's SDK" shape the
// github adapter uses with go-github, rather than the hand-rolled REST in
// the gitlab adapter.
//
// The Provider implements the always-available base plus the
// RepoMetadataFetcher, TokenValidator, ReleaseCreator, ReleasePublisher, and
// ReleaseAssetUploader roles. Each role lives in its own file
// (context.go, metadata.go, token.go, release.go, describe.go) sharing
// the same struct receiver.
//
// Deliberately unimplemented: SARIFUploader — Forgejo has no Code
// Scanning ingestion endpoint (Capabilities reports SARIFUpload=false so
// commands degrade to a step-summary / artifact sink instead).
package forgejo

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"code.gitea.io/sdk/gitea"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/httpretry"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// defaultServer is the fallback Forgejo host when neither
// FORGEJO_SERVER_URL nor GITHUB_SERVER_URL is set (e.g. a misconfigured
// runner). Codeberg is the most common public Forgejo instance; this
// mirrors the gitlab adapter defaulting to gitlab.com.
const defaultServer = "https://codeberg.org"

// assumedServerVersion is fed to the SDK so NewClient does NOT probe the
// server's /api/v1/version on every construction (we build one client
// per call). The endpoints this adapter uses — repos, releases,
// attachments, user, branches — are long-stable, so the assumed value
// only suppresses the probe; it never gates a feature we rely on.
const assumedServerVersion = "1.22.0"

// Provider satisfies the Forgejo-side provider roles.
type Provider struct {
	// Env holds the env-var snapshot used for resolution. Empty values
	// fall back to os.Getenv at the time methods are called. Tests
	// substitute a fixed map.
	Env func(string) string

	// HTTPClient overrides the SDK's default client. Tests inject an
	// httptest-backed client; production leaves it nil.
	HTTPClient *http.Client

	// APIBaseOverride overrides the resolved server base URL. The Gitea
	// SDK appends /api/v1 itself. Production reads $FORGEJO_SERVER_URL /
	// $GITHUB_SERVER_URL via Env; tests set this to an httptest URL.
	APIBaseOverride string
}

// New returns a Provider that reads from os.Getenv.
func New() *Provider {
	return &Provider{Env: os.Getenv}
}

// defaultHTTPClient returns a client backed by the retry transport. The
// timeout caps total per-request wall time including retries (default
// 30s, overridable via REUSABLE_CI_HTTP_TIMEOUT for slow links); the
// transport's MaxCumulativeDelay caps in-process sleep separately.
// Identical to the github/gitlab adapters so all three providers bound
// their requests the same way.
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   httpretry.ClientTimeout(),
		Transport: httpretry.NewTransport(httpretry.Config{}),
	}
}

// Name reports the platform identifier.
func (p *Provider) Name() provider.Platform { return provider.PlatformForgejo }

// envFunc returns the env-getter, defaulting to os.Getenv.
func (p *Provider) envFunc() func(string) string {
	if p.Env != nil {
		return p.Env
	}

	return os.Getenv
}

// serverURL resolves the Forgejo server base URL (no trailing slash).
// Precedence: explicit override → $FORGEJO_SERVER_URL → $GITHUB_SERVER_URL
// (the runner sets both) → defaultServer.
func (p *Provider) serverURL() string {
	if p.APIBaseOverride != "" {
		return strings.TrimRight(p.APIBaseOverride, "/")
	}

	if v := firstNonEmpty(p.envFunc(), "FORGEJO_SERVER_URL", "GITHUB_SERVER_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}

	return defaultServer
}

// token resolves the API token. Precedence: $FORGEJO_TOKEN →
// $GITEA_TOKEN → $GITHUB_TOKEN (the runner-injected token).
func (p *Provider) token() string {
	return firstNonEmpty(p.envFunc(), "FORGEJO_TOKEN", "GITEA_TOKEN", "GITHUB_TOKEN")
}

// httpClient returns the Provider's configured client (tests inject an
// httptest-backed one) or the default retry-wrapped client. Production
// leaves Provider.HTTPClient nil, so this is the path that bounds real
// Forgejo calls.
func (p *Provider) httpClient() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}

	return defaultHTTPClient()
}

// client builds a Gitea SDK client authenticated with the configured
// token and bound to ctx.
func (p *Provider) client(ctx context.Context) (*gitea.Client, error) {
	return p.clientWithToken(ctx, p.token())
}

// clientWithToken builds a Gitea SDK client using an explicit token.
// SetGiteaVersion suppresses the server-version probe NewClient would
// otherwise make, so construction is offline-safe and tests need no
// /api/v1/version stub.
func (p *Provider) clientWithToken(ctx context.Context, token string) (*gitea.Client, error) {
	opts := []gitea.ClientOption{
		gitea.SetToken(token),
		gitea.SetGiteaVersion(assumedServerVersion),
		// Always pin our own client: the SDK's fallback is an unbounded
		// &http.Client{} (no Timeout), so a slow/hung Forgejo server could
		// stall a release step forever. httpClient() supplies the same
		// timeout + retry transport the github/gitlab adapters use.
		gitea.SetHTTPClient(p.httpClient()),
	}

	client, err := gitea.NewClient(p.serverURL(), opts...)
	if err != nil {
		return nil, fmt.Errorf("forgejo client: %w", err)
	}

	client.SetContext(ctx)

	return client, nil
}

// repoFromEnv resolves owner/repo from the runner context for roles
// whose interface carries no repo argument (UploadReleaseAsset).
func (p *Provider) repoFromEnv() (string, string, error) {
	return splitRepo(firstNonEmpty(p.envFunc(), "FORGEJO_REPOSITORY", "GITHUB_REPOSITORY"))
}

// firstNonEmpty returns the first non-empty env value among keys.
func firstNonEmpty(get func(string) string, keys ...string) string {
	for _, k := range keys {
		if v := get(k); v != "" {
			return v
		}
	}

	return ""
}

// splitRepo splits "owner/repo" into its parts; the Gitea SDK takes them
// separately.
func splitRepo(repo string) (string, string, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return "", "", fmt.Errorf("invalid repo %q (want owner/repo): %w", repo, errs.ErrUsage)
	}

	return owner, name, nil
}

// classifyErr maps a Gitea SDK error + response into the project's error
// ladder so callers can errors.Is on the class. A nil err returns nil.
func classifyErr(resp *gitea.Response, err error) error {
	if err == nil {
		return nil
	}

	cls := errs.ErrDependencyUnavailable

	if resp != nil && resp.Response != nil {
		if mapped := errs.FromHTTPStatus(resp.StatusCode); mapped != nil {
			cls = mapped
		}
	}

	return fmt.Errorf("%w: %w", err, cls)
}

// Compile-time conformance checks. SARIFUploader is intentionally absent
// — see package doc.
var (
	_ provider.Provider                = (*Provider)(nil)
	_ provider.RepoMetadataFetcher     = (*Provider)(nil)
	_ provider.TokenValidator          = (*Provider)(nil)
	_ provider.ReleaseCreator          = (*Provider)(nil)
	_ provider.ReleasePublisher        = (*Provider)(nil)
	_ provider.ReleaseAssetUploader    = (*Provider)(nil)
	_ provider.Describer               = (*Provider)(nil)
	_ provider.CapabilityReporter      = (*Provider)(nil)
	_ provider.TagDeleter              = (*Provider)(nil)
	_ provider.RunArtifactDownloader   = (*Provider)(nil)
	_ provider.RunArtifactUploader     = (*Provider)(nil)
	_ provider.SigningIdentityResolver = (*Provider)(nil)
	_ provider.RegistryAuthResolver    = (*Provider)(nil)
)
