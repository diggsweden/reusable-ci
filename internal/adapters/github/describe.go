// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/domain/validate"
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

// Capabilities reports the GitHub feature set: SARIF ingestion (Code
// Scanning), SLSA build-provenance attestation, keyless OIDC signing,
// and release-asset upload are all available.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		SARIFUpload:   true,
		Attestation:   true,
		KeylessOIDC:   true,
		ReleaseAssets: true,
	}
}

// ProvenanceProfile returns the GitHub SLSA-provenance vocabulary
// (workflows live under .github/workflows/; the runner is
// github-actions). Note: GitHub releases normally get provenance from
// the attest-build-provenance action; this profile is for the CLI's own
// generator when used directly.
func (p *Provider) ProvenanceProfile() provider.ProvenanceProfile {
	return provider.ProvenanceProfile{
		BuildType:         "https://github.com/actions/buildtypes/workflow/v1",
		WorkflowDirPrefix: ".github/workflows/",
		RunnerLabel:       "github-actions",
	}
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
