// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package baseimages implements the base-image lifecycle behind the
// `reusable-ci container base-*` and freshness commands: computing
// base-graph content IDs, collecting built/verified images into fan-in
// JSON, verifying existing final tags and their signed evidence, signing
// candidates with SBOM and SLSA base-lineage attestations, promoting
// candidates to immutable final tags, cleaning up staging tags, and
// checking that release images still sit on fresh bases.
package baseimages

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

var (
	baseImageFlavorRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	baseImageTagRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)+:[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
)

// BaseImageMetadata is the JSON shape exchanged by forgejo-ci's base-image
// workflows. Candidate fields are populated only for newly built images waiting
// to be promoted to their immutable final tag.
type BaseImageMetadata struct {
	Flavor       string `json:"flavor"`
	Tag          string `json:"tag"`
	Ref          string `json:"ref"`
	CandidateTag string `json:"candidate_tag,omitempty"`
	CandidateRef string `json:"candidate_ref,omitempty"`
	BaseInputID  string `json:"base_input_id"`
	SBOMSHA256   string `json:"sbom_sha256,omitempty"`
}

// BaseInput maps a flavor to the sha256 content ID that produced that base.
type BaseInput struct {
	Flavor      string `json:"flavor"`
	ContentID   string `json:"content_id,omitempty"`
	BaseInputID string `json:"base_input_id"`
}

// BaseImageVerifyExistingInput drives VerifyExistingBaseImages.
type BaseImageVerifyExistingInput struct {
	Flavors            []string
	BaseInputs         []BaseInput
	BaseInputID        string
	ExpectedRepository string
	ExpectedSource     string
	ExpectedWorkflow   string
	CosignPublicKey    string
}

// BaseImageVerifyExistingResult reports which final base tags exist and verify.
type BaseImageVerifyExistingResult struct {
	AllFound           bool
	AllImagesJSON      string
	MissingFlavorsJSON string
}

// BaseImagePromoteInput drives PromoteBaseImages.
type BaseImagePromoteInput struct {
	Images             []BaseImageMetadata
	BaseInputID        string
	ExpectedRepository string
	ExpectedSource     string
	ExpectedWorkflow   string
	CosignPublicKey    string
}

// BaseImagePromoteResult is the compact JSON output of PromoteBaseImages.
type BaseImagePromoteResult struct {
	BaseInputID      string
	BaseImagesJSON   string
	BaseInputIDsJSON string
}

// BaseImageCleanupStagingInput drives CleanupStagingBaseImages.
type BaseImageCleanupStagingInput struct {
	Images             []BaseImageMetadata
	BaseInputs         []BaseInput
	ExpectedRepository string
}

type baseImageRegistry interface {
	ResolveDigest(ctx context.Context, ref string) (string, error)
	CopyTag(ctx context.Context, source, dest string) error
}

// imageDigestResolver is the registry surface base-image cleanup and
// freshness checks share: resolve a tag or ref to its manifest digest.
type imageDigestResolver interface {
	ResolveDigest(ctx context.Context, ref string) (string, error)
}

type baseImageStagingCleaner interface {
	DeleteTag(ctx context.Context, ref string) error
	ListContainerPackageVersions(ctx context.Context, owner, name string) ([]string, error)
}

// imageEvidenceVerifier is the cosign surface base-image evidence
// verification needs.
type imageEvidenceVerifier interface {
	VerifyImage(ctx context.Context, in domaincontainer.ImageVerifyRequest, errOut io.Writer) error
	VerifyAttestation(ctx context.Context, in domaincontainer.AttestationVerifyRequest, errOut io.Writer) error
	VerifyAttestationOutput(ctx context.Context, in domaincontainer.AttestationVerifyRequest, out, errOut io.Writer) error
}

func validateBaseImageCommon(expectedRepository, expectedSource, expectedWorkflow, baseInputID, publicKey string) error {
	if expectedRepository == "" {
		return fmt.Errorf("base images: expected repository is required: %w", errs.ErrMissingInput)
	}

	if expectedSource == "" {
		return fmt.Errorf("base images: expected source is required: %w", errs.ErrMissingInput)
	}

	if expectedWorkflow == "" {
		return fmt.Errorf("base images: expected workflow is required: %w", errs.ErrMissingInput)
	}

	if publicKey == "" {
		return fmt.Errorf("base images: Cosign public key path is required: %w", errs.ErrMissingInput)
	}

	if baseInputID != "" && !domaincontainer.ValidSHA256Hex(baseInputID) {
		return fmt.Errorf("base images: base-input-id must be a sha256 hex digest: %w", errs.ErrValidation)
	}

	return nil
}

