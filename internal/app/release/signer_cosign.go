// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package release

import (
	"context"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domain "github.com/diggsweden/reusable-ci/internal/domain/release"
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
	method     domain.SignMethod
	keyRef     string
	oidcIssuer string
	errOut     io.Writer
}

// cosignSignBlobber is the slice of *cosign.Adapter that CosignSigner
// needs. Defining it as an interface lets unit tests inject a fake
// without spinning up the mockbinary stack.
type cosignSignBlobber interface {
	SignBlob(ctx context.Context, in cosign.SignBlobInput, errOut io.Writer) error
}

// CosignSignerInput captures the per-invocation configuration for
// constructing a CosignSigner.
type CosignSignerInput struct {
	// Method is SignMethodSigstore or SignMethodKMS. Other values
	// (gpg, "") are rejected — caller picked the wrong factory.
	Method domain.SignMethod

	// KeyRef is required when Method == SignMethodKMS, forbidden
	// when Method == SignMethodSigstore. Passed verbatim to cosign's
	// --key (KMS URI, PKCS#11 URI, or local key-file path).
	KeyRef string

	// OIDCIssuer is optional when Method == SignMethodSigstore.
	// Empty lets cosign auto-detect from the runner. Forbidden when
	// Method == SignMethodKMS.
	OIDCIssuer string
}

// NewCosignSigner constructs a CosignSigner. errOut receives a
// redacted view of cosign's stderr for every SignFile call.
func NewCosignSigner(adapter cosignSignBlobber, in CosignSignerInput, errOut io.Writer) (*CosignSigner, error) {
	if adapter == nil {
		return nil, fmt.Errorf("cosign signer: adapter is required: %w", errs.ErrUsage)
	}

	switch in.Method {
	case domain.SignMethodSigstore:
		if in.KeyRef != "" {
			return nil, fmt.Errorf("cosign signer (sigstore): KeyRef forbidden (got %q): %w", in.KeyRef, errs.ErrUsage)
		}
	case domain.SignMethodKMS:
		if in.KeyRef == "" {
			return nil, fmt.Errorf("cosign signer (kms): KeyRef is required: %w", errs.ErrUsage)
		}

		if in.OIDCIssuer != "" {
			return nil, fmt.Errorf("cosign signer (kms): OIDCIssuer forbidden (got %q): %w", in.OIDCIssuer, errs.ErrUsage)
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
	in := cosign.SignBlobInput{
		Artefact:   file,
		BundlePath: file + ".bundle",
	}

	switch s.method {
	case domain.SignMethodSigstore:
		in.Keyless = true
		in.OIDCIssuer = s.oidcIssuer
	case domain.SignMethodKMS:
		in.KeyRef = s.keyRef
	default:
		return fmt.Errorf("cosign signer: invariant violated, method %q is not sigstore/kms: %w", s.method, errs.ErrInvalidConfig)
	}

	return s.adapter.SignBlob(ctx, in, s.errOut)
}
