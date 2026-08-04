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
	// SetupURL is a display hint. When the server is unresolved (a
	// misconfigured runner) it degrades to the bare settings path rather than
	// failing a describe — the operations that must not target the wrong host
	// (client, registry auth, keyless identity) fail closed on their own.
	server, _ := p.serverURL()

	return provider.Info{
		DisplayName: "Forgejo",
		SetupURL:    server + "/user/settings/applications",
		ScopesHint:  "A Forgejo access token with repository read/write scope is required.",
		OIDCIssuer:  "",
	}
}

// Capabilities reports the Forgejo feature set. The role-backed bools are
// derived from the roles this adapter implements — SARIFUpload comes out
// false because the SARIFUploader role is deliberately unimplemented
// (codeberg.org/forgejo/forgejo#3669), which is what makes security
// commands degrade to a step-summary / artifact sink.
//
// The two keyless capabilities split here, which is the case they exist to
// tell apart: Forgejo v15.0+ issues OIDC id-tokens (enable-openid-connect) and
// this adapter implements SigningIdentityResolver, so keyless works against a
// Fulcio told to trust the instance — MintsOIDCToken.
//
// PublicFulcioTrusted is asked of the issuer, as everywhere else, and answers
// false for every Forgejo: the issuer is <instance>/api/actions, so each
// instance publishes its own and public Fulcio has onboarded none of them. A
// run therefore passes --oidc-issuer and --fulcio-url, or signs with a key.
//
// Attestation=false: no build-provenance attestation API today.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.DeriveCapabilities(p, provider.Declared{
		MintsOIDCToken:      true,
		PublicFulcioTrusted: provider.PublicFulcioTrusts(p.Describe().OIDCIssuer),
	})
}
