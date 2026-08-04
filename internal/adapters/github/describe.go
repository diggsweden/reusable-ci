// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/validate"
)

// Describe returns GitHub's self-description. The OIDC issuer is the
// fixed GitHub Actions token endpoint (what runner-issued tokens claim
// via `iss` and what cosign verifies against).
func (p *Provider) Describe() provider.Info {
	return provider.Info{
		DisplayName: "GitHub",
		SetupURL:    "https://github.com/settings/personal-access-tokens/new",
		ScopesHint:  "A fine-grained PAT (github_pat_*) with 'contents: write' permission is required.",
		OIDCIssuer:  "https://token.actions.githubusercontent.com",
	}
}

// Capabilities reports the GitHub feature set. The role-backed bools
// (SARIF ingestion, release assets, run-artifact store) are derived from
// the roles this adapter implements; keyless OIDC and the SLSA
// build-provenance attestation API are both available on GitHub.
//
// PublicFulcioTrusted is asked of the issuer rather than declared, the same way
// every adapter asks it. Describe() returns the fixed github.com Actions
// issuer, which public Fulcio trusts, so it answers true today — and a GitHub
// Enterprise Server instance, which publishes its own issuer, would answer
// false the moment Describe() learns to report one.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.DeriveCapabilities(p, provider.Declared{
		MintsOIDCToken:      true,
		PublicFulcioTrusted: provider.PublicFulcioTrusts(p.Describe().OIDCIssuer),
		Attestation:         true,
	})
}

// AdviseToken classifies a release-bot token by prefix and advises on
// it: classic PATs (ghp_*) are refused as overly broad; unknown shapes
// get an informational note; fine-grained PATs and App tokens pass
// silently. The forge owns this knowledge so app code does not branch
// on platform identity.
func (p *Provider) AdviseToken(token string) (string, bool) {
	switch validate.ClassifyGitHubToken(token) {
	case validate.GitHubTokenClassic:
		return "classic PAT detected (ghp_*)\n" +
			"Classic PATs have broad access and are not recommended.\n" +
			"Please use a fine-grained PAT (github_pat_*) with 'contents: write' permission.\n" +
			"See: https://github.com/settings/personal-access-tokens/new", true
	case validate.GitHubTokenUnknown:
		return "ℹ️  Unknown token type. Expected fine-grained PAT (github_pat_*) or GitHub App token (ghs_*).", false
	case validate.GitHubTokenFineGrained, validate.GitHubTokenApp:
		return "", false
	default:
		return "", false
	}
}
