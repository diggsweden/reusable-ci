// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import "github.com/diggsweden/reusable-ci/v3/internal/cli/deps"

// keylessVerifyIdentity fills empty cert-identity-regexp / cert-oidc-issuer
// from the detected forge's keyless signing identity, so verifying a sigstore
// signature "as this repo on this forge" needs no hand-written regexp. The
// derived identity is anchored to the repository and read from the trusted
// runner environment, so it is both safer and less error-prone than a
// hand-typed pattern.
//
// "Trusted runner environment" is load-bearing, not decoration: this fills a
// value that decides which certificates are ACCEPTED, so the resolvers behind
// it read only what the runner injected. They deliberately do NOT use
// runcontext.Repository()/ServerURL(), which prefer the bare $REPOSITORY /
// $CI_SERVER_URL the orchestration layer computes — right for describing a
// run, wrong for anchoring trust. That distinction was once lost to a
// consistency pass and is now held by
// archguard.TestKeylessIdentityAnchorsOnAttestedValues.
//
// Explicit flag values always win (cross-repo verification stays possible). It
// is a no-op when the forge exposes no resolver or does not support keyless
// (Forgejo out of the box, local), preserving the prior behaviour where the
// operator supplied the constraints. Callers must skip it for KMS
// verification (a non-empty --key), where an identity regexp would otherwise
// flip auto-detection from kms to sigstore.
func keylessVerifyIdentity(identityRegexp, oidcIssuer string) (string, string) {
	if identityRegexp != "" && oidcIssuer != "" {
		return identityRegexp, oidcIssuer
	}

	resolver, ok := deps.SigningIdentityForDetected()
	if !ok || !resolver.SupportsKeyless() {
		return identityRegexp, oidcIssuer
	}

	id, err := resolver.ResolveKeylessIdentity()
	if err != nil {
		return identityRegexp, oidcIssuer
	}

	if identityRegexp == "" {
		identityRegexp = id.SubjectRegexp
	}

	if oidcIssuer == "" {
		oidcIssuer = id.OIDCIssuer
	}

	return identityRegexp, oidcIssuer
}
