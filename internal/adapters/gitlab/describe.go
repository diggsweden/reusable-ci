// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import "github.com/diggsweden/reusable-ci/v3/internal/domain/provider"

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

// Capabilities reports the GitLab feature set. The role-backed bools are
// derived from the roles this adapter implements — SARIFUpload is false
// because the SARIFUploader role is deliberately unimplemented (GitLab
// consumes the JSON SAST report directly), and RunArtifacts is false by
// design: GitLab passes intra-pipeline artifacts declaratively via the job
// YAML (`artifacts:` + `needs:`), not a programmatic in-job store, so the
// run-artifact roles are unimplemented and that hand-off lives in the
// GitLab template rather than this binary. Attestation=false (no
// build-provenance attestation API today).
//
// GitLab CI mints id-tokens on every instance, so MintsOIDCToken is
// unconditional. Whether public Fulcio trusts the issuer is asked of the
// issuer itself: true on gitlab.com, false behind a $CI_SERVER_URL, which is
// the difference between keyless working out of the box and needing a Fulcio
// of your own.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.DeriveCapabilities(p, provider.Declared{
		MintsOIDCToken:      true,
		PublicFulcioTrusted: provider.PublicFulcioTrusts(p.Describe().OIDCIssuer),
	})
}
