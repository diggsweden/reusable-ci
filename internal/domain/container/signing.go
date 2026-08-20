// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// Well-known cosign --type names for the attestation predicates this
// pipeline attaches to images: SLSA Provenance v1 and CycloneDX SBOMs.
const (
	PredicateTypeSLSAProvenance1 = "slsaprovenance1"
	PredicateTypeCycloneDX       = "cyclonedx"
)

// UnsafeCosignErrorLine reports whether a cosign stderr line may carry
// credential material (authorization headers, tokens, …) and must not be
// echoed into CI logs or step summaries.
func UnsafeCosignErrorLine(line string) bool {
	lower := strings.ToLower(line)
	for _, marker := range []string{"authorization", "bearer", "token", "password", "secret"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	return false
}

// ImageSignRequest is the port-level request for signing an OCI image.
// The cosign adapter implements the operation (its SignImageInput is an
// alias of this type); app-layer use cases construct it without
// importing the adapter, mirroring BuildRequest. Unlike blob signing,
// the signature is stored in the OCI registry next to the image (as a
// sibling tag), not on the local filesystem — so no bundle path exists.
type ImageSignRequest struct {
	// ImageRef is the OCI reference. Must be a fully-qualified
	// digest reference (registry/image@sha256:...) — cosign refuses
	// to sign a mutable tag.
	ImageRef string

	// Recursive walks a manifest-list, signing each per-arch digest
	// referenced by the list in addition to the list itself.
	// Production releases use multi-arch manifest lists, so this is
	// almost always true.
	Recursive bool

	// Keyless selects Sigstore-keyless mode. When true, KeyRef must
	// be empty.
	Keyless bool

	// OIDCIssuer overrides cosign's default Sigstore OIDC issuer.
	// Only meaningful when Keyless is true; must be empty otherwise.
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

	// TrustedRootPath supplies the trust material cosign verifies against after
	// signing. Empty uses cosign's own, which is correct for public Sigstore.
	//
	// Needed for a self-hosted CA: cosign verifies the certificate it has just
	// been issued, and has no way to learn a private CA's root -- so signing
	// succeeds and verification fails, with "failed to verify leaf certificate".
	//
	// A path to cosign's own trusted-root document rather than a certificate,
	// because a private deployment has more than a CA to declare (log and CTFE
	// keys), and `cosign trusted-root create` already builds it. Owning a second
	// Sigstore document format here would buy nothing.
	TrustedRootPath string

	// KeyRef is the cosign --key URI for non-keyless signing
	// (awskms://, hashivault://, etc.). Must be empty when
	// Keyless is true; required otherwise.
	KeyRef string
}

// requireDigestRef enforces the rule all four cosign requests share: the
// image must be named by digest, never by a tag.
//
// It exists as one function because the rule was copied three times and
// the fourth copy was simply missing -- `attest` had no digest check at
// all, so an attestation could be bound to a mutable tag while its
// siblings refused one. A shared helper is how that stays fixed.
//
// why says what a mutable tag costs for this particular operation; the
// operations differ enough that one message would be vague.
func requireDigestRef(op, ref, why string) error {
	if !strings.Contains(ref, "@sha256:") {
		return fmt.Errorf("%s: image reference %q must be a digest reference (registry/image@sha256:...); %s: %w",
			op, ref, why, errs.ErrUsage)
	}

	return nil
}

// requireKeylessConsistency enforces the keyless/--key/--oidc-issuer trio
// that sign and attest share. Same reasoning as requireDigestRef: the
// three checks were written twice, and two copies of a rule is how they
// drift.
func requireKeylessConsistency(op string, keyless bool, keyRef, oidcIssuer string) error {
	if keyless && keyRef != "" {
		return fmt.Errorf("%s: keyless mode forbids --key (got %q): %w", op, keyRef, errs.ErrUsage)
	}

	if !keyless && keyRef == "" {
		return fmt.Errorf("%s: non-keyless mode requires --key: %w", op, errs.ErrUsage)
	}

	if !keyless && oidcIssuer != "" {
		return fmt.Errorf("%s: --oidc-issuer only applies to keyless mode: %w", op, errs.ErrUsage)
	}

	return nil
}

// Validate enforces request consistency. The image ref must be a
// digest reference — cosign refuses tag-based signing, but the rule is
// surfaced here so the operator gets an actionable error instead of a
// cryptic cosign-internal failure.
func (in ImageSignRequest) Validate() error {
	if in.ImageRef == "" {
		return fmt.Errorf("cosign sign image: image reference is empty: %w", errs.ErrUsage)
	}

	if err := requireDigestRef("cosign sign image", in.ImageRef, "cosign refuses to sign mutable tags"); err != nil {
		return err
	}

	if err := requireKeylessConsistency("cosign sign image", in.Keyless, in.KeyRef, in.OIDCIssuer); err != nil {
		return err
	}

	return nil
}

// ImageAttestRequest is the port-level request for attaching a signed
// in-toto attestation (SLSA provenance, SBOM, …) to an OCI image in
// the registry.
type ImageAttestRequest struct {
	// ImageRef is a fully-qualified digest reference. cosign refuses a
	// mutable tag — an attestation must bind to immutable content.
	ImageRef string

	// PredicateType is cosign's --type: a well-known name
	// (slsaprovenance, cyclonedx, spdx, …) or a predicate-type URI.
	PredicateType string

	// PredicatePath is the file holding the predicate JSON (the SLSA
	// provenance predicate, the CycloneDX SBOM, …).
	PredicatePath string

	// Recursive attests each per-arch child of a manifest list in
	// addition to the list itself — used for one provenance predicate
	// that applies to the whole multi-arch release.
	Recursive bool

	// Keyless selects Sigstore-keyless mode. KeyRef must be empty.
	Keyless bool

	// OIDCIssuer overrides the default issuer; keyless-only.
	OIDCIssuer string

	// FulcioURL and RekorURL override the Sigstore services, for a
	// self-hosted deployment. Keyless-only, and the same reasoning as
	// ImageSignRequest: an issuer override alone still asks the public CA.
	FulcioURL string
	RekorURL  string

	// TrustedRootPath is the trust material for verifying against a
	// self-hosted CA; see ImageSignRequest.
	TrustedRootPath string

	// KeyRef is the cosign --key URI for non-keyless attestation.
	KeyRef string
}

// Validate checks request consistency before any subprocess runs — a
// single ErrUsage naming the field at fault instead of a cryptic
// cosign exit.
func (in ImageAttestRequest) Validate() error {
	if in.ImageRef == "" {
		return fmt.Errorf("cosign attest: image reference is empty: %w", errs.ErrUsage)
	}

	if err := requireDigestRef("cosign attest", in.ImageRef,
		"attesting a mutable tag records a claim about whatever it resolves to"); err != nil {
		return err
	}

	if in.PredicateType == "" {
		return fmt.Errorf("cosign attest: predicate type is empty: %w", errs.ErrUsage)
	}

	if in.PredicatePath == "" {
		return fmt.Errorf("cosign attest: predicate path is empty: %w", errs.ErrUsage)
	}

	if err := requireKeylessConsistency("cosign attest", in.Keyless, in.KeyRef, in.OIDCIssuer); err != nil {
		return err
	}

	return nil
}

// ImageVerifyRequest is the port-level request for verifying an OCI
// image signature. The signature is read from the registry — no local
// sidecar path is needed.
type ImageVerifyRequest struct {
	ImageRef string

	// Keyless verification: requires CertIdentityRegexp +
	// CertOIDCIssuer. KeyRef must be empty.
	Keyless            bool
	CertIdentityRegexp string
	CertOIDCIssuer     string

	// KMS verification: requires KeyRef. The other identity fields
	// must be empty.
	KeyRef string
}

// Validate enforces request consistency before any subprocess runs.
func (in ImageVerifyRequest) Validate() error {
	if in.ImageRef == "" {
		return fmt.Errorf("cosign verify image: image reference is empty: %w", errs.ErrUsage)
	}

	if err := requireDigestRef("cosign verify image", in.ImageRef, "verifying a mutable tag is unsafe"); err != nil {
		return err
	}

	if in.Keyless {
		if in.CertIdentityRegexp == "" {
			return fmt.Errorf("cosign verify image (keyless): cert-identity-regexp is empty: %w", errs.ErrUsage)
		}

		if in.CertOIDCIssuer == "" {
			return fmt.Errorf("cosign verify image (keyless): cert-oidc-issuer is empty: %w", errs.ErrUsage)
		}

		if in.KeyRef != "" {
			return fmt.Errorf("cosign verify image: keyless mode forbids --key (got %q): %w", in.KeyRef, errs.ErrUsage)
		}

		return nil
	}

	if in.KeyRef == "" {
		return fmt.Errorf("cosign verify image: non-keyless mode requires --key: %w", errs.ErrUsage)
	}

	return nil
}

// AttestationVerifyRequest is the port-level request for verifying a
// signed in-toto attestation of a given predicate type, attached to an
// image in the registry, against the signing identity. The attestation
// is read from the registry — no local path is needed.
type AttestationVerifyRequest struct {
	ImageRef string

	// PredicateType is cosign's --type: slsaprovenance1 (SLSA v1.0) | cyclonedx
	// | spdx | a predicate-type URI. Bare "slsaprovenance" is the obsolete v0.2.
	PredicateType string

	// Keyless verification: requires CertIdentityRegexp + CertOIDCIssuer.
	// KeyRef must be empty.
	Keyless            bool
	CertIdentityRegexp string
	CertOIDCIssuer     string

	// KMS verification: requires KeyRef. The identity fields must be empty.
	KeyRef string
}

// Validate enforces request consistency before any subprocess runs.
func (in AttestationVerifyRequest) Validate() error {
	if in.ImageRef == "" {
		return fmt.Errorf("cosign verify-attestation: image reference is empty: %w", errs.ErrUsage)
	}

	if err := requireDigestRef("cosign verify-attestation", in.ImageRef, "verifying a mutable tag is unsafe"); err != nil {
		return err
	}

	if in.PredicateType == "" {
		return fmt.Errorf("cosign verify-attestation: predicate type is empty: %w", errs.ErrUsage)
	}

	if in.Keyless {
		if in.CertIdentityRegexp == "" {
			return fmt.Errorf("cosign verify-attestation (keyless): cert-identity-regexp is empty: %w", errs.ErrUsage)
		}

		if in.CertOIDCIssuer == "" {
			return fmt.Errorf("cosign verify-attestation (keyless): cert-oidc-issuer is empty: %w", errs.ErrUsage)
		}

		if in.KeyRef != "" {
			return fmt.Errorf("cosign verify-attestation: keyless mode forbids --key (got %q): %w", in.KeyRef, errs.ErrUsage)
		}

		return nil
	}

	if in.KeyRef == "" {
		return fmt.Errorf("cosign verify-attestation: non-keyless mode requires --key: %w", errs.ErrUsage)
	}

	return nil
}
