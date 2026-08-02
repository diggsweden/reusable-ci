// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package baseimages

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// CleanupStagingBaseImages deletes promoted staging base tags through the forge
// package-version API, verifies final tags before and after deletion, and sweeps
// stale staging versions that are still present in the package registry.
func CleanupStagingBaseImages(ctx context.Context, registry imageDigestResolver, cleaner baseImageStagingCleaner, out io.Writer, in BaseImageCleanupStagingInput) error {
	if registry == nil {
		return fmt.Errorf("base images cleanup: registry resolver is required: %w", errs.ErrUsage)
	}

	if cleaner == nil {
		return fmt.Errorf("base images cleanup: staging tag cleaner is required: %w", errs.ErrUsage)
	}

	if strings.TrimSpace(in.ExpectedRepository) == "" {
		return fmt.Errorf("base images cleanup: expected repository is required: %w", errs.ErrUsage)
	}

	archFinalTags, err := reusableBaseArchFinalTags(in.ExpectedRepository, in.BaseInputs)
	if err != nil {
		return err
	}

	cleanupError, err := cleanupPromotedStagingImages(ctx, registry, cleaner, out, in, archFinalTags)
	if err != nil {
		return err
	}

	if err := sweepStaleStagingVersions(ctx, registry, cleaner, out, in.ExpectedRepository, archFinalTags); err != nil {
		return err
	}

	if cleanupError {
		return fmt.Errorf("base images cleanup: staging cleanup safety check failed: %w", errs.ErrValidation)
	}

	return nil
}

// cleanupPromotedStagingImages deletes each promoted image's staging tag,
// reporting (but not aborting on) failed final-tag safety checks.
func cleanupPromotedStagingImages(ctx context.Context, registry imageDigestResolver, cleaner baseImageStagingCleaner, out io.Writer, in BaseImageCleanupStagingInput, archFinalTags map[string]string) (bool, error) {
	cleanupError := false

	for idx, image := range in.Images {
		item, digest, skip, err := normalizeCleanupStagingImage(image, in.ExpectedRepository, idx)
		if err != nil {
			return false, err
		}

		if skip {
			continue
		}

		flagged, err := cleanupOnePromotedStagingImage(ctx, registry, cleaner, out, in.ExpectedRepository, archFinalTags, item, digest)
		if err != nil {
			return false, err
		}

		if flagged {
			cleanupError = true
		}
	}

	return cleanupError, nil
}

// normalizeCleanupStagingImage validates one cleanup entry and returns the
// promoted digest it must keep pointing at. skip=true means the entry has no
// staging candidate to clean up.
func normalizeCleanupStagingImage(image BaseImageMetadata, expectedRepository string, idx int) (BaseImageMetadata, string, bool, error) {
	item, err := normalizePromoteBaseImage(image, "", expectedRepository)
	if err != nil {
		return item, "", false, fmt.Errorf("base images cleanup: entry %d: %w", idx, err)
	}

	if item.CandidateTag == "" {
		return item, "", true, nil
	}

	digest := digestFromRef(item.CandidateRef)
	if digest == "" {
		digest = digestFromRef(item.Ref)
	}

	if !domaincontainer.ValidDigest(digest) {
		return item, "", false, fmt.Errorf("base images cleanup: candidate ref lacks a valid digest for %s: %w", item.Flavor, errs.ErrValidation)
	}

	if !strings.HasPrefix(item.CandidateTag, expectedRepository+":staging-"+item.BaseInputID+"-") {
		return item, "", false, fmt.Errorf("base images cleanup: refusing to delete unexpected staging tag: %s: %w", item.CandidateTag, errs.ErrValidation)
	}

	if !strings.HasPrefix(item.Tag, expectedRepository+":"+item.BaseInputID+"-") {
		return item, "", false, fmt.Errorf("base images cleanup: refusing cleanup with unexpected final tag: %s: %w", item.Tag, errs.ErrValidation)
	}

	return item, digest, false, nil
}

