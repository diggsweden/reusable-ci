// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// SupportsKeyless reports whether this Forgejo instance can drive Sigstore
// keyless signing out of the box. It delegates to Capabilities, which today
// reports false: Forgejo v15.0+ issues OIDC id-tokens, but public Fulcio does
// not trust a Forgejo issuer, so keyless needs an explicit --oidc-issuer and a
// trusting Fulcio. Implementing the role (rather than omitting it) lets a
// caller still resolve the anchored verification identity, and lets a future
// instance-trusting deployment flip the capability without new wiring.
func (p *Provider) SupportsKeyless() bool { return p.Capabilities().KeylessOIDC }

// ResolveKeylessIdentity returns the audience and anchored verification
// identity for the running Forgejo Actions job. OIDCIssuer reuses Describe(),
// which is empty by design (no auto-suppliable public-Fulcio-trusted issuer);
// a keyless caller must supply --oidc-issuer explicitly. SubjectRegexp is still
// anchored to the repository so verification, when configured, stays pinned.
func (p *Provider) ResolveKeylessIdentity() (provider.KeylessIdentity, error) {
	repo := strings.TrimSpace(runcontext.Repository().Resolve(p.envFunc()))
	if repo == "" {
		return provider.KeylessIdentity{}, fmt.Errorf(
			"$FORGEJO_REPOSITORY (or $GITHUB_REPOSITORY) is required to resolve the keyless signing identity: %w", errs.ErrUsage)
	}

	server, err := p.serverURL()
	if err != nil {
		return provider.KeylessIdentity{}, err
	}

	repoURL := server + "/" + repo

	return provider.KeylessIdentity{
		OIDCIssuer:    p.Describe().OIDCIssuer,
		TokenAudience: provider.KeylessAudience,
		SubjectID:     repoURL,
		SubjectRegexp: provider.AnchorIdentity(repoURL),
	}, nil
}
