// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/internal/domain/release"
)

// SignImageInput drives `reusable-ci container sign`. Image signing
// goes through cosign with registry-attached storage — there is no
// local sidecar. GPG cannot sign OCI images (OpenPGP signs blobs, not
// OCI manifests), so the gpg method is rejected upstream.
type SignImageInput struct {
	// Image is the fully-qualified OCI digest reference, e.g.
	// ghcr.io/diggsweden/myapp@sha256:abc123…. Mutable tags are
	// rejected by the adapter.
	Image string

	// Method is sigstore | kms. gpg returns an error — it cannot
	// sign OCI artefacts.
	Method domainrelease.SignMethod

	// Recursive walks manifest lists, signing each per-arch digest.
	// Production releases use manifest lists, so callers should set
	// this true for multi-arch builds.
	Recursive bool

	// KeyRef is required when Method == kms. Forbidden otherwise.
	KeyRef string

	// OIDCIssuer is optional when Method == sigstore. Forbidden
	// when Method == kms.
	OIDCIssuer string
}

// cosignImageSigner is the slice of *cosign.Adapter that container-
// signing needs. Lets tests inject an in-process fake.
type cosignImageSigner interface {
	SignImage(ctx context.Context, in cosign.SignImageInput, errOut io.Writer) error
}

// SignImage dispatches to the configured cosign signing path. Errors
// returned wrap errs.ErrUsage for input mistakes and propagate
// cosign's exit error otherwise.
//
// Returns nil on a successful signature push.
func SignImage(ctx context.Context, signer cosignImageSigner, out io.Writer, in SignImageInput) error {
	if signer == nil {
		return fmt.Errorf("container sign: cosign adapter is required: %w", errs.ErrUsage)
	}

	if in.Image == "" {
		return fmt.Errorf("container sign: image reference is empty: %w", errs.ErrMissingInput)
	}

	switch in.Method {
	case domainrelease.SignMethodSigstore:
		_, _ = fmt.Fprintf(out, "Signing image %s (method=sigstore, keyless OIDC)\n", in.Image)

		return signer.SignImage(ctx, cosign.SignImageInput{
			ImageRef:   in.Image,
			Recursive:  in.Recursive,
			Keyless:    true,
			OIDCIssuer: in.OIDCIssuer,
		}, out)
	case domainrelease.SignMethodKMS:
		_, _ = fmt.Fprintf(out, "Signing image %s (method=kms, key=%s)\n", in.Image, in.KeyRef)

		return signer.SignImage(ctx, cosign.SignImageInput{
			ImageRef:  in.Image,
			Recursive: in.Recursive,
			KeyRef:    in.KeyRef,
		}, out)
	case domainrelease.SignMethodGPG:
		return fmt.Errorf(
			"container sign: method=gpg cannot sign OCI images — use --method=sigstore (keyless OIDC) or --method=kms (cosign + KMS): %w",
			errs.ErrInvalidConfig,
		)
	default:
		return fmt.Errorf("container sign: method %q is not supported: %w", in.Method, errs.ErrInvalidConfig)
	}
}

// VerifyImageInput drives `reusable-ci validate container-signature`.
type VerifyImageInput struct {
	Image  string
	Method domainrelease.SignMethod

	// Sigstore-keyless verification:
	CertIdentityRegexp string
	CertOIDCIssuer     string

	// KMS verification:
	KeyRef string
}

// cosignImageVerifier mirrors cosignImageSigner for the verify side.
type cosignImageVerifier interface {
	VerifyImage(ctx context.Context, in cosign.VerifyImageInput, errOut io.Writer) error
}

// VerifyImage dispatches `cosign verify` against the chosen method.
//
// The method is required (no auto-detect from a sidecar — for images
// the signature is in the registry, not on disk, so detection would
// require a registry probe). Callers either pass --method explicitly
// or read sign.method from a known config-plan-json.
func VerifyImage(ctx context.Context, verifier cosignImageVerifier, out io.Writer, in VerifyImageInput) error {
	if verifier == nil {
		return fmt.Errorf("container verify: cosign adapter is required: %w", errs.ErrUsage)
	}

	if in.Image == "" {
		return fmt.Errorf("container verify: image reference is empty: %w", errs.ErrMissingInput)
	}

	switch in.Method {
	case domainrelease.SignMethodSigstore:
		_, _ = fmt.Fprintf(out, "Verifying image %s (method=sigstore)\n", in.Image)

		return verifier.VerifyImage(ctx, cosign.VerifyImageInput{
			ImageRef:           in.Image,
			Keyless:            true,
			CertIdentityRegexp: in.CertIdentityRegexp,
			CertOIDCIssuer:     in.CertOIDCIssuer,
		}, out)
	case domainrelease.SignMethodKMS:
		_, _ = fmt.Fprintf(out, "Verifying image %s (method=kms)\n", in.Image)

		return verifier.VerifyImage(ctx, cosign.VerifyImageInput{
			ImageRef: in.Image,
			KeyRef:   in.KeyRef,
		}, out)
	case domainrelease.SignMethodGPG:
		return fmt.Errorf(
			"container verify: method=gpg cannot verify OCI images — use --method=sigstore or --method=kms: %w",
			errs.ErrInvalidConfig,
		)
	default:
		return fmt.Errorf("container verify: --method is required (sigstore or kms): %w", errs.ErrMissingInput)
	}
}