// cleanupOnePromotedStagingImage verifies the final tag before and after
// deleting one staging tag. It returns true when a safety check failed and
// the overall cleanup must be reported as failed.
func cleanupOnePromotedStagingImage(ctx context.Context, registry imageDigestResolver, cleaner baseImageStagingCleaner, out io.Writer, expectedRepository string, archFinalTags map[string]string, item BaseImageMetadata, digest string) (bool, error) {
	finalDigest, err := registry.ResolveDigest(ctx, item.Tag)
	if err != nil {
		_, _ = fmt.Fprintf(out, "Final base tag is absent before staging cleanup; stale sweep will remove the candidate if it still exists: %s\n", item.Tag)

		return false, nil //nolint:nilerr // absent final tag defers cleanup to the stale sweep; not a failure.
	}

	if finalDigest != digest {
		_, _ = fmt.Fprintln(out, "ERROR: final base tag does not point to the promoted digest before cleanup")
		_, _ = fmt.Fprintf(out, "  tag:      %s\n", item.Tag)
		_, _ = fmt.Fprintf(out, "  expected: %s\n", digest)
		_, _ = fmt.Fprintf(out, "  actual:   %s\n", finalDigest)

		return true, nil
	}

	stagingVersion := tagName(item.CandidateTag)
	if deleteErr := deleteStagingBaseVersion(ctx, registry, cleaner, out, expectedRepository, archFinalTags, stagingVersion, item.CandidateTag); deleteErr != nil {
		return false, deleteErr
	}

	finalDigestAfter, err := registry.ResolveDigest(ctx, item.Tag)
	if err != nil {
		_, _ = fmt.Fprintln(out, "ERROR: final base tag disappeared after staging cleanup")
		_, _ = fmt.Fprintf(out, "  tag: %s\n", item.Tag)

		return true, nil //nolint:nilerr // reported as a failed safety check (true), not an error, so other flavors still clean up.
	}

	if finalDigestAfter != digest {
		_, _ = fmt.Fprintln(out, "ERROR: final base tag changed after staging cleanup")
		_, _ = fmt.Fprintf(out, "  tag:      %s\n", item.Tag)
		_, _ = fmt.Fprintf(out, "  expected: %s\n", digest)
		_, _ = fmt.Fprintf(out, "  actual:   %s\n", finalDigestAfter)

		return true, nil
	}

	return false, nil
}

// sweepStaleStagingVersions deletes staging package versions that are still
// present in the registry after promotion.
func sweepStaleStagingVersions(ctx context.Context, registry imageDigestResolver, cleaner baseImageStagingCleaner, out io.Writer, expectedRepository string, archFinalTags map[string]string) error {
	owner, name, err := baseImagePackageOwnerName(expectedRepository)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(out, "Checking for stale staging base tags.")

	staleVersions, err := cleaner.ListContainerPackageVersions(ctx, owner, name)
	if err != nil {
		return err
	}

	slices.Sort(staleVersions)

	for _, version := range staleVersions {
		if !strings.HasPrefix(version, "staging-") {
			continue
		}

		if err := deleteStagingBaseVersion(ctx, registry, cleaner, out, expectedRepository, archFinalTags, version, expectedRepository+":"+version); err != nil {
			return err
		}
	}

	return nil
}

func reusableBaseArchFinalTags(expectedRepository string, inputs []BaseInput) (map[string]string, error) {
	finalTags := make(map[string]string, len(inputs)*2)
	for idx, input := range inputs {
		if input.Flavor == "" || !hex64RE.MatchString(input.BaseInputID) || !hex64RE.MatchString(input.ContentID) {
			return nil, fmt.Errorf("base images cleanup: base-inputs-json entry %d must include flavor, sha256 content_id, and sha256 base_input_id: %w", idx, errs.ErrValidation)
		}

		if !baseImageFlavorRE.MatchString(input.Flavor) {
			return nil, fmt.Errorf("base images cleanup: invalid flavor name in base-inputs-json: %s: %w", input.Flavor, errs.ErrValidation)
		}

		for _, arch := range []string{domaincontainer.ArchAMD64, domaincontainer.ArchARM64} {
			version := fmt.Sprintf("staging-%s-%s-%s", input.ContentID, input.Flavor, arch)
			finalTags[version] = fmt.Sprintf("%s:%s-%s", expectedRepository, input.BaseInputID, input.Flavor)
		}
	}

	return finalTags, nil
}

