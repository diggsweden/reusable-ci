// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// VerifyAttestationInput drives `reusable-ci validate container-attestation`.
// It re-checks a signed in-toto attestation (SLSA provenance or an SBOM) at the
// trust boundary — the verify side of `container attest`, the same discipline
// forgejo-ci's promote/verify-base workflows apply before moving a tag.
type VerifyAttestationInput struct {
	Image  string
	Method domainrelease.SignMethod

	// PredicateType is cosign's --type: slsaprovenance1 (SLSA v1.0) | cyclonedx
	// | spdx | a predicate-type URI.
	PredicateType string

	// Sigstore-keyless verification:
	CertIdentityRegexp string
	CertOIDCIssuer     string

	// KMS verification:
	KeyRef string
}

// cosignAttestationVerifier mirrors cosignImageVerifier for the attestation side.
type cosignAttestationVerifier interface {
	VerifyAttestation(ctx context.Context, in cosign.VerifyAttestationInput, errOut io.Writer) error
}

// VerifyAttestation re-verifies a registry-attached attestation of the given
// predicate type against the signing identity. Only SLSA v1.0 is supported for
// provenance: bare "slsaprovenance" (cosign's obsolete v0.2 alias) is rejected
// with a pointer to slsaprovenance1, matching `container attest`.
func VerifyAttestation(ctx context.Context, verifier cosignAttestationVerifier, out io.Writer, in VerifyAttestationInput) error {
	if verifier == nil {
		return fmt.Errorf("container verify-attestation: cosign adapter is required: %w", errs.ErrUsage)
	}

	if in.Image == "" {
		return fmt.Errorf("container verify-attestation: image reference is empty: %w", errs.ErrMissingInput)
	}

	if in.PredicateType == "" {
		return fmt.Errorf("container verify-attestation: predicate type is required: %w", errs.ErrMissingInput)
	}

	if in.PredicateType == "slsaprovenance" {
		return fmt.Errorf(
			"container verify-attestation: --type slsaprovenance is SLSA v0.2 and unsupported; use --type slsaprovenance1 (SLSA v1.0): %w", errs.ErrUsage)
	}

	switch in.Method {
	case domainrelease.SignMethodSigstore:
		_, _ = fmt.Fprintf(out, "Verifying %s attestation on %s (method=sigstore)\n", in.PredicateType, in.Image)

		return verifier.VerifyAttestation(ctx, cosign.VerifyAttestationInput{
			ImageRef:           in.Image,
			PredicateType:      in.PredicateType,
			Keyless:            true,
			CertIdentityRegexp: in.CertIdentityRegexp,
			CertOIDCIssuer:     in.CertOIDCIssuer,
		}, out)
	case domainrelease.SignMethodKMS:
		_, _ = fmt.Fprintf(out, "Verifying %s attestation on %s (method=kms)\n", in.PredicateType, in.Image)

		return verifier.VerifyAttestation(ctx, cosign.VerifyAttestationInput{
			ImageRef:      in.Image,
			PredicateType: in.PredicateType,
			KeyRef:        in.KeyRef,
		}, out)
	case domainrelease.SignMethodGPG:
		return fmt.Errorf(
			"container verify-attestation: method=gpg cannot verify OCI attestations — use --method=sigstore or --method=kms: %w",
			errs.ErrInvalidConfig,
		)
	default:
		return fmt.Errorf("container verify-attestation: --method is required (sigstore or kms): %w", errs.ErrMissingInput)
	}
}
