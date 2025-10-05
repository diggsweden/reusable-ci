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
// The struct is small and immutable after construction: it carries a key
// reference, not key bytes. OIDC tokens and remote KMS keys stay with cosign or
// the provider; for a local key-file reference, cosign reads the file in its
// subprocess. The swap-refusal policy does not apply because no decrypted key
// enters this process's Go heap.
type CosignSigner struct {
	adapter         cosignSignBlobber
	method          domainrelease.SignMethod
	keyRef          string
	oidcIssuer      string
	fulcioURL       string
	rekorURL        string
	trustedRootPath string
	errOut          io.Writer
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
	// Sigstore. Empty uses cosign's defaults. Both overrides are forbidden for
	// KMS; the adapter's transparency mode still decides whether KMS signing
	// publishes to the default public Rekor log.
	FulcioURL string
	RekorURL  string

	// TrustedRootPath is forbidden for KMS. It is cosign's trusted-root document, needed to verify
	// against a self-hosted CA; sigstore-only.
	TrustedRootPath string
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
			return nil, fmt.Errorf("cosign signer (sigstore): KeyRef forbidden: %w", errs.ErrUsage)
		}
	case domainrelease.SignMethodKMS:
		if in.KeyRef == "" {
			return nil, fmt.Errorf("cosign signer (kms): KeyRef is required: %w", errs.ErrUsage)
		}

		for _, field := range []struct{ name, value string }{
			{"OIDCIssuer", in.OIDCIssuer}, {"FulcioURL", in.FulcioURL},
			{"RekorURL", in.RekorURL}, {"TrustedRootPath", in.TrustedRootPath},
		} {
			if field.value != "" {
				return nil, fmt.Errorf("cosign signer (kms): %s forbidden: %w", field.name, errs.ErrUsage)
			}
		}
	default:
		return nil, fmt.Errorf(
			"cosign signer: method %q not supported (use sigstore or kms): %w",
			in.Method, errs.ErrUsage,
		)
	}

	return &CosignSigner{
		adapter:         adapter,
		method:          in.Method,
		keyRef:          in.KeyRef,
		oidcIssuer:      in.OIDCIssuer,
		fulcioURL:       in.FulcioURL,
		rekorURL:        in.RekorURL,
		trustedRootPath: in.TrustedRootPath,
		errOut:          errOut,
	}, nil
}

// Extensions reports the sidecar files this signer produces. Both cosign
// methods emit a single v3 bundle sidecar containing the signature, an optional
// keyless Fulcio certificate, and a Rekor proof when transparency is enabled.
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
		in.TrustedRootPath = s.trustedRootPath
	case domainrelease.SignMethodKMS:
		in.KeyRef = s.keyRef
	default:
		return fmt.Errorf("cosign signer: invariant violated, method %q is not sigstore/kms: %w", s.method, errs.ErrInvalidConfig)
	}

	return s.adapter.SignBlob(ctx, in, s.errOut)
}