func deleteStagingBaseVersion(ctx context.Context, registry imageDigestResolver, cleaner baseImageStagingCleaner, out io.Writer, expectedRepository string, archFinalTags map[string]string, stagingVersion, stagingLabel string) error {
	if isBaseArchStagingVersion(stagingVersion) && !shouldDeleteArchStagingVersion(ctx, registry, out, archFinalTags, stagingVersion, stagingLabel) {
		return nil
	}

	if !validBaseStagingVersion(stagingVersion) {
		return fmt.Errorf("base images cleanup: refusing to delete unexpected staging version: %s: %w", stagingVersion, errs.ErrValidation)
	}

	stagingTag := expectedRepository + ":" + stagingVersion

	stagingDigest, _ := registry.ResolveDigest(ctx, stagingTag)
	if err := cleaner.DeleteTag(ctx, stagingTag); err != nil {
		if errors.Is(err, errs.ErrPermissionDenied) {
			_, _ = fmt.Fprintf(out, "Registry refused staging base tag deletion; preserving: %s\n", stagingLabel)

			return cleanupOrphanBaseSignatureVersions(ctx, registry, out, expectedRepository, stagingVersion, stagingDigest)
		}

		return fmt.Errorf("base images cleanup: failed to delete staging base tag: %s: %w", stagingLabel, err)
	}

	_, _ = fmt.Fprintf(out, "Deleted staging base tag: %s\n", stagingLabel)

	return cleanupOrphanBaseSignatureVersions(ctx, registry, out, expectedRepository, stagingVersion, stagingDigest)
}

// shouldDeleteArchStagingVersion decides whether a per-arch staging tag may
// be deleted: only after its expected final tag resolves, or when no final
// tag is expected for it at all (a stale leftover).
func shouldDeleteArchStagingVersion(ctx context.Context, registry imageDigestResolver, out io.Writer, archFinalTags map[string]string, stagingVersion, stagingLabel string) bool {
	finalTag := archFinalTags[stagingVersion]
	if finalTag == "" {
		_, _ = fmt.Fprintf(out, "Deleting stale architecture base tag: %s\n", stagingLabel)

		return true
	}

	if _, err := registry.ResolveDigest(ctx, finalTag); err != nil {
		_, _ = fmt.Fprintf(out, "Preserving reusable architecture base tag until final exists: %s\n", stagingLabel)

		return false
	}

	_, _ = fmt.Fprintf(out, "Deleting reusable architecture base tag after final promotion: %s\n", stagingLabel)

	return true
}

func cleanupOrphanBaseSignatureVersions(ctx context.Context, registry imageDigestResolver, out io.Writer, expectedRepository, stagingVersion, digest string) error {
	if digest == "" || isBaseArchStagingVersion(stagingVersion) {
		return nil
	}

	finalTag := expectedRepository + ":" + strings.TrimPrefix(stagingVersion, "staging-")
	if finalDigest, err := registry.ResolveDigest(ctx, finalTag); err == nil && finalDigest == digest {
		_, _ = fmt.Fprintf(out, "Preserving base signature artifacts for promoted digest: %s\n", digest)

		return nil
	}

	_, _ = fmt.Fprintf(out, "Preserving base signature artifacts because promoted final tag is not currently readable as this digest: %s\n", finalTag)

	return nil
}

func validBaseStagingVersion(version string) bool {
	if !strings.HasPrefix(version, "staging-") {
		return false
	}

	parts := strings.SplitN(strings.TrimPrefix(version, "staging-"), "-", 2)

	return len(parts) == 2 && hex64RE.MatchString(parts[0]) && baseImageFlavorRE.MatchString(parts[1])
}

func isBaseArchStagingVersion(version string) bool {
	return strings.HasSuffix(version, "-amd64") || strings.HasSuffix(version, "-arm64")
}

func tagName(ref string) string {
	slash := strings.LastIndex(ref, "/")

	colon := strings.LastIndex(ref, ":")
	if colon <= slash {
		return ""
	}

	return ref[colon+1:]
}

func baseImagePackageOwnerName(repository string) (string, string, error) {
	parts := strings.Split(repository, "/")
	if len(parts) < 3 || parts[1] == "" || parts[2] == "" || strings.Contains(repository, "@") {
		return "", "", fmt.Errorf("base images cleanup: expected repository must be <host>/<owner>/<name>: %s: %w", repository, errs.ErrUsage)
	}

	return parts[1], strings.Join(parts[2:], "/"), nil
}
