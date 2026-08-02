// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package baseimages

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

type verifiedBaseImage struct {
	Flavor       string `json:"flavor"`
	Tag          string `json:"tag"`
	Ref          string `json:"ref"`
	CandidateTag string `json:"candidate_tag"`
	CandidateRef string `json:"candidate_ref"`
	BaseInputID  string `json:"base_input_id"`
}

// VerifyExistingBaseImages checks the immutable final base-image tags for the
// requested flavors. Missing tags are reported as cache misses, matching the
// previous workflow behavior; a tag that exists must verify with signature, SBOM
// attestation, and SLSA lineage before it is returned.
func VerifyExistingBaseImages(ctx context.Context, resolver baseImageRegistry, verifier imageEvidenceVerifier, out io.Writer, in BaseImageVerifyExistingInput) (BaseImageVerifyExistingResult, error) {
	if err := validateVerifyExistingBaseInput(resolver, verifier, in); err != nil {
		return BaseImageVerifyExistingResult{}, err
	}

	baseInputs, err := baseInputIDByFlavor(in.BaseInputs)
	if err != nil {
		return BaseImageVerifyExistingResult{}, err
	}

	allFound := true
	existing := make([]verifiedBaseImage, 0, len(in.Flavors))
	missing := make([]string, 0)

	for _, flavor := range in.Flavors {
		baseInputID, idErr := existingBaseInputID(baseInputs, flavor, in.BaseInputID)
		if idErr != nil {
			return BaseImageVerifyExistingResult{}, idErr
		}

		image, found, verifyErr := verifyOneExistingBaseImage(ctx, resolver, verifier, out, in, flavor, baseInputID)
		if verifyErr != nil {
			return BaseImageVerifyExistingResult{}, verifyErr
		}

		if !found {
			allFound = false

			missing = append(missing, flavor)

			continue
		}

		existing = append(existing, image)
	}

	allImagesJSON, err := marshalSortedBaseImages(existing)
	if err != nil {
		return BaseImageVerifyExistingResult{}, err
	}

	slices.Sort(missing)

	missingJSON, err := compactJSON(missing)
	if err != nil {
		return BaseImageVerifyExistingResult{}, err
	}

	return BaseImageVerifyExistingResult{AllFound: allFound, AllImagesJSON: allImagesJSON, MissingFlavorsJSON: missingJSON}, nil
}

func validateVerifyExistingBaseInput(resolver baseImageRegistry, verifier imageEvidenceVerifier, in BaseImageVerifyExistingInput) error {
	if resolver == nil {
		return fmt.Errorf("base images verify: registry resolver is required: %w", errs.ErrUsage)
	}

	if verifier == nil {
		return fmt.Errorf("base images verify: cosign verifier is required: %w", errs.ErrUsage)
	}

	if err := validateBaseImageCommon(in.ExpectedRepository, in.ExpectedSource, in.ExpectedWorkflow, in.BaseInputID, in.CosignPublicKey); err != nil {
		return err
	}

	if len(in.Flavors) == 0 {
		return fmt.Errorf("base images verify: flavors file contains no flavors: %w", errs.ErrValidation)
	}

	return nil
}

// existingBaseInputID picks the per-flavor base input ID, falling back to the
// shared one, and validates both the flavor and the ID.
func existingBaseInputID(baseInputs map[string]string, flavor, fallback string) (string, error) {
	if !baseImageFlavorRE.MatchString(flavor) {
		return "", fmt.Errorf("base images verify: invalid flavor name: %s: %w", flavor, errs.ErrValidation)
	}

	baseInputID := baseInputs[flavor]
	if baseInputID == "" {
		baseInputID = fallback
	}

	if !domaincontainer.ValidSHA256Hex(baseInputID) {
		return "", fmt.Errorf("base images verify: missing or invalid base input ID for flavor: %s: %w", flavor, errs.ErrValidation)
	}

	return baseInputID, nil
}

// verifyOneExistingBaseImage resolves one flavor's final tag and verifies its
// evidence. A missing tag is a cache miss (found=false), not an error.
func verifyOneExistingBaseImage(ctx context.Context, resolver baseImageRegistry, verifier imageEvidenceVerifier, out io.Writer, in BaseImageVerifyExistingInput, flavor, baseInputID string) (verifiedBaseImage, bool, error) {
	tag := fmt.Sprintf("%s:%s-%s", in.ExpectedRepository, baseInputID, flavor)

	digest, err := resolver.ResolveDigest(ctx, tag)
	if err != nil {
		_, _ = fmt.Fprintf(out, "Base image missing and will be built if needed: %s\n", tag)

		return verifiedBaseImage{}, false, nil //nolint:nilerr // missing final tag is a cache miss (found=false), not an error.
	}

	ref := in.ExpectedRepository + "@" + digest
	if err := verifyBaseImageEvidence(ctx, verifier, out, ref, in.CosignPublicKey, baseImageLineageExpectation{
		Source: in.ExpectedSource, Workflow: in.ExpectedWorkflow, Flavor: flavor, BaseInputID: baseInputID,
	}, 1); err != nil {
		return verifiedBaseImage{}, false, fmt.Errorf("base images verify: final base tag exists but does not have the expected signature and attestation: %s: %w", tag, err)
	}

	_, _ = fmt.Fprintf(out, "Base image already exists and verifies: %s\n", ref)

	return verifiedBaseImage{Flavor: flavor, Tag: tag, Ref: ref, CandidateTag: "", CandidateRef: "", BaseInputID: baseInputID}, true, nil
}

func baseInputIDByFlavor(inputs []BaseInput) (map[string]string, error) {
	byFlavor := make(map[string]string, len(inputs))
	for idx, input := range inputs {
		if input.Flavor == "" || !domaincontainer.ValidSHA256Hex(input.BaseInputID) {
			return nil, fmt.Errorf("base images verify: base-inputs-json entry %d must include flavor and sha256 base_input_id: %w", idx, errs.ErrValidation)
		}

		if _, exists := byFlavor[input.Flavor]; !exists {
			byFlavor[input.Flavor] = input.BaseInputID
		}
	}

	return byFlavor, nil
}

func marshalSortedBaseImages(images []verifiedBaseImage) (string, error) {
	slices.SortFunc(images, func(a, b verifiedBaseImage) int { return strings.Compare(a.Flavor, b.Flavor) })

	return compactJSON(images)
}
