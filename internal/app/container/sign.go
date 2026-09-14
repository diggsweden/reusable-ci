// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// SignImageInput drives `reusable-ci container sign`. Image signing
// goes through cosign with registry-attached storage — there is no
// local sidecar. GPG cannot sign OCI images (OpenPGP signs blobs, not
// OCI manifests), so the gpg method is rejected upstream.
type SignImageInput struct {
	// Image is the fully-qualified OCI digest reference, e.g.
	// ghcr.io/examplescope/myapp@sha256:abc123…. Mutable tags are
	// rejected by the adapter.
	Image string

	// Method is sigstore | kms. gpg returns an error — it cannot
	// sign OCI artifacts.
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

	// FulcioURL and RekorURL point keyless signing at a self-hosted Sigstore.
	// Empty uses cosign's defaults. Meaningful only when Method == sigstore.
	FulcioURL string
	RekorURL  string

	// TrustedRootPath is cosign's trusted-root document, needed to verify
	// against a self-hosted CA; sigstore-only.
	TrustedRootPath string
}

// cosignImageSigner is the slice of *cosign.Adapter that container-
// signing needs, expressed against the domain port request so this
// package never imports the adapter. Lets tests inject an in-process
// fake.
type cosignImageSigner interface {
	SignImage(ctx context.Context, in container.ImageSignRequest, errOut io.Writer) error
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

	var mode string

	switch in.Method {
	case domainrelease.SignMethodSigstore:
		mode = "sigstore, keyless OIDC"
	case domainrelease.SignMethodKMS:
		mode = "kms"
	case domainrelease.SignMethodGPG:
		return fmt.Errorf(
			"container sign: method=gpg cannot sign OCI images — use --method=sigstore (keyless OIDC) or --method=kms (cosign + KMS): %w",
			errs.ErrInvalidConfig,
		)
	default:
		return fmt.Errorf("container sign: method %q is not supported: %w", in.Method, errs.ErrInvalidConfig)
	}

	_, _ = fmt.Fprintf(out, "Signing image %s (method=%s)\n", in.Image, mode)

	// Every field is forwarded for both methods, as ledger signing does, so
	// the request's validation refuses a key given with sigstore or an
	// endpoint given with kms instead of this switch dropping it.
	return signer.SignImage(ctx, container.ImageSignRequest{
		ImageRef:        in.Image,
		Recursive:       in.Recursive,
		Keyless:         in.Method == domainrelease.SignMethodSigstore,
		KeyRef:          in.KeyRef,
		OIDCIssuer:      in.OIDCIssuer,
		FulcioURL:       in.FulcioURL,
		RekorURL:        in.RekorURL,
		TrustedRootPath: in.TrustedRootPath,
	}, out)
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
	VerifyImage(ctx context.Context, in container.ImageVerifyRequest, errOut io.Writer) error
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
	case domainrelease.SignMethodSigstore, domainrelease.SignMethodKMS:
	case domainrelease.SignMethodGPG:
		return fmt.Errorf(
			"container verify: method=gpg cannot verify OCI images — use --method=sigstore or --method=kms: %w",
			errs.ErrInvalidConfig,
		)
	default:
		return fmt.Errorf("container verify: --method is required (sigstore or kms): %w", errs.ErrMissingInput)
	}

	_, _ = fmt.Fprintf(out, "Verifying image %s (method=%s)\n", in.Image, in.Method)

	// As for signing, every field is forwarded and the request's validation
	// refuses a combination the method does not take, rather than a
	// certificate identity given with kms being dropped unseen.
	request := container.ImageVerifyRequest{
		ImageRef:           in.Image,
		Keyless:            in.Method == domainrelease.SignMethodSigstore,
		CertIdentityRegexp: in.CertIdentityRegexp,
		CertOIDCIssuer:     in.CertOIDCIssuer,
		KeyRef:             in.KeyRef,
	}
	if err := request.Validate(); err != nil {
		return err
	}

	return wrapVerificationFailure("container signature verification failed", verifier.VerifyImage(ctx, request, out))
}

func wrapVerificationFailure(operation string, err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, errs.ErrDependencyUnavailable) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", operation, err)
	}

	return fmt.Errorf("%s: %w: %w", operation, err, errs.ErrValidation)
}
