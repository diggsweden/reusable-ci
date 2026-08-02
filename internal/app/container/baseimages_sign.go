// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cosign"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
)

var baseImagesSourceSHARE = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

// BaseImageSignInput drives SignBaseImages.
type BaseImageSignInput struct {
	Images             []BaseImageMetadata
	BaseInputID        string
	ExpectedRepository string
	ExpectedSource     string
	ExpectedWorkflow   string
	SourceSHA          string
	BuildType          string
	KeyRef             string
	PremadeSBOMDir     string
}

type baseImageSigner interface {
	SignImage(ctx context.Context, in cosign.SignImageInput, errOut io.Writer) error
	AttestImage(ctx context.Context, in cosign.AttestImageInput, errOut io.Writer) error
	PublicKey(ctx context.Context, keyRef string, out, errOut io.Writer) error
}

type baseImageArchiveCopier interface {
	CopyDockerToOCIArchive(ctx context.Context, ref, archive string, errOut io.Writer) error
}

// SignBaseImages signs each digest-pinned base image, attests a verified or
// generated CycloneDX SBOM, attests standard SLSA v1 base lineage, and verifies
// the registry-published evidence is readable before returning.
func SignBaseImages(ctx context.Context, signer baseImageSigner, verifier imageEvidenceVerifier, sbom ImageEvidenceSyft, archive baseImageArchiveCopier, resolver baseImageRegistry, out, stderr io.Writer, in BaseImageSignInput) error {
	if err := validateSignBaseDependencies(signer, verifier, resolver, sbom, archive, in.PremadeSBOMDir); err != nil {
		return err
	}

	if err := validateBaseImageSignInput(in); err != nil {
		return err
	}

	images, err := normalizeSignBaseImages(ctx, resolver, in)
	if err != nil {
		return err
	}

	tempDir, err := os.MkdirTemp("", "base-images-sign-*")
	if err != nil {
		return fmt.Errorf("base images sign: create temp dir: %w", err)
	}

	defer func() { _ = os.RemoveAll(tempDir) }()

	publicKeyPath, err := writeBaseImageSigningPublicKey(ctx, signer, stderr, tempDir, in.KeyRef)
	if err != nil {
		return err
	}

	for idx, image := range images {
		if err := signOneBaseImage(ctx, signer, verifier, sbom, archive, out, stderr, tempDir, publicKeyPath, in, image); err != nil {
			return fmt.Errorf("base images sign: entry %d: %w", idx, err)
		}
	}

	return nil
}

// validateSignBaseDependencies checks the injected adapters; the syft and
// archive adapters are only needed when no premade SBOM directory is given.
func validateSignBaseDependencies(signer baseImageSigner, verifier imageEvidenceVerifier, resolver baseImageRegistry, sbom ImageEvidenceSyft, archive baseImageArchiveCopier, premadeSBOMDir string) error {
	if signer == nil {
		return fmt.Errorf("base images sign: cosign signer is required: %w", errs.ErrUsage)
	}

	if verifier == nil {
		return fmt.Errorf("base images sign: cosign verifier is required: %w", errs.ErrUsage)
	}

	if resolver == nil {
		return fmt.Errorf("base images sign: registry resolver is required: %w", errs.ErrUsage)
	}

	if premadeSBOMDir == "" {
		if sbom == nil {
			return fmt.Errorf("base images sign: syft generator is required without --premade-sbom-dir: %w", errs.ErrUsage)
		}

		if archive == nil {
			return fmt.Errorf("base images sign: OCI archive copier is required without --premade-sbom-dir: %w", errs.ErrUsage)
		}
	}

	return nil
}

