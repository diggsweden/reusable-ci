// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import "github.com/diggsweden/reusable-ci/internal/domain/provider"

// Describe returns Forgejo's self-description. The token-setup URL is
// derived from the resolved server so it points at the actual instance
// (e.g. https://codeberg.org/user/settings/applications). No OIDC issuer
// is published yet — Forgejo keyless signing lands in a later phase, so
// operators set --oidc-issuer explicitly until then.
func (p *Provider) Describe() provider.Info {
	return provider.Info{
		DisplayName: "Forgejo",
		SetupURL:    p.serverURL() + "/user/settings/applications",
		ScopesHint:  "A Forgejo access token with repository read/write scope is required.",
		OIDCIssuer:  "",
	}
}

// ProvenanceProfile returns the Forgejo SLSA-provenance vocabulary
// (workflows live under .forgejo/workflows/; the runner is
// forgejo-actions). Matches forgejo-ci's slsa-provenance.sh.
func (p *Provider) ProvenanceProfile() provider.ProvenanceProfile {
	return provider.ProvenanceProfile{
		BuildType:         "https://forgejo.org/actions/buildtypes/workflow/v1",
		WorkflowDirPrefix: ".forgejo/workflows/",
		RunnerLabel:       "forgejo-actions",
	}
}

// Capabilities reports the Forgejo feature set. Forgejo has no Code
// Scanning SARIF ingestion and no build-provenance attestation API
// today, and keyless OIDC signing is not yet wired; release-asset upload
// is available. SARIFUpload=false is what makes security commands
// degrade to a step-summary / artifact sink on Forgejo.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		SARIFUpload:   false,
		Attestation:   false,
		KeylessOIDC:   false,
		ReleaseAssets: true,
	}
}
