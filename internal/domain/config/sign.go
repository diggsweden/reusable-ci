// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/internal/domain/release"
)

// SignConfig is the top-level `sign:` block in artifacts.yml. It
// selects which signing backend `release sign` uses. All fields are
// optional; the absence of the block (or an empty Method) defaults to
// gpg, which preserves the pre-cosign contract for existing repos.
//
// Validation invariants (see Validate):
//
//   - Method is one of [gpg, sigstore, kms] or empty (→ gpg default).
//   - method=kms requires Key; method=sigstore + method=gpg forbid it.
//   - method=sigstore optionally accepts OIDCIssuer; method=gpg + kms forbid it.
//   - When Method is kms, Key's URI scheme is restricted to a known
//     allowlist (see allowedKMSSchemes). This is a security-hardening
//     measure: the config file is committed to the repo, so a malicious
//     PR could in principle change `sign.key` to an attacker-controlled
//     URI or local path. The allowlist forces the scheme to be one of
//     the recognised KMS / HSM / Vault providers — local key-file
//     paths are accepted only when prefixed with `file:` to make the
//     intent explicit.
type SignConfig struct {
	Method     domainrelease.SignMethod `yaml:"method,omitempty"`
	Key        string                   `yaml:"key,omitempty"`
	OIDCIssuer string                   `yaml:"oidc-issuer,omitempty"`
}

// allowedKMSSchemes restricts which URI shapes can appear in sign.key.
// Adding a scheme here is a security-relevant decision — it allows a
// future PR to point sign.key at that target. The current list mirrors
// cosign's supported provider URIs; `file:` is explicit and intended
// only for local-development / integration-test usage.
//
//nolint:gochecknoglobals // schema enumeration — read-only and ordered.
var allowedKMSSchemes = []string{
	"awskms",
	"gcpkms",
	"azurekms",
	"hashivault",
	"pkcs11", // TPM / HSM via PKCS#11 URI
	"file",   // local key file (intentionally explicit prefix)
}

// EffectiveMethod returns the configured method or the package default
// when none is set. Callers that need to know whether the operator made
// an explicit choice should inspect SignConfig.Method directly.
func (s SignConfig) EffectiveMethod() domainrelease.SignMethod {
	if s.Method == "" {
		return domainrelease.DefaultSignMethod
	}

	return s.Method
}

// Validate enforces the per-method invariants documented on SignConfig.
// Returns nil when the config is well-formed (including the all-empty
// case, which defaults to gpg). Errors wrap errs.ErrInvalidConfig so the
// CLI exits with sysexits EX_CONFIG (78).
//
//nolint:cyclop // straight-line invariant check — fewer branches would obscure which field failed.
func (s SignConfig) Validate() error {
	method := s.EffectiveMethod()

	if _, err := domainrelease.ParseSignMethod(string(method)); err != nil {
		return err
	}

	switch method {
	case domainrelease.SignMethodGPG:
		if s.Key != "" {
			return fmt.Errorf("sign.key is forbidden for method=gpg (got %q): %w", s.Key, errs.ErrInvalidConfig)
		}

		if s.OIDCIssuer != "" {
			return fmt.Errorf("sign.oidc-issuer is forbidden for method=gpg (got %q): %w", s.OIDCIssuer, errs.ErrInvalidConfig)
		}
	case domainrelease.SignMethodSigstore:
		if s.Key != "" {
			return fmt.Errorf("sign.key is forbidden for method=sigstore (keyless has no key) (got %q): %w", s.Key, errs.ErrInvalidConfig)
		}

		if s.OIDCIssuer != "" {
			if err := validateOIDCIssuer(s.OIDCIssuer); err != nil {
				return err
			}
		}
	case domainrelease.SignMethodKMS:
		if s.Key == "" {
			return fmt.Errorf("sign.key is required for method=kms (e.g. hashivault://transit/keys/release): %w", errs.ErrInvalidConfig)
		}

		if s.OIDCIssuer != "" {
			return fmt.Errorf("sign.oidc-issuer is forbidden for method=kms (got %q): %w", s.OIDCIssuer, errs.ErrInvalidConfig)
		}

		if err := validateKMSKey(s.Key); err != nil {
			return err
		}
	}

	return nil
}

// validateKMSKey enforces the allowlist of sign.key URI schemes. The
// goal is twofold:
//
//  1. Reject arbitrary local paths (e.g. `/etc/...` or `../somefile`)
//     that could be exploited by a malicious PR to make the signing
//     job read or sign data from outside the workspace. Local keys
//     must be prefixed `file:` to make the intent explicit.
//  2. Limit the URI to recognised KMS / HSM provider schemes so that a
//     PR can't quietly redirect signing to an attacker-controlled host
//     via, say, a `http://attacker.example/key` URI that cosign might
//     resolve via plugins.
func validateKMSKey(key string) error {
	colon := strings.Index(key, ":")
	if colon <= 0 {
		return fmt.Errorf(
			"sign.key %q does not start with a recognised scheme; expected one of %s (e.g. hashivault://transit/keys/<name>, awskms:///alias/<name>, file:<path>): %w",
			key, strings.Join(allowedKMSSchemes, ", "), errs.ErrInvalidConfig,
		)
	}

	scheme := key[:colon]
	for _, allowed := range allowedKMSSchemes {
		if scheme == allowed {
			return nil
		}
	}

	return fmt.Errorf(
		"sign.key scheme %q is not in the allowlist %s — to extend the allowlist, edit internal/domain/config/sign.go (security-relevant change): %w",
		scheme, strings.Join(allowedKMSSchemes, ", "), errs.ErrInvalidConfig,
	)
}

// validateOIDCIssuer enforces that sign.oidc-issuer is a well-formed
// HTTPS URL. We don't allowlist hostnames — operators may run their
// own Fulcio (Sigstore self-hosted) or use a non-standard issuer for
// Forgejo. But we do reject HTTP and malformed URLs, since either
// would degrade the trust model.
func validateOIDCIssuer(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("sign.oidc-issuer %q is not a valid URL: %w: %w", raw, err, errs.ErrInvalidConfig)
	}

	if parsed.Scheme != "https" {
		return fmt.Errorf(
			"sign.oidc-issuer %q must use https:// (got scheme %q) — OIDC tokens must travel over TLS: %w",
			raw, parsed.Scheme, errs.ErrInvalidConfig,
		)
	}

	if parsed.Host == "" {
		return fmt.Errorf("sign.oidc-issuer %q has no host component: %w", raw, errs.ErrInvalidConfig)
	}

	return nil
}
