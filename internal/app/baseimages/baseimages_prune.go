// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package baseimages

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
)

// BaseImagePruneInput drives PruneBaseImages.
type BaseImagePruneInput struct {
	// ExpectedRepository is the base-image repository, e.g.
	// registry.example/owner/project-base. Every tag considered and every tag
	// deleted must sit under it.
	ExpectedRepository string

	// ReleaseImages are digest-pinned refs of the release images belonging to
	// releases that are still supported. Each one's attestation names the base
	// it was built on, and that is what keeps a base alive.
	//
	// Which releases count as supported is the consumer's policy, so it is an
	// input rather than something derived here.
	ReleaseImages []string

	// Attestation is the verification template applied to each release image;
	// ImageRef is filled in per image. Verification is not optional: an
	// unverifiable attestation aborts the pass.
	Attestation domaincontainer.AttestationVerifyRequest

	// MaxDelete bounds one pass. Zero means unbounded, which callers should
	// avoid: a retention pass that suddenly wants to delete everything is
	// more likely mistaken than right, and the bound is what turns that into
	// a failure rather than a loss.
	MaxDelete int

	// DryRun reports what would be deleted and deletes nothing.
	DryRun bool
}

// BaseImagePruneResult reports what a pass found and did.
type BaseImagePruneResult struct {
	Inventory  []string `json:"inventory"`
	Referenced []string `json:"referenced"`
	Prunable   []string `json:"prunable"`
	Deleted    []string `json:"deleted"`
	DryRun     bool     `json:"dry_run"`
}

// PruneBaseImages deletes promoted base images no supported release is built
// on.
//
// Base images are content-addressed by base_input_id, so a build never selects
// a stale one: it derives the ID from its own inputs and either finds that
// image or builds it. Retention therefore cannot break a future build, and a
// wrong deletion costs a rebuild rather than a broken release. What it can
// break is reproducing or re-verifying an existing release, which is why the
// keep-set comes from release attestations.
//
// The attestation is the source of truth because it is the only durable one:
// the release-image ledger is deliberately excluded from published release
// assets, while the SLSA predicate that records base.input_id is signed and
// pushed alongside the image.
//
// Deletion is tag-scoped, never digest-scoped, for the same reason
// CleanupStagingBaseImages is: several tags can share one manifest, and a
// digest delete would take the others with it.
func PruneBaseImages(
	ctx context.Context,
	pruner baseImagePackageAPI,
	verifier imageEvidenceVerifier,
	out io.Writer,
	in BaseImagePruneInput,
) (BaseImagePruneResult, error) {
	if pruner == nil {
		return BaseImagePruneResult{}, fmt.Errorf("base images prune: registry pruner is required: %w", errs.ErrUsage)
	}

	if verifier == nil {
		return BaseImagePruneResult{}, fmt.Errorf("base images prune: attestation verifier is required: %w", errs.ErrUsage)
	}

	if strings.TrimSpace(in.ExpectedRepository) == "" {
		return BaseImagePruneResult{}, fmt.Errorf("base images prune: expected repository is required: %w", errs.ErrUsage)
	}

	if err := requireDigestPinnedReleaseImages(in.ReleaseImages); err != nil {
		return BaseImagePruneResult{}, err
	}

	inventory, err := baseImageInventory(ctx, pruner, in.ExpectedRepository)
	if err != nil {
		return BaseImagePruneResult{}, err
	}

	referenced, err := referencedBaseInputs(ctx, verifier, out, in)
	if err != nil {
		return BaseImagePruneResult{}, err
	}

	prunable, err := imageledger.UnreferencedBaseInputs(inventory, referenced)
	if err != nil {
		return BaseImagePruneResult{}, err
	}

	result := BaseImagePruneResult{
		Inventory: inventory, Referenced: referenced, Prunable: prunable, DryRun: in.DryRun,
	}

	if err := guardPruneScope(inventory, referenced, prunable, in); err != nil {
		return result, err
	}

	reportPruneScope(out, result)

	deleted, deleteErr := deletePrunableBaseImages(ctx, pruner, out, in, prunable)
	result.Deleted = deleted

	return result, deleteErr
}