// writeBaseImageSigningPublicKey materialises the cosign public key for the
// post-sign verification step and returns its path inside tempDir.
func writeBaseImageSigningPublicKey(ctx context.Context, signer baseImageSigner, stderr io.Writer, tempDir, keyRef string) (string, error) {
	publicKeyPath := filepath.Join(tempDir, "cosign-public-key.pem")

	publicKeyFile, err := os.OpenFile(publicKeyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // path is inside the MkdirTemp directory created above.
	if err != nil {
		return "", fmt.Errorf("base images sign: create public key file: %w", err)
	}

	if err := signer.PublicKey(ctx, keyRef, publicKeyFile, stderr); err != nil {
		_ = publicKeyFile.Close()

		return "", err
	}

	if err := publicKeyFile.Close(); err != nil {
		return "", fmt.Errorf("base images sign: close public key file: %w", err)
	}

	return publicKeyPath, nil
}

func validateBaseImageSignInput(in BaseImageSignInput) error {
	if err := validateBaseImageCommon(in.ExpectedRepository, in.ExpectedSource, in.ExpectedWorkflow, in.BaseInputID, "signing key"); err != nil {
		return err
	}

	if len(in.Images) == 0 {
		return fmt.Errorf("base images sign: images-json must contain at least one image: %w", errs.ErrValidation)
	}

	if in.SourceSHA == "" || !baseImagesSourceSHARE.MatchString(in.SourceSHA) {
		return fmt.Errorf("base images sign: source-sha must be a git commit hex digest: %w", errs.ErrValidation)
	}

	if in.BuildType == "" {
		return fmt.Errorf("base images sign: build-type is required: %w", errs.ErrMissingInput)
	}

	if in.KeyRef == "" {
		return fmt.Errorf("base images sign: key reference is required: %w", errs.ErrMissingInput)
	}

	return nil
}

func normalizeSignBaseImages(ctx context.Context, resolver baseImageRegistry, in BaseImageSignInput) ([]BaseImageMetadata, error) {
	images := append([]BaseImageMetadata(nil), in.Images...)
	for idx, image := range images {
		normalized, err := normalizeSignBaseImage(ctx, resolver, image, in.BaseInputID, in.ExpectedRepository)
		if err != nil {
			return nil, fmt.Errorf("base images sign: entry %d: %w", idx, err)
		}

		images[idx] = normalized
	}

	slices.SortFunc(images, func(a, b BaseImageMetadata) int { return strings.Compare(a.Flavor, b.Flavor) })

	return images, nil
}

func normalizeSignBaseImage(ctx context.Context, resolver baseImageRegistry, image BaseImageMetadata, fallbackBaseInputID, expectedRepository string) (BaseImageMetadata, error) {
	if image.BaseInputID == "" {
		image.BaseInputID = fallbackBaseInputID
	}

	if image.Flavor == "" || image.Tag == "" || image.Ref == "" {
		return image, fmt.Errorf("image entry must include flavor, tag, and ref: %w", errs.ErrValidation)
	}

	if !baseImageFlavorRE.MatchString(image.Flavor) {
		return image, fmt.Errorf("invalid flavor name: %s: %w", image.Flavor, errs.ErrValidation)
	}

	if err := validateSignBaseImageDigests(image, fallbackBaseInputID); err != nil {
		return image, err
	}

	if err := validateBaseImageRef(image.Ref, expectedRepository); err != nil {
		return image, err
	}

	if err := verifyBaseTagDigest(ctx, resolver, image.Tag, digestFromRef(image.Ref), expectedRepository); err != nil {
		return image, err
	}

	return image, nil
}

// validateSignBaseImageDigests checks the hex-digest fields of one image
// entry against the shared base input ID.
func validateSignBaseImageDigests(image BaseImageMetadata, fallbackBaseInputID string) error {
	if !hex64RE.MatchString(image.BaseInputID) {
		return fmt.Errorf("image entry base_input_id must be a sha256 hex digest: %w", errs.ErrValidation)
	}

	if fallbackBaseInputID != "" && image.BaseInputID != fallbackBaseInputID {
		return fmt.Errorf("image entry base_input_id does not match base-input-id: %w", errs.ErrValidation)
	}

	if image.SBOMSHA256 != "" && !hex64RE.MatchString(image.SBOMSHA256) {
		return fmt.Errorf("image entry sbom_sha256 must be a sha256 hex digest: %w", errs.ErrValidation)
	}

	return nil
}

func signOneBaseImage(ctx context.Context, signer baseImageSigner, verifier imageEvidenceVerifier, sbom ImageEvidenceSyft, archive baseImageArchiveCopier, out, stderr io.Writer, tempDir, publicKeyPath string, in BaseImageSignInput, image BaseImageMetadata) error {
	digest := digestFromRef(image.Ref)
	_, _ = fmt.Fprintf(out, "Signing base image: flavor=%s digest=%s\n", image.Flavor, digest)

	if err := retryBaseImageCosign(stderr, "sign", func(errOut io.Writer) error {
		return signer.SignImage(ctx, cosign.SignImageInput{ImageRef: image.Ref, KeyRef: in.KeyRef}, errOut)
	}); err != nil {
		return err
	}

	sbomPath, err := baseImageSBOMPath(ctx, sbom, archive, stderr, tempDir, in.PremadeSBOMDir, image)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "Attesting base image SBOM: flavor=%s digest=%s\n", image.Flavor, digest)

	if attestErr := retryBaseImageCosign(stderr, "attest", func(errOut io.Writer) error {
		return signer.AttestImage(ctx, cosign.AttestImageInput{ImageRef: image.Ref, PredicateType: predicateTypeCycloneDX, PredicatePath: sbomPath, KeyRef: in.KeyRef}, errOut)
	}); attestErr != nil {
		return attestErr
	}

	lineagePath, err := writeBaseImageLineagePredicate(tempDir, in, image)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "Attesting base image lineage (SLSA provenance): flavor=%s digest=%s\n", image.Flavor, digest)

	if attestErr := retryBaseImageCosign(stderr, "attest", func(errOut io.Writer) error {
		return signer.AttestImage(ctx, cosign.AttestImageInput{ImageRef: image.Ref, PredicateType: predicateTypeSLSAProvenance1, PredicatePath: lineagePath, KeyRef: in.KeyRef}, errOut)
	}); attestErr != nil {
		return attestErr
	}

	return verifyBaseImageEvidence(ctx, verifier, out, image.Ref, publicKeyPath, baseImageLineageExpectation{
		Source: in.ExpectedSource, Workflow: in.ExpectedWorkflow, Flavor: image.Flavor, BaseInputID: image.BaseInputID,
	}, 3)
}

