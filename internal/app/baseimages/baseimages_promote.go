// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package baseimages

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

type promotedBaseImage struct {
	Flavor      string `json:"flavor"`
	Tag         string `json:"tag"`
	Ref         string `json:"ref"`
	BaseInputID string `json:"base_input_id"`
}

type baseImageRefOutput struct {
	Flavor      string `json:"flavor"`
	Ref         string `json:"ref"`
	BaseInputID string `json:"base_input_id"`
}

type baseImageInputOutput struct {
	Flavor      string `json:"flavor"`
	BaseInputID string `json:"base_input_id"`
}

// PromoteBaseImages verifies candidate/base evidence, promotes missing final
// immutable tags, and emits the compact JSON outputs consumed by downstream base
// image jobs.
func PromoteBaseImages(ctx context.Context, registry baseImageRegistry, verifier imageEvidenceVerifier, out io.Writer, in BaseImagePromoteInput) (BaseImagePromoteResult, error) {
	if registry == nil {
		return BaseImagePromoteResult{}, fmt.Errorf("base images promote: registry is required: %w", errs.ErrUsage)
	}

	if verifier == nil {
		return BaseImagePromoteResult{}, fmt.Errorf("base images promote: cosign verifier is required: %w", errs.ErrUsage)
	}

	if err := validateBaseImageCommon(in.ExpectedRepository, in.ExpectedSource, in.ExpectedWorkflow, in.BaseInputID, in.CosignPublicKey); err != nil {
		return BaseImagePromoteResult{}, err
	}

	if len(in.Images) == 0 {
		return BaseImagePromoteResult{}, fmt.Errorf("base images promote: all-images-json must contain at least one image: %w", errs.ErrValidation)
	}

	promoted := make([]promotedBaseImage, 0, len(in.Images))
	for idx, image := range in.Images {
		item, err := normalizePromoteBaseImage(image, in.BaseInputID, in.ExpectedRepository)
		if err != nil {
			return BaseImagePromoteResult{}, fmt.Errorf("base images promote: entry %d: %w", idx, err)
		}

		ref, err := promoteOneBaseImage(ctx, registry, verifier, out, in, item)
		if err != nil {
			return BaseImagePromoteResult{}, err
		}

		promoted = append(promoted, promotedBaseImage{Flavor: item.Flavor, Tag: item.Tag, Ref: ref, BaseInputID: item.BaseInputID})
	}

	return baseImagePromotionResult(promoted)
}

// promoteOneBaseImage promotes one normalized base image (verifying its
// candidate evidence and copying the final tag when needed) and returns the
// digest-pinned ref recorded for downstream jobs.
func promoteOneBaseImage(ctx context.Context, registry baseImageRegistry, verifier imageEvidenceVerifier, out io.Writer, in BaseImagePromoteInput, item BaseImageMetadata) (string, error) {
	ref := item.Ref
	if item.CandidateRef != "" {
		if err := promoteCandidateBaseImage(ctx, registry, verifier, out, in, item); err != nil {
			return "", err
		}

		ref = item.CandidateRef
	} else if err := verifyBaseTagDigest(ctx, registry, item.Tag, digestFromRef(item.Ref), in.ExpectedRepository); err != nil {
		return "", err
	}

	_, _ = fmt.Fprintf(out, "Verifying promoted base image evidence: flavor=%s digest=%s\n", item.Flavor, digestFromRef(ref))
	if err := verifyBaseImageEvidence(ctx, verifier, out, ref, in.CosignPublicKey, baseImageLineageExpectation{
		Source: in.ExpectedSource, Workflow: in.ExpectedWorkflow, Flavor: item.Flavor, BaseInputID: item.BaseInputID,
	}, 3); err != nil {
		return "", err
	}

	return ref, nil
}

// promoteCandidateBaseImage verifies the candidate's evidence and tag digest,
// then ensures the immutable final tag exists and points at the candidate.
func promoteCandidateBaseImage(ctx context.Context, registry baseImageRegistry, verifier imageEvidenceVerifier, out io.Writer, in BaseImagePromoteInput, item BaseImageMetadata) error {
	digest := digestFromRef(item.CandidateRef)

	_, _ = fmt.Fprintf(out, "Verifying candidate base image before promotion: flavor=%s digest=%s\n", item.Flavor, digest)
	if err := verifyBaseImageEvidence(ctx, verifier, out, item.CandidateRef, in.CosignPublicKey, baseImageLineageExpectation{
		Source: in.ExpectedSource, Workflow: in.ExpectedWorkflow, Flavor: item.Flavor, BaseInputID: item.BaseInputID,
	}, 3); err != nil {
		return err
	}

	if item.CandidateTag != "" {
		if err := verifyBaseTagDigest(ctx, registry, item.CandidateTag, digest, in.ExpectedRepository); err != nil {
			return err
		}
	}

	return ensureFinalBaseTag(ctx, registry, out, item, digest)
}

// ensureFinalBaseTag copies the candidate to the immutable final tag when the
// final tag is absent, and rejects promotion when an existing final tag (or
// the tag just copied) does not resolve to the candidate digest.
func ensureFinalBaseTag(ctx context.Context, registry baseImageRegistry, out io.Writer, item BaseImageMetadata, digest string) error {
	finalDigest, err := registry.ResolveDigest(ctx, item.Tag)
	if err == nil {
		if finalDigest != digest {
			return fmt.Errorf("base images promote: immutable final base tag already exists with a different digest\n  tag:       %s\n  existing:  %s\n  candidate: %s: %w", item.Tag, finalDigest, digest, errs.ErrValidation)
		}

		return nil
	}

	_, _ = fmt.Fprintf(out, "Promoting base image: flavor=%s digest=%s final=%s\n", item.Flavor, digest, item.Tag)
	if copyErr := registry.CopyTag(ctx, item.CandidateRef, item.Tag); copyErr != nil {
		return copyErr
	}

	finalDigest, err = registry.ResolveDigest(ctx, item.Tag)
	if err != nil {
		return err
	}

	if finalDigest != digest {
		return fmt.Errorf("base images promote: final base digest changed during promotion: %w", errs.ErrValidation)
	}

	return nil
}

func baseImagePromotionResult(promoted []promotedBaseImage) (BaseImagePromoteResult, error) {
	slices.SortFunc(promoted, func(a, b promotedBaseImage) int { return strings.Compare(a.Flavor, b.Flavor) })

	refs := make([]baseImageRefOutput, 0, len(promoted))
	inputs := make([]baseImageInputOutput, 0, len(promoted))

	uniqueIDs := make(map[string]bool, len(promoted))
	for _, image := range promoted {
		refs = append(refs, baseImageRefOutput{Flavor: image.Flavor, Ref: image.Ref, BaseInputID: image.BaseInputID})
		inputs = append(inputs, baseImageInputOutput{Flavor: image.Flavor, BaseInputID: image.BaseInputID})
		uniqueIDs[image.BaseInputID] = true
	}

	refsJSON, err := compactJSON(refs)
	if err != nil {
		return BaseImagePromoteResult{}, err
	}

	inputsJSON, err := compactJSON(inputs)
	if err != nil {
		return BaseImagePromoteResult{}, err
	}

	baseInputID := ""

	if len(uniqueIDs) == 1 {
		for id := range uniqueIDs {
			baseInputID = id
		}
	}

	return BaseImagePromoteResult{BaseInputID: baseInputID, BaseImagesJSON: refsJSON, BaseInputIDsJSON: inputsJSON}, nil
}