func normalizePromoteBaseImage(image BaseImageMetadata, fallbackBaseInputID, expectedRepository string) (BaseImageMetadata, error) {
	if image.BaseInputID == "" {
		image.BaseInputID = fallbackBaseInputID
	}

	if image.Flavor == "" || image.Tag == "" || image.Ref == "" {
		return image, fmt.Errorf("base image metadata must include flavor, tag, and ref: %w", errs.ErrValidation)
	}

	if !baseImageFlavorRE.MatchString(image.Flavor) {
		return image, fmt.Errorf("invalid flavor name: %s: %w", image.Flavor, errs.ErrValidation)
	}

	if err := validatePromoteBaseInputID(image, fallbackBaseInputID); err != nil {
		return image, err
	}

	if err := validateBaseImageTag(image.Tag, expectedRepository); err != nil {
		return image, err
	}

	if err := validateBaseImageRef(image.Ref, expectedRepository); err != nil {
		return image, err
	}

	return image, validatePromoteCandidateFields(image, expectedRepository)
}

// validatePromoteBaseInputID checks the metadata's base_input_id shape and,
// when a fallback/expected ID is supplied, that it matches.
func validatePromoteBaseInputID(image BaseImageMetadata, fallbackBaseInputID string) error {
	if !domaincontainer.ValidSHA256Hex(image.BaseInputID) {
		return fmt.Errorf("base image metadata has missing or invalid base_input_id\n  flavor: %s: %w", image.Flavor, errs.ErrValidation)
	}

	if fallbackBaseInputID != "" && image.BaseInputID != fallbackBaseInputID {
		return fmt.Errorf("base image metadata has unexpected base_input_id\n  flavor:   %s\n  expected: %s\n  actual:   %s: %w", image.Flavor, fallbackBaseInputID, image.BaseInputID, errs.ErrValidation)
	}

	return nil
}

// validatePromoteCandidateFields checks the optional candidate tag/ref pair.
func validatePromoteCandidateFields(image BaseImageMetadata, expectedRepository string) error {
	if image.CandidateRef != "" {
		if err := validateBaseImageRef(image.CandidateRef, expectedRepository); err != nil {
			return err
		}
	}

	if image.CandidateTag != "" {
		if err := validateBaseImageTag(image.CandidateTag, expectedRepository); err != nil {
			return err
		}
	}

	return nil
}

func validateBaseImageRef(ref, expectedRepository string) error {
	if !domaincontainer.ValidDigestPinnedRef(ref) {
		return fmt.Errorf("base images: image ref must be digest-pinned: %s: %w", ref, errs.ErrValidation)
	}

	if got := domaincontainer.StripTagOrDigest(ref); got != expectedRepository {
		return fmt.Errorf("base images: image ref must be under %s: %s: %w", expectedRepository, ref, errs.ErrValidation)
	}

	return nil
}

func validateBaseImageTag(tag, expectedRepository string) error {
	if !baseImageTagRE.MatchString(tag) {
		return fmt.Errorf("base images: image tag must be a registry path with a tag: %s: %w", tag, errs.ErrValidation)
	}

	if got := domaincontainer.StripTagOrDigest(tag); got != expectedRepository {
		return fmt.Errorf("base images: image tag must be under %s: %s: %w", expectedRepository, tag, errs.ErrValidation)
	}

	return nil
}

func verifyBaseTagDigest(ctx context.Context, resolver baseImageRegistry, tag, digest, expectedRepository string) error {
	if err := validateBaseImageTag(tag, expectedRepository); err != nil {
		return err
	}

	actualDigest, err := resolver.ResolveDigest(ctx, tag)
	if err != nil {
		return err
	}

	if actualDigest != digest {
		return fmt.Errorf("base images: image tag does not point to expected digest\n  tag:      %s\n  expected: %s\n  actual:   %s: %w", tag, digest, actualDigest, errs.ErrValidation)
	}

	return nil
}

func compactJSON(v any) (string, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("base images: marshal JSON output: %w", err)
	}

	return string(body), nil
}

func digestFromRef(ref string) string {
	_, digest, _ := strings.Cut(ref, "@")

	return digest
}
