// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// PAR-SIGN-2: keyless signing against a Sigstore the lab owns.
//
// The lab runs its own Fulcio and trusts the issuers its contract names. A job
// asks its forge for an ID token, the product hands it to that CA, and the
// certificate that comes back names the pipeline. That covers the
// MintsOIDCToken capability: the forge issues tokens a Fulcio accepts, and the
// product can be pointed at one. It says nothing about public Sigstore's
// policies, which is the separate PublicFulcioTrusted claim.
//
// Both live forges run it. Adding a third needs an exact issuer entry in the
// contract's Fulcio mapping and a way for its runner to hand the job a token.
//
// No transparency log takes part: the suite runs with
// REUSABLE_CI_COSIGN_TRANSPARENCY=none and the lab deploys no Rekor, so a lab
// signature cannot reach the public log.

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

// claimsMintsOIDCToken selects forges whose capability matrix says they can
// mint an OIDC token for a Fulcio that trusts them. That is the claim this
// scenario tests: the lab runs its own CA, so "public Fulcio trusts the issuer"
// (PublicFulcioTrusted) is the wrong question here and would exclude Forgejo, which can
// sign perfectly well against a CA told to accept it.
func claimsMintsOIDCToken(c provider.Capabilities) bool { return c.MintsOIDCToken }

func TestInRunner_KeylessSigningAgainstTheLabCA(t *testing.T) {
	const tag = "v0.0.10-keyless"

	for _, forge := range livetest.ForgesMeeting(t, forgesClaiming(t, claimsMintsOIDCToken, "keyless signing against an own CA"), livetest.NeedsInRunner, livetest.NeedsFulcio) {

		t.Run(string(forge), func(t *testing.T) {
			target := livetest.Accept(t, forge)
			fulcioURL, oidcIssuer := livetest.FulcioConfig(t, target)
			// A fresh path per run: GitLab will not issue an ID token for a
			// repository path that has been used and deleted before.
			repo := livetest.NewScratchRepoUnique(t, target, "keyless")

			livetest.PrepareTag(t, target, repo, tag)
			livetest.PublishKeylessAssets(t, target, repo, tag, t.TempDir())

			assetURL := livetest.ReleaseAssetURL(t, target, repo, tag, "reusable-ci")
			proxyAssetURL := livetest.ReleaseAssetURL(t, target, repo, tag, "credential-proxy")

			conclusion := livetest.RunWorkflow(t, target, repo, "keyless-sign",
				livetest.KeylessSignProbe(target, assetURL, proxyAssetURL, fulcioURL, oidcIssuer))
			if conclusion != "success" {
				t.Errorf("%s: keyless signing concluded %q — this forge mints OIDC tokens, but one did not produce a signing certificate from the lab CA",
					forge, conclusion)
			}
		})
	}
}
