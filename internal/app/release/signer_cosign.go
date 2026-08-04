// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// CosignSigner adapts the low-level cosign subprocess wrapper to the
// release.Signer interface. One CosignSigner is constructed per
// `release sign` invocation, capturing the chosen method
// (sigstore | kms) and the method-specific configuration (KeyRef
// for KMS, OIDCIssuer for Sigstore). SignFile then dispatches to
// `cosign sign-blob` with the right argv each time.
//
// The struct is small and immutable after construction — it carries
// no secret material (the OIDC token lives in cosign's subprocess,
// the KMS private key lives in the KMS provider). This is why the
// swap-refusal policy does NOT apply to the cosign signing path:
// there is no decrypted key in our heap to leak.
type CosignSigner struct {
	adapter    cosignSignBlobber
	method     domainrelease.SignMethod
	keyRef     string
	oidcIssuer string
	fulcioURL  string
	rekorURL   string
	errOut     io.Writer
}

// cosignSignBlobber is the slice of *cosign.Adapter that CosignSigner
// needs. Defining it as an interface lets unit tests inject a fake
// without spinning up the mockbinary stack.
type cosignSignBlobber interface {
	SignBlob(ctx context.Context, in domainrelease.BlobSignRequest, errOut io.Writer) error
}

// CosignSignerInput captures the per-invocation configuration for
// constructing a CosignSigner.
type CosignSignerInput struct {
	// Method is SignMethodSigstore or SignMethodKMS. Other values
	// (gpg, "") are rejected — caller picked the wrong factory.
	Method domainrelease.SignMethod

	// KeyRef is required when Method == SignMethodKMS, forbidden
	// when Method == SignMethodSigstore. Passed verbatim to cosign's
	// --key (KMS URI, PKCS#11 URI, or local key-file path).
	KeyRef string

	// OIDCIssuer is optional when Method == SignMethodSigstore.
	// Empty lets cosign auto-detect from the runner. Forbidden when
	// Method == SignMethodKMS.
	OIDCIssuer string

	// FulcioURL and RekorURL point keyless signing at a self-hosted
	// Sigstore. Empty uses cosign's defaults. Both are forbidden when
	// Method == SignMethodKMS, which contacts no Sigstore service at all.
	FulcioURL string
	RekorURL  string
}

// NewCosignSigner constructs a CosignSigner. errOut receives a
// redacted view of cosign's stderr for every SignFile call.
func NewCosignSigner(adapter cosignSignBlobber, in CosignSignerInput, errOut io.Writer) (*CosignSigner, error) {
	if adapter == nil {
		return nil, fmt.Errorf("cosign signer: adapter is required: %w", errs.ErrUsage)
	}

	switch in.Method {
	case domainrelease.SignMethodSigstore:
		if in.KeyRef != "" {
			return nil, fmt.Errorf("cosign signer (sigstore): KeyRef forbidden (got %q): %w", in.KeyRef, errs.ErrUsage)
		}
	case domainrelease.SignMethodKMS:
		if in.KeyRef == "" {
			return nil, fmt.Errorf("cosign signer (kms): KeyRef is required: %w", errs.ErrUsage)
		}

		if in.OIDCIssuer != "" {
			return nil, fmt.Errorf("cosign signer (kms): OIDCIssuer forbidden (got %q): %w", in.OIDCIssuer, errs.ErrUsage)
		}

		if in.FulcioURL != "" {
			return nil, fmt.Errorf("cosign signer (kms): FulcioURL forbidden (got %q): %w", in.FulcioURL, errs.ErrUsage)
		}

		if in.RekorURL != "" {
			return nil, fmt.Errorf("cosign signer (kms): RekorURL forbidden (got %q): %w", in.RekorURL, errs.ErrUsage)
		}
	default:
		return nil, fmt.Errorf(
			"cosign signer: method %q not supported (use sigstore or kms): %w",
			in.Method, errs.ErrUsage,
		)
	}

	return &CosignSigner{
		adapter:    adapter,
		method:     in.Method,
		keyRef:     in.KeyRef,
		oidcIssuer: in.OIDCIssuer,
		fulcioURL:  in.FulcioURL,
		rekorURL:   in.RekorURL,
		errOut:     errOut,
	}, nil
}

// Extensions reports the sidecar files this signer produces. Both
// cosign methods emit a single v3 bundle sidecar that wraps
// signature + (keyless only) Fulcio cert + Rekor proof.
func (s *CosignSigner) Extensions() []string { return s.method.SignatureExtensions() }

// SignFile invokes `cosign sign-blob` against file. The v3 bundle
// lands at <file>.bundle. The caller's asset-walking loop handles
// any subsequent rename.
func (s *CosignSigner) SignFile(ctx context.Context, file string) error {
	in := domainrelease.BlobSignRequest{
		Artifact:   file,
		BundlePath: file + ".bundle",
	}

	switch s.method {
	case domainrelease.SignMethodSigstore:
		in.Keyless = true
		in.OIDCIssuer = s.oidcIssuer
		in.FulcioURL = s.fulcioURL
		in.RekorURL = s.rekorURL
	case domainrelease.SignMethodKMS:
		in.KeyRef = s.keyRef
	default:
		return fmt.Errorf("cosign signer: invariant violated, method %q is not sigstore/kms: %w", s.method, errs.ErrInvalidConfig)
	}

	return s.adapter.SignBlob(ctx, in, s.errOut)
}
