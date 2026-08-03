// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package github

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// SupportsKeyless reports whether GitHub Actions can supply an OIDC token for
// Sigstore keyless signing. It delegates to Capabilities so KeylessOIDC has a
// single source of truth.
func (p *Provider) SupportsKeyless() bool { return p.Capabilities().KeylessOIDC }

// ResolveKeylessIdentity returns the issuer, audience, and verification
// identity for the running GitHub Actions job. The issuer reuses Describe()
// (the fixed Actions token endpoint); the verification regexp is anchored to
// the repository so any workflow under it verifies and nothing broader does.
//
// SubjectID is the exact signing-certificate SAN — $GITHUB_SERVER_URL joined
// with $GITHUB_WORKFLOW_REF (owner/repo/.github/workflows/file@ref) — usable
// for exact-identity verification; it is empty when the runner did not inject
// the ref (e.g. outside a workflow).
// $GITHUB_REPOSITORY and $GITHUB_SERVER_URL are read by NAME here on
// purpose, not through runcontext.Repository()/ServerURL(). SubjectRegexp
// becomes cosign's --certificate-identity-regexp, so it decides which
// certificates verification ACCEPTS: it must come from what the runner
// injected, never from the bare $REPOSITORY the orchestration layer computes.
// Nor from a chain spanning forges -- $FORGEJO_REPOSITORY on a GitHub runner
// names a target, not this repository.
func (p *Provider) ResolveKeylessIdentity() (provider.KeylessIdentity, error) {
	env := p.envFunc()

	repo := strings.TrimSpace(env("GITHUB_REPOSITORY"))
	if repo == "" {
		return provider.KeylessIdentity{}, fmt.Errorf(
			"$GITHUB_REPOSITORY is required to resolve the keyless signing identity: %w", errs.ErrUsage)
	}

	server := strings.TrimRight(strings.TrimSpace(env("GITHUB_SERVER_URL")), "/")
	if server == "" {
		server = "https://github.com"
	}

	subjectID := ""
	if ref := strings.TrimSpace(env("GITHUB_WORKFLOW_REF")); ref != "" {
		subjectID = server + "/" + ref
	}

	return provider.KeylessIdentity{
		OIDCIssuer:    p.Describe().OIDCIssuer,
		TokenAudience: provider.KeylessAudience,
		SubjectID:     subjectID,
		SubjectRegexp: provider.AnchorIdentity(provider.AttestedRepoURL(server + "/" + repo)),
	}, nil
}