func baseImageSBOMPath(ctx context.Context, sbom ImageEvidenceSyft, archive baseImageArchiveCopier, stderr io.Writer, tempDir, premadeDir string, image BaseImageMetadata) (string, error) {
	sbomName := "base-sbom-" + image.Flavor + ".cyclonedx.json"
	if premadeDir != "" {
		return verifiedPremadeBaseImageSBOM(filepath.Join(premadeDir, sbomName), image)
	}

	archivePath := filepath.Join(tempDir, "base-sbom-src-"+image.Flavor+".tar")
	if err := archive.CopyDockerToOCIArchive(ctx, image.Ref, archivePath, stderr); err != nil {
		return "", err
	}

	sbomPath := filepath.Join(tempDir, sbomName)
	if err := sbom.Generate(ctx, "oci-archive:"+archivePath, map[string]string{sbomFormatCycloneDXJSON: sbomPath}, stderr); err != nil {
		return "", err
	}

	return sbomPath, nil
}

func verifiedPremadeBaseImageSBOM(path string, image BaseImageMetadata) (string, error) {
	if image.SBOMSHA256 == "" {
		return "", fmt.Errorf("%s promised a pre-built SBOM but sbom_sha256 was not provided; refusing to attest a base without its declared SBOM: %w", image.Flavor, errs.ErrValidation)
	}

	body, err := os.ReadFile(path) //nolint:gosec // caller-selected artifact directory plus validated flavor-derived filename.
	if err != nil {
		return "", fmt.Errorf("%s promised a pre-built SBOM but it was not delivered (file=%q): %w", image.Flavor, path, errs.ErrMissingInput)
	}

	sum := sha256.Sum256(body)

	actual := hex.EncodeToString(sum[:])
	if actual != image.SBOMSHA256 {
		return "", fmt.Errorf("pre-built SBOM for %s fails integrity check; refusing to attest\n  expected sha256: %s\n  actual sha256:   %s: %w", image.Flavor, image.SBOMSHA256, actual, errs.ErrValidation)
	}

	return path, nil
}

func writeBaseImageLineagePredicate(tempDir string, in BaseImageSignInput, image BaseImageMetadata) (string, error) {
	body, err := provenance.BaseLineagePredicate(provenance.BaseLineageInput{
		Source:      in.ExpectedSource,
		Commit:      in.SourceSHA,
		Workflow:    in.ExpectedWorkflow,
		Flavor:      image.Flavor,
		BaseInputID: image.BaseInputID,
		Image:       image.Ref,
		BuildType:   in.BuildType,
	})
	if err != nil {
		return "", err
	}

	path := filepath.Join(tempDir, "lineage-"+image.Flavor+".json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", fmt.Errorf("base images sign: write lineage predicate: %w", err)
	}

	return path, nil
}

func retryBaseImageCosign(stderr io.Writer, verb string, fn func(io.Writer) error) error {
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		if err = fn(stderr); err == nil {
			return nil
		}

		if attempt == 3 {
			return fmt.Errorf("cosign %s failed after 3 attempts: %w", verb, err)
		}

		_, _ = fmt.Fprintf(stderr, "cosign %s failed (attempt %d/3); retrying...\n", verb, attempt)
		time.Sleep(time.Duration(attempt*15) * time.Second)
	}

	return err
}
