// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"os"

	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// DefaultOIDCIssuer returns the canonical OIDC issuer URL for the
// platform the binary is running under. The returned URL is both:
//
//   - what the runner-issued token claims via its `iss` field, and
//   - what cosign should be told to expect when verifying signatures
//     (the `--certificate-oidc-issuer` flag).
//
// Empty result means "we can't infer it; the caller must supply
// --oidc-issuer explicitly". This is the case for local invocations
// and any future platform not yet recognised here.
//
// GitLab self-hosted: $CI_SERVER_URL is the issuer (the project
// configures Fulcio / its own Sigstore deployment to trust it).
// gitlab.com is the SaaS default.
//
// Forgejo: not yet integrated. Add the corresponding case once
// internal/domain/provider has a PlatformForgejo constant. Until
// then, Forgejo operators set --oidc-issuer explicitly.
func DefaultOIDCIssuer(plat provider.Platform) string {
	switch plat {
	case provider.PlatformGitHub:
		return "https://token.actions.githubusercontent.com"
	case provider.PlatformGitLab:
		if v := os.Getenv("CI_SERVER_URL"); v != "" {
			return v
		}

		return "https://gitlab.com"
	default:
		return ""
	}
}
