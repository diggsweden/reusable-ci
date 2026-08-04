// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gitlab

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// SupportsKeyless reports whether GitLab CI can supply an OIDC token for
// Sigstore keyless signing. It delegates to Capabilities so the claim has a
// single source of truth, and reads MintsOIDCToken because that is the
// question the role asks: can this forge mint a token at all.
func (p *Provider) SupportsKeyless() bool { return p.Capabilities().MintsOIDCToken }

// ResolveKeylessIdentity returns the issuer, audience, and verification
// identity for the running GitLab CI job. The issuer reuses Describe() (the
// $CI_SERVER_URL-derived issuer); the verification regexp is anchored to the
// project URL so any pipeline under it verifies and nothing broader does.
//
// SubjectID is set to the project URL (informational): GitLab's full
// certificate SAN encodes the CI config path and ref, which verification does
// not need because SubjectRegexp already pins the project.
func (p *Provider) ResolveKeylessIdentity() (provider.KeylessIdentity, error) {
	env := p.envFunc()

	// The anchor decides which certificates verification accepts, so it must
	// be the runner's own value rather than anything the orchestration layer
	// computed. $CI_PROJECT_URL is GitLab's, and has no neutral alias.
	attested, ok := runcontext.ProjectURL().ResolveAttested(env)
	if !ok {
		return provider.KeylessIdentity{}, fmt.Errorf(
			"a runner-provided project URL (%s) is required to resolve the keyless signing identity: %w",
			runcontext.ProjectURL(), errs.ErrUsage)
	}

	projectURL := strings.TrimRight(attested.String(), "/")

	return provider.KeylessIdentity{
		OIDCIssuer:    p.Describe().OIDCIssuer,
		TokenAudience: provider.KeylessAudience,
		SubjectID:     projectURL,
		SubjectRegexp: provider.AnchorIdentity(attested),
	}, nil
}