// baseImageInventory lists the promoted base images in the package: versions
// whose tag is a bare base_input_id.
//
// Staging versions are skipped rather than rejected — CleanupStagingBaseImages
// owns those, and a pass running while a build is mid-flight will legitimately
// see them. Anything else unrecognised is left alone too: this pass deletes
// only what it positively identifies as a promoted base.
func baseImageInventory(ctx context.Context, pruner baseImagePackageAPI, expectedRepository string) ([]string, error) {
	owner, name, err := baseImagePackageOwnerName(expectedRepository)
	if err != nil {
		return nil, err
	}

	versions, err := pruner.ListContainerPackageVersions(ctx, owner, name)
	if err != nil {
		return nil, fmt.Errorf("base images prune: list %s/%s versions: %w", owner, name, err)
	}

	inventory := make([]string, 0, len(versions))

	for _, version := range versions {
		trimmed := strings.TrimSpace(version)
		if domaincontainer.ValidSHA256Hex(trimmed) && !slices.Contains(inventory, trimmed) {
			inventory = append(inventory, trimmed)
		}
	}

	slices.Sort(inventory)

	return inventory, nil
}

// requireDigestPinnedReleaseImages refuses a release image that is not pinned
// to a digest.
//
// The keep-set is only as trustworthy as the refs it is derived from. A tag
// resolves to whatever it points at NOW: the engine refuses to move an
// immutable final tag to a different digest, but a retag through the registry
// itself is outside that boundary. A moved tag would send cosign to a
// different image, whose attestation names a different base -- so the pass
// would keep the wrong base and delete the one the release was actually built
// on. Nothing downstream can detect that, because a wrong-but-verifiable
// attestation looks exactly like a right one.
//
// Refused up front, before any registry call, so a mistyped input costs
// nothing and cannot half-run.
func requireDigestPinnedReleaseImages(refs []string) error {
	for _, ref := range refs {
		trimmed := strings.TrimSpace(ref)
		if trimmed == "" {
			continue
		}

		if !domaincontainer.ValidDigestPinnedRef(trimmed) {
			return fmt.Errorf(
				"base images prune: release image %q is not digest-pinned.\n"+
					"    Pass <registry>/<path>@sha256:<64 hex>, with no tag: a tag resolves to whatever it\n"+
					"    serves now, and a retagged release would keep the wrong base and prune the right one.\n"+
					"    Record the digests when the release is made rather than resolving them here: %w",
				trimmed, errs.ErrUsage)
		}
	}

	return nil
}

// referencedBaseInputs reads each supported release image's verified
// attestation and collects the base_input_id it was built on.
//
// A release image whose attestation does not verify, does not parse, or names
// no base aborts the pass. Skipping it would under-count the keep-set, and an
// under-counted keep-set deletes a base that a supported release depends on —
// the one outcome this pass must never produce.
func referencedBaseInputs(ctx context.Context, verifier imageEvidenceVerifier, out io.Writer, in BaseImagePruneInput) ([]string, error) {
	referenced := make([]string, 0, len(in.ReleaseImages))

	for _, imageRef := range in.ReleaseImages {
		ref := strings.TrimSpace(imageRef)
		if ref == "" {
			continue
		}

		request := in.Attestation
		request.ImageRef = ref

		var payload bytes.Buffer

		if err := verifier.VerifyAttestationOutput(ctx, request, &payload, out); err != nil {
			return nil, fmt.Errorf("base images prune: verify attestation for %s: %w", ref, err)
		}

		id, err := baseInputIDFromAttestation(payload.Bytes())
		if err != nil {
			return nil, fmt.Errorf("base images prune: %s: %w", ref, err)
		}

		if !slices.Contains(referenced, id) {
			referenced = append(referenced, id)
		}
	}

	slices.Sort(referenced)

	return referenced, nil
}

