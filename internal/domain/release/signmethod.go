// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"fmt"
	"slices"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// SignMethod selects how `release sign` produces the per-artifact
// signature. Three methods cover the matrix of operator constraints:
//
//   - SignMethodGPG: long-lived OpenPGP key passed via $GPG_PRIVATE_KEY.
//     The historical default. Subject to the swap-refusal policy
//     because the decrypted key lives in the Go heap during signing.
//     Verification is `gpg --verify <artifact>.asc <artifact>`.
//
//   - SignMethodSigstore: keyless OIDC-bound signing via cosign +
//     Fulcio + Rekor. No private key exists for more than ~10 minutes;
//     the signing identity is the CI workflow that ran. Verification
//     is `cosign verify-blob` against the workflow's identity claim.
//     Requires the runner to emit OIDC tokens (GHA, GitLab, Forgejo).
//
//   - SignMethodKMS: cosign with an explicit key reference. Supports
//     KMS provider URIs (awskms://, gcpkms://, hashivault://, etc.),
//     PKCS#11 (TPM/HSM), and local key files. The private key never
//     leaves the KMS / device. Verification is `cosign verify-blob
//     --key <pubkey>`.
//
// The GPG path is unchanged from the original implementation. The
// cosign paths share an adapter (internal/adapters/cosign).
type SignMethod string

const (
	// SignMethodGPG selects the OpenPGP detached-signature flow.
	// Default for backwards compatibility with the original CI
	// surface; existing consumers continue to verify with `gpg`.
	SignMethodGPG SignMethod = "gpg"

	// SignMethodSigstore selects keyless cosign signing via OIDC.
	// No --key required; the OIDC issuer is auto-detected from the
	// runner platform or set via --oidc-issuer.
	SignMethodSigstore SignMethod = "sigstore"

	// SignMethodKMS selects cosign signing against an explicit key
	// reference (KMS URI, PKCS#11 URI, or local key file path).
	// --key is required.
	SignMethodKMS SignMethod = "kms"
)

// DefaultSignMethod is what `release sign` uses when neither the
// CLI flag nor artifacts.yml specifies a method. GPG is the default
// because it preserves the historical contract for existing
// downstream consumers; new repos should set sign.method explicitly.
const DefaultSignMethod = SignMethodGPG

// ValidSignMethods lists every SignMethod in canonical order. It is
// the single definition of the set: ParseSignMethod accepts exactly
// these values and the generated artifacts.yml JSON Schema renders
// its sign.method enum from this slice.
//
//nolint:gochecknoglobals // schema enumeration — read-only and ordered.
var ValidSignMethods = []SignMethod{
	SignMethodGPG,
	SignMethodSigstore,
	SignMethodKMS,
}

// ParseSignMethod validates and returns a SignMethod from raw input.
// Empty input is rejected; callers that want a default should fall
// back to DefaultSignMethod themselves, so the "user typed nothing"
// vs "user typed garbage" cases stay distinct in error messages.
func ParseSignMethod(raw string) (SignMethod, error) {
	if raw == "" {
		return "", fmt.Errorf("sign method is empty: %w", errs.ErrMissingInput)
	}

	if slices.Contains(ValidSignMethods, SignMethod(raw)) {
		return SignMethod(raw), nil
	}

	return "", fmt.Errorf(
		"sign method %q is not one of [%s]: %w",
		raw, joinMethods(ValidSignMethods), errs.ErrInvalidConfig,
	)
}

// joinMethods renders a method list for error messages: "gpg, sigstore, kms".
func joinMethods(methods []SignMethod) string {
	parts := make([]string, len(methods))
	for i, method := range methods {
		parts[i] = string(method)
	}

	return strings.Join(parts, ", ")
}

// SignatureExtensions returns the sidecar file extensions a given
// method produces alongside an artifact. GPG produces one armored
// detached signature (`.asc`); cosign methods (both Sigstore-keyless
// and KMS) emit a single Sigstore bundle (`.bundle`) — the v3 bundle
// format wraps the signature, optional Fulcio cert, and Rekor
// inclusion proof into one self-contained JSON document, so the
// downstream verifier only needs the bundle file plus the matching
// identity constraints (cert-identity-regexp + oidc-issuer for
// keyless, or pubkey for KMS).
//
// Returned slices are stable and safe to range over.
func (m SignMethod) SignatureExtensions() []string {
	switch m {
	case SignMethodGPG:
		return []string{".asc"}
	case SignMethodSigstore, SignMethodKMS:
		return []string{".bundle"}
	default:
		return nil
	}
}
