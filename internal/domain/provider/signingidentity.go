// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provider

import (
	"regexp"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// KeylessIdentity is the resolved Sigstore identity for the job currently
// running on the detected forge. It spares a caller hand-writing a
// --certificate-identity-regexp that can silently be too loose.
//
// It is read by VERIFICATION only: `cosign verify-*` via validate's
// keylessVerifyIdentity, which fills an empty --cert-identity-regexp /
// --cert-oidc-issuer. Signing does not read it — the signer derives its
// issuer from Describe() (see apprelease.DefaultOIDCIssuer). Earlier prose
// here claimed both sides read it; they do not, and the difference matters:
// every field below feeds a decision about which certificates to ACCEPT, so
// each must resolve from what the runner injected rather than from anything
// computed upstream. See AnchorIdentity and runcontext.Attested.
type KeylessIdentity struct {
	// OIDCIssuer is the issuer URL the Fulcio certificate must claim —
	// e.g. https://token.actions.githubusercontent.com on GitHub, or the
	// $CI_SERVER_URL-derived issuer on GitLab. Empty is never valid.
	OIDCIssuer string

	// TokenAudience is the audience requested when minting the runner's
	// OIDC id-token. Defaults to KeylessAudience.
	TokenAudience string

	// SubjectID is the exact certificate identity (SAN) of the signing
	// workflow — the workflow-ref URL. Informational / for exact-match
	// verification; SubjectRegexp is what verification uses by default.
	SubjectID string

	// SubjectRegexp is the anchored regexp a verifier matches the
	// certificate identity against. It is produced by AnchorIdentity from
	// the repository URL, so it pins verification to "any workflow under
	// this repository on this forge" and nothing broader.
	SubjectRegexp string
}

// KeylessAudience is the default OIDC token audience for Sigstore keyless
// signing. cosign requests "sigstore" unless told otherwise.
const KeylessAudience = "sigstore"

// SigningIdentityResolver is the provider role for forges that can mint an
// OIDC token usable for Sigstore keyless signing (GitHub Actions, GitLab CI,
// Forgejo with OIDC). Forges without OIDC (local) do not implement it, so
// deps.RequireSigningIdentityResolver returns the unsupported-role error and
// the caller falls back to gpg/kms.
//
// Resolution is pure environment reading — no token is minted here (cosign
// does that from the runner's id-token), so the role needs no context, matching
// the other forge-registry resolver roles.
type SigningIdentityResolver interface {
	// SupportsKeyless reports whether this forge/runner can supply an OIDC
	// token for keyless signing right now. A resolver may be wired but
	// report false (e.g. a Forgejo instance with OIDC disabled), in which
	// case the caller falls back rather than erroring.
	SupportsKeyless() bool

	// ResolveKeylessIdentity returns the issuer, audience, and verification
	// identity for the current job. It errors when SupportsKeyless is true
	// but the required environment is missing or malformed (an in-job
	// misconfiguration).
	ResolveKeylessIdentity() (KeylessIdentity, error)
}

// AnchorIdentity builds the anchored certificate-identity regexp for a
// repository URL. The result matches every Sigstore SAN that begins with the
// repository URL followed by a path separator — i.e. any workflow/ref under
// that exact repository — and nothing else.
//
// This regexp is what `cosign verify-*` receives as
// --certificate-identity-regexp, so it decides which certificates are
// ACCEPTED. Whatever widens repoURL widens what verification will trust,
// which is why the parameter is a runcontext.Attested rather than a string.
//
// Only runcontext.Var.ResolveAttested and JoinAttested mint one, and they
// walk ONLY the names the runner injected. So a value resolved through the
// descriptive chains — which prefer the $REPOSITORY the orchestration layer
// computed — cannot reach this sink at all. That is a compiler property, not
// a convention: the earlier named-string type still allowed an explicit
// conversion, so an archguard test had to police the gap. It was retired when
// this parameter changed type.
//
// Security: the repository URL is regexp-escaped (QuoteMeta) before anchoring,
// so a '.' in "github.com" cannot act as a wildcard, and the leading '^' plus
// trailing '/' stop a substring or sibling-repository match (e.g. anchoring
// "…/acme/app" must not also accept "…/acme/app-evil"). An empty or
// whitespace-only repoURL yields a regexp that matches no real SAN ("^$"),
// failing closed rather than open.
func AnchorIdentity(repoURL runcontext.Attested) string {
	trimmed := strings.TrimRight(strings.TrimSpace(repoURL.String()), "/")
	if trimmed == "" {
		return "^$"
	}

	return "^" + regexp.QuoteMeta(trimmed) + "/"
}
