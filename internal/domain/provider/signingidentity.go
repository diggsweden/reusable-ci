// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provider

import (
	"regexp"
	"strings"
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
// computed upstream. See AttestedRepoURL.
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

// AttestedRepoURL is a repository URL fit to anchor a TRUST decision:
// resolved from values the runner injected, not from a chain that prefers
// whatever the orchestration layer computed.
//
// It is a distinct type because the two kinds of repository URL are
// interchangeable to the compiler and opposite in meaning. The descriptive
// chains (runcontext.Repository / ServerURL) sort by "most deliberate wins",
// which is right for naming what to build and wrong for deciding what to
// accept — an anchor must sort by "least forgeable wins". Requiring an
// explicit conversion here does not make the mistake impossible, but it does
// make it a deliberate, greppable act rather than a passing resemblance
// between two strings.
//
// Obtain one from runcontext's Attested* chains. See
// internal/archguard/anchor_guard_test.go, which fails a ResolveKeylessIdentity
// that reaches for a descriptive chain.
type AttestedRepoURL string

// AnchorIdentity builds the anchored certificate-identity regexp for a
// repository URL. The result matches every Sigstore SAN that begins with the
// repository URL followed by a path separator — i.e. any workflow/ref under
// that exact repository — and nothing else.
//
// This regexp is what `cosign verify-*` receives as
// --certificate-identity-regexp, so it decides which certificates are
// ACCEPTED. Whatever widens repoURL widens what verification will trust,
// which is why the parameter is typed.
//
// Security: the repository URL is regexp-escaped (QuoteMeta) before anchoring,
// so a '.' in "github.com" cannot act as a wildcard, and the leading '^' plus
// trailing '/' stop a substring or sibling-repository match (e.g. anchoring
// "…/acme/app" must not also accept "…/acme/app-evil"). An empty or
// whitespace-only repoURL yields a regexp that matches no real SAN ("^$"),
// failing closed rather than open.
func AnchorIdentity(repoURL AttestedRepoURL) string {
	trimmed := strings.TrimRight(strings.TrimSpace(string(repoURL)), "/")
	if trimmed == "" {
		return "^$"
	}

	return "^" + regexp.QuoteMeta(trimmed) + "/"
}
