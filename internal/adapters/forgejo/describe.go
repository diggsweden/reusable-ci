// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import "github.com/diggsweden/reusable-ci/v3/internal/domain/provider"

// Describe returns Forgejo's self-description. The token-setup URL is
// derived from the resolved server so it points at the actual instance
// (e.g. https://codeberg.org/user/settings/applications).
//
// OIDCIssuer is left empty deliberately — NOT because Forgejo lacks OIDC.
// Forgejo v15.0+ (Runner >v12.5.0) does issue OIDC id-tokens: a job with
// `enable-openid-connect: true` gets ACTIONS_ID_TOKEN_REQUEST_URL /
// ACTIONS_ID_TOKEN_REQUEST_TOKEN injected
// (https://forgejo.org/docs/v15.0/user/actions/security-openid-connect/).
// But public Sigstore/Fulcio does not trust a Forgejo instance as an
// issuer, so there is no issuer we can safely auto-supply: keyless signing
// needs an explicit --oidc-issuer plus a Fulcio configured to trust it.
func (p *Provider) Describe() provider.Info {
	return provider.Info{
		DisplayName: "Forgejo",
		SetupURL:    p.serverURL() + "/user/settings/applications",
		ScopesHint:  "A Forgejo access token with repository read/write scope is required.",
		OIDCIssuer:  "",
	}
}

// Capabilities reports the Forgejo feature set. Forgejo has no Code
// Scanning SARIF ingestion (codeberg.org/forgejo/forgejo#3669) and no
// build-provenance attestation API today; release-asset upload is
// available. KeylessOIDC=false reflects out-of-the-box Sigstore signing:
// although Forgejo v15.0+ issues OIDC id-tokens (enable-openid-connect),
// public Fulcio does not trust a Forgejo issuer, so keyless needs an
// explicit --oidc-issuer + a trusting Fulcio. SARIFUpload=false is what
// makes security commands degrade to a step-summary / artifact sink.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		SARIFUpload:   false,
		Attestation:   false,
		KeylessOIDC:   false,
		ReleaseAssets: true,
	}
}
