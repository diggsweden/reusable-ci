// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package forgejo

import (
	"fmt"

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
//
// # Why this reads Attested* rather than the chains the rest of the adapter uses
//
// SubjectRegexp becomes cosign's --certificate-identity-regexp: it decides
// which certificates verification ACCEPTS. So it must resolve from what the
// runner injected, not from Repository()/serverURL(), which prefer the bare
// $REPOSITORY and $CI_SERVER_URL the orchestration layer computes. Those are
// the right answer for describing the run and the wrong one for anchoring
// trust — "most deliberate wins" and "least forgeable wins" sort in opposite
// directions, so the anchor needs its own source.
//
// The API calls elsewhere in this adapter keep the descriptive chains on
// purpose: pointing a request at a computed server is a feature; accepting a
// signature because of one is not.
func (p *Provider) ResolveKeylessIdentity() (provider.KeylessIdentity, error) {
	env := p.envFunc()

	// A Forgejo job's identity only exists on a Forgejo runner. Stating that
	// precondition here, rather than encoding it in a name list, is what keeps
	// the chains single: off a Forgejo runner the attested names would resolve
	// to the HOST runner's repository ($GITHUB_REPOSITORY on GitHub), which is
	// a real repository and the wrong one to anchor a Forgejo verification to.
	if !runcontext.ForgejoRunner(env) {
		return provider.KeylessIdentity{}, fmt.Errorf(
			"not executing on a Forgejo runner, so there is no Forgejo job identity to"+
				" anchor verification on: %w", errs.ErrUsage)
	}

	repo, ok := runcontext.Repository().ResolveAttested(env)
	if !ok {
		return provider.KeylessIdentity{}, fmt.Errorf(
			"a runner-provided repository (%s) is required to resolve the keyless signing identity: %w",
			runcontext.Repository(), errs.ErrUsage)
	}

	server, ok := runcontext.ServerURL().ResolveAttested(env)
	if !ok {
		return provider.KeylessIdentity{}, fmt.Errorf(
			"a runner-provided server URL (%s) is required to resolve the keyless signing identity: %w",
			runcontext.ServerURL(), errs.ErrUsage)
	}

	repoURL := runcontext.JoinAttested("/", server, repo)

	return provider.KeylessIdentity{
		OIDCIssuer:    p.Describe().OIDCIssuer,
		TokenAudience: provider.KeylessAudience,
		SubjectID:     repoURL.String(),
		SubjectRegexp: provider.AnchorIdentity(repoURL),
	}, nil
}
