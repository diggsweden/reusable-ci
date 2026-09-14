// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// AttestImageInput drives `reusable-ci container attest`. It attaches a
// signed in-toto attestation (SLSA provenance or an SBOM) to an image
// digest via cosign — portable and verifiable with `cosign
// verify-attestation` on any forge, unlike BuildKit's unsigned in-index
// attestations or GitHub's attestation-API actions.
type AttestImageInput struct {
	// Image is the fully-qualified OCI digest reference.
	Image string

	// Method is sigstore | kms. gpg is rejected.
	Method domainrelease.SignMethod

	// PredicateType is cosign's --type (slsaprovenance1 | cyclonedx |
	// spdx | a predicate-type URI). Only SLSA v1.0 is supported via
	// cosign's "slsaprovenance1" alias; the obsolete "slsaprovenance"
	// (v0.2) is rejected.
	PredicateType string

	// PredicatePath is the predicate file. When empty and PredicateType
	// is "slsaprovenance1", the predicate is generated from Provenance.
	PredicatePath string

	// Provenance feeds the generated SLSA v1.0 predicate (used only when
	// PredicatePath is empty and PredicateType is slsaprovenance[1]).
	Provenance provenance.Input

	// Recursive attests each per-arch child of a manifest list too.
	Recursive bool

	// KeyRef is required for kms; OIDCIssuer is optional for sigstore.
	KeyRef     string
	OIDCIssuer string

	// FulcioURL and RekorURL point keyless attestation at a self-hosted
	// Sigstore. Empty uses cosign's defaults; sigstore-only.
	FulcioURL string
	RekorURL  string

	// TrustedRootPath is cosign's trusted-root document, needed to verify
	// against a self-hosted CA; sigstore-only.
	TrustedRootPath string
}

// cosignImageAttestor is the slice of *cosign.Adapter that attestation
// needs; lets tests inject a fake.
type cosignImageAttestor interface {
	AttestImage(ctx context.Context, in container.ImageAttestRequest, errOut io.Writer) error
}

// AttestImage materialises the predicate (generating the SLSA provenance
// when none is supplied) and dispatches to cosign. Errors wrap
// errs.ErrUsage / ErrMissingInput for input mistakes and propagate
// cosign's exit error otherwise.
func AttestImage(ctx context.Context, attestor cosignImageAttestor, out io.Writer, in AttestImageInput) error {
	if attestor == nil {
		return fmt.Errorf("container attest: cosign adapter is required: %w", errs.ErrUsage)
	}

	if in.Image == "" {
		return fmt.Errorf("container attest: image reference is empty: %w", errs.ErrMissingInput)
	}

	if in.PredicateType == "" {
		return fmt.Errorf("container attest: predicate type is required: %w", errs.ErrMissingInput)
	}

	var keyless bool

	switch in.Method {
	case domainrelease.SignMethodSigstore:
		keyless = true
	case domainrelease.SignMethodKMS:
	case domainrelease.SignMethodGPG:
		return fmt.Errorf(
			"container attest: method=gpg cannot attest OCI images — use --method=sigstore or --method=kms: %w",
			errs.ErrInvalidConfig)
	default:
		return fmt.Errorf("container attest: --method is required (sigstore or kms): %w", errs.ErrMissingInput)
	}

	predicatePath, predicateType, cleanup, err := resolvePredicate(in)
	if err != nil {
		return err
	}
	defer cleanup()

	_, _ = fmt.Fprintf(out, "Attesting %s to %s (method=%s)\n", predicateType, in.Image, in.Method)

	// Every identity and endpoint field is forwarded for both methods, and
	// the request's own validation decides which combinations are allowed.
	// Picking fields per method here dropped a key given with sigstore, and
	// the Sigstore endpoints and trust root given with kms, without a word,
	// where the release signer refuses the same mistakes.
	return attestor.AttestImage(ctx, container.ImageAttestRequest{
		ImageRef: in.Image, PredicateType: predicateType, PredicatePath: predicatePath,
		Recursive: in.Recursive, Keyless: keyless, KeyRef: in.KeyRef, OIDCIssuer: in.OIDCIssuer,
		FulcioURL: in.FulcioURL, RekorURL: in.RekorURL, TrustedRootPath: in.TrustedRootPath,
	}, out)
}

// resolvePredicate returns the predicate file path, the predicate type to pass
// to cosign, and a cleanup func. A supplied PredicatePath is used as-is (no
// cleanup) with the caller's type. When absent, a SLSA Provenance v1.0 predicate
// is generated from Provenance into a temp file (removed by cleanup).
//
// Only "slsaprovenance1" (SLSA Provenance v1.0) is generated. cosign's bare
// "slsaprovenance" alias is the obsolete v0.2 and is rejected with a pointer to
// v1.0 — the predicate domain/provenance emits is v1.0 (predicateType
// https://slsa.dev/provenance/v1), so emitting it under the v0.2 label would
// mislabel the statement and break v1.0 verifiers.
func resolvePredicate(in AttestImageInput) (string, string, func(), error) {
	noop := func() {}

	if in.PredicatePath != "" {
		return in.PredicatePath, in.PredicateType, noop, nil
	}

	if in.PredicateType == "slsaprovenance" {
		return "", "", noop, fmt.Errorf(
			"container attest: --type slsaprovenance is SLSA v0.2 and unsupported; use --type slsaprovenance1 (SLSA v1.0): %w", errs.ErrUsage)
	}

	if in.PredicateType != container.PredicateTypeSLSAProvenance1 {
		return "", "", noop, fmt.Errorf("container attest: --predicate is required for type %q: %w", in.PredicateType, errs.ErrMissingInput)
	}

	body, err := provenance.Predicate(in.Provenance)
	if err != nil {
		return "", "", noop, err
	}

	file, err := os.CreateTemp("", "slsa-provenance-*.json")
	if err != nil {
		return "", "", noop, fmt.Errorf("container attest: create predicate file: %w", err)
	}

	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())

		return "", "", noop, fmt.Errorf("container attest: write predicate: %w", err)
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())

		return "", "", noop, fmt.Errorf("container attest: close predicate: %w", err)
	}

	return file.Name(), container.PredicateTypeSLSAProvenance1, func() { _ = os.Remove(file.Name()) }, nil
}
