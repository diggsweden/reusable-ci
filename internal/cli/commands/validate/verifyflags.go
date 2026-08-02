// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

// Shared flag names and usage text for the three signature-verification
// commands (artifact-signature, container-signature, container-attestation),
// which accept the same gpg/sigstore/kms verification inputs. Named once so the
// definitions stay in sync and the package stays under goconst's literal
// budget.
const (
	flagMethod             = "method"
	flagCertIdentityRegexp = "cert-identity-regexp"
	flagCertOIDCIssuer     = "cert-oidc-issuer"
	flagKey                = "key"

	usageCertIdentityRegexp = "regexp the Fulcio cert identity must match for --method=sigstore (matched against the full identity URL; example: ^https://github\\.com/<owner>/<repo>/)"
)
