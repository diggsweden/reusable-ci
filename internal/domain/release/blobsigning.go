// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"fmt"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// BlobSignRequest is the port-level request for producing a Sigstore
// bundle signature over a release artifact file. The cosign adapter
// implements the operation (its SignBlobInput is an alias of this
// type); app-layer use cases construct it without importing the
// adapter — the same pattern as container.ImageSignRequest. The
// adapter never invents file paths: the caller decides where the
// bundle lands. cosign 3.x emits the v3 bundle format exclusively —
// one self-contained JSON sidecar holding signature, optional Fulcio
// certificate, and Rekor proof.
type BlobSignRequest struct {
	// Artifact is the file being signed.
	Artifact string

	// BundlePath receives the Sigstore v3 bundle JSON. Required for
	// every method (cosign 3.x removed the split sig+cert layout).
	BundlePath string

	// Keyless selects Sigstore-keyless mode. When true, KeyRef must
	// be empty; the adapter passes --yes (acknowledging the
	// transparency-log upload) and --oidc-issuer when set.
	Keyless bool

	// OIDCIssuer is the OIDC issuer URL for keyless signing. Empty
	// lets cosign auto-detect from the runner. Must be empty when
	// Keyless is false.
	OIDCIssuer string

	// FulcioURL overrides the certificate authority keyless signing asks for
	// a certificate. Empty uses cosign's default, the public Sigstore CA.
	//
	// Needed because a self-hosted Sigstore is not reachable by naming its
	// OIDC issuer alone: cosign still calls the public Fulcio, and the
	// signature comes back vouched for by the wrong CA. Regulated and
	// air-gapped deployments run their own, and until this existed they could
	// not use keyless signing here at all.
	FulcioURL string

	// RekorURL overrides the transparency log. Empty uses cosign's default.
	//
	// Separate from Transparency, which decides WHETHER to publish: this
	// decides where. A private Sigstore usually has both, and a lab has a
	// private Fulcio and no log at all.
	RekorURL string

	// KeyRef is the cosign --key argument: a KMS URI
	// (awskms://, gcpkms://, hashivault://, …), a PKCS#11 URI, or
	// a local key-file path. Must be empty when Keyless is true.
	KeyRef string
}

// Validate checks request consistency before any subprocess runs.
// Keeps the error surfaces narrow: callers get a single ErrUsage with
// the specific field at fault rather than a cryptic cosign-exit-1
// with a stack trace.
func (in BlobSignRequest) Validate() error {
	if in.Artifact == "" {
		return fmt.Errorf("cosign sign: artifact path is empty: %w", errs.ErrUsage)
	}

	if in.BundlePath == "" {
		return fmt.Errorf("cosign sign: bundle path is empty: %w", errs.ErrUsage)
	}

	if in.Keyless && in.KeyRef != "" {
		return fmt.Errorf("cosign sign: keyless mode forbids --key (got %q): %w", in.KeyRef, errs.ErrUsage)
	}

	if !in.Keyless && in.KeyRef == "" {
		return fmt.Errorf("cosign sign: non-keyless mode requires --key: %w", errs.ErrUsage)
	}

	if !in.Keyless && in.OIDCIssuer != "" {
		return fmt.Errorf("cosign sign: --oidc-issuer only applies to keyless mode: %w", errs.ErrUsage)
	}

	return nil
}

// BlobVerifyRequest is the port-level request for verifying a Sigstore
// bundle signature over a release artifact file. Like BlobSignRequest,
// the caller supplies every path; the adapter never guesses. cosign
// 3.x verify consumes a single bundle file and requires the caller to
// declare identity constraints (regexp + issuer for keyless; pubkey
// for KMS) — the bundle itself doesn't encode trust.
type BlobVerifyRequest struct {
	// Artifact is the file whose signature is being verified.
	Artifact string

	// BundlePath is the v3 bundle JSON sidecar (produced by Sign-
	// Blob). Required for every method.
	BundlePath string

	// Keyless verification — requires CertIdentityRegexp and
	// CertOIDCIssuer. KeyRef must be empty.
	Keyless            bool
	CertIdentityRegexp string
	CertOIDCIssuer     string

	// KeyRef is required for non-keyless verify. Local pubkey path
	// or KMS URI (cosign reads pubkey from KMS).
	KeyRef string
}

// Validate enforces request consistency before any subprocess runs.
func (in BlobVerifyRequest) Validate() error {
	if in.Artifact == "" {
		return fmt.Errorf("cosign verify: artifact path is empty: %w", errs.ErrUsage)
	}

	if in.BundlePath == "" {
		return fmt.Errorf("cosign verify: bundle path is empty: %w", errs.ErrUsage)
	}

	if in.Keyless {
		if in.CertIdentityRegexp == "" {
			return fmt.Errorf("cosign verify (keyless): cert-identity-regexp is empty: %w", errs.ErrUsage)
		}

		if in.CertOIDCIssuer == "" {
			return fmt.Errorf("cosign verify (keyless): cert-oidc-issuer is empty: %w", errs.ErrUsage)
		}

		if in.KeyRef != "" {
			return fmt.Errorf("cosign verify: keyless mode forbids --key (got %q): %w", in.KeyRef, errs.ErrUsage)
		}

		return nil
	}

	if in.KeyRef == "" {
		return fmt.Errorf("cosign verify: non-keyless mode requires --key: %w", errs.ErrUsage)
	}

	return nil
}