// baseInputIDFromAttestation reads externalParameters.base.input_id from a
// verified in-toto statement, the field enrichPredicateBaseLineage writes when
// it signs a release image.
func baseInputIDFromAttestation(payload []byte) (string, error) {
	envelopes, err := provenance.Envelopes(payload)
	if err != nil {
		return "", err
	}

	for _, envelope := range envelopes {
		encoded, ok := provenance.EnvelopePayload(envelope)
		if !ok {
			continue
		}

		statement, err := provenance.DecodeStatement(encoded)
		if err != nil {
			return "", err
		}

		base, ok := provenance.NestedMap(statement, "predicate", "buildDefinition", "externalParameters", "base")
		if !ok {
			continue
		}

		id := provenance.StatementString(base, "input_id")
		if !domaincontainer.ValidSHA256Hex(id) {
			return "", fmt.Errorf("attestation base.input_id is not a sha256 hex digest: %q: %w", id, errs.ErrValidation)
		}

		return id, nil
	}

	return "", fmt.Errorf(
		"no verified attestation names a base image (predicate.buildDefinition.externalParameters.base): %w",
		errs.ErrValidation)
}

// guardPruneScope refuses a pass whose shape suggests the keep-set is wrong
// rather than the inventory being stale.
func guardPruneScope(inventory, referenced, prunable []string, in BaseImagePruneInput) error {
	// No release named a base, yet bases exist. Either no supported releases
	// were passed or every attestation lacked lineage; deleting the whole
	// inventory on that basis is never the intended answer.
	if len(referenced) == 0 && len(inventory) > 0 {
		return fmt.Errorf(
			"base images prune: no supported release references any base image, "+
				"which would prune all %d: pass the release images whose bases must be kept: %w",
			len(inventory), errs.ErrUsage)
	}

	if in.MaxDelete > 0 && len(prunable) > in.MaxDelete {
		return fmt.Errorf(
			"base images prune: %d images are unreferenced but --max-delete is %d: "+
				"re-run with a higher bound once the list looks right: %w",
			len(prunable), in.MaxDelete, errs.ErrValidation)
	}

	return nil
}

// reportPruneScope states the arithmetic behind the decision before any tag is
// touched.
//
// A dry run that lists only what it would delete asks to be trusted. Printing
// the inventory size, how many releases were verified and what they keep lets
// an operator check the subtraction instead -- and the number worth checking is
// the keep-set, since that is the one an incomplete --release-images list
// silently shrinks.
func reportPruneScope(out io.Writer, result BaseImagePruneResult) {
	verb := "pruning"
	if result.DryRun {
		verb = "dry run"
	}

	_, _ = fmt.Fprintf(out,
		"Base image retention (%s): %d promoted in the registry, %d kept by verified release attestations, %d unreferenced.\n",
		verb, len(result.Inventory), len(result.Referenced), len(result.Prunable))

	for _, id := range result.Referenced {
		_, _ = fmt.Fprintf(out, "  keep  %s (a supported release is built on it)\n", id)
	}
}

// deletePrunableBaseImages deletes each unreferenced base tag, or reports them
// when DryRun is set.
func deletePrunableBaseImages(ctx context.Context, pruner baseImagePackageAPI, out io.Writer, in BaseImagePruneInput, prunable []string) ([]string, error) {
	if len(prunable) == 0 {
		_, _ = fmt.Fprintln(out, "No unreferenced base images to prune.")

		return nil, nil
	}

	deleted := make([]string, 0, len(prunable))

	for _, id := range prunable {
		ref := in.ExpectedRepository + ":" + id

		if in.DryRun {
			_, _ = fmt.Fprintf(out, "Would prune unreferenced base image %s\n", ref)

			continue
		}

		if err := pruner.DeleteTag(ctx, ref); err != nil {
			return deleted, fmt.Errorf("base images prune: delete %s: %w", ref, err)
		}

		deleted = append(deleted, id)

		_, _ = fmt.Fprintf(out, "Pruned unreferenced base image %s\n", ref)
	}

	return deleted, nil
}
