// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import "github.com/diggsweden/reusable-ci/internal/domain/provider"

// DefaultOIDCIssuer returns the canonical OIDC issuer URL for the active
// provider, read from its self-description. The returned URL is both:
//
//   - what the runner-issued token claims via its `iss` field, and
//   - what cosign should be told to expect when verifying signatures
//     (the `--certificate-oidc-issuer` flag).
//
// Empty result means "we can't infer it; the caller must supply
// --oidc-issuer explicitly" — the case for local invocations and any
// provider that doesn't publish an issuer (e.g. Forgejo until its
// adapter lands). The per-forge value lives in each adapter's
// Describe(), not in a switch here, so a new forge adds its issuer
// without touching this function.
func DefaultOIDCIssuer(d provider.Describer) string {
	if d == nil {
		return ""
	}

	return d.Describe().OIDCIssuer
}

// KeylessNeedsIssuer reports whether a sigstore (keyless) signing request
// will be left without an OIDC issuer: true when the operator supplied no
// explicit --oidc-issuer AND the active forge does not publish one
// (its KeylessOIDC capability is false, e.g. Forgejo today). The CLI uses
// this to warn up front — keyless signing on such a forge otherwise fails
// later inside cosign with an opaque "no issuer" error. Pure predicate so
// the decision is testable without wiring cosign.
func KeylessNeedsIssuer(explicitIssuer string, keylessCapable bool) bool {
	return explicitIssuer == "" && !keylessCapable
}
