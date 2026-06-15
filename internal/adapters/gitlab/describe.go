// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import "github.com/diggsweden/reusable-ci/internal/domain/provider"

// Describe returns GitLab's self-description. The OIDC issuer is the
// GitLab server URL ($CI_SERVER_URL on self-hosted; gitlab.com as the
// SaaS default) — the project configures Fulcio / its Sigstore
// deployment to trust it.
func (p *Provider) Describe() provider.Info {
	issuer := p.envFunc()("CI_SERVER_URL")
	if issuer == "" {
		issuer = defaultAPIBase
	}

	return provider.Info{
		DisplayName: "GitLab",
		SetupURL:    "https://gitlab.com/-/user_settings/personal_access_tokens",
		ScopesHint:  "A project / group / personal access token with api + write_repository scopes is required.",
		OIDCIssuer:  issuer,
	}
}

// Capabilities reports the GitLab feature set. GitLab has no Code
// Scanning SARIF ingestion (it consumes the JSON SAST report directly)
// and no build-provenance attestation API today; keyless OIDC and
// release-asset linking are available.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		SARIFUpload:   false,
		Attestation:   false,
		KeylessOIDC:   true,
		ReleaseAssets: true,
	}
}
