// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger

import (
	"context"
	"fmt"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// Registry is the set of registry operations the promotion flow needs:
// resolve a ref's digest (DigestResolver) and copy/retag one ref to
// another. The domain depends only on this port so promotion is
// fake-testable without a registry.
type Registry interface {
	DigestResolver

	// CopyTag retags the image at source to dest (e.g. promoting a
	// staging candidate to the immutable release tag), preserving the
	// digest. Use for same-repository promotions, where the registry-
	// attached signature is digest-addressed and already shared.
	CopyTag(ctx context.Context, source, dest string) error
}

// SignatureCopier copies an image together with its registry-attached
// signatures and attestations from source to dest, preserving the digest.
// It is required only for cross-repository / cross-registry promotion: a
// same-repo moving tag shares the digest-addressed signature, but a
// different repo or registry does not, so the signature must travel with
// the image (cosign copy). A nil copier means cross-repo promotion is not
// configured and must be refused rather than silently dropping the sig.
type SignatureCopier interface {
	CopyWithSignatures(ctx context.Context, source, dest string) error
}

// Promote retags each entry's candidate image to the release stage's moving
// pointer (<base>:release) on the same digest, verifying — after every copy —
// that the destination resolves to the recorded digest. The immutable
// :<version> tag was applied at build and is not touched here. Entries without
// a candidate_tag are skipped. A post-copy digest mismatch fails loudly, so a
// promotion that lands the wrong image cannot pass silently.
//
// Promote is the terminal (release) promotion; PromoteToStage generalises it
// to any stage in the dev → staging → release graph.
func Promote(ctx context.Context, reg Registry, entries []Entry, releaseTag string) error {
	// Same-registry release promotion shares a base with the candidate, so no
	// signature copier is needed.
	return PromoteToStage(ctx, reg, nil, entries, releaseTag, Stage{})
}

// PromoteToStage retags each entry's candidate image to the destination
// tag(s) of the given stage, with the same digest-verification trust
// boundary as Promote: the candidate must serve the recorded digest before
// any copy, and every destination is re-verified after the copy. Every stage
// targets its <base>:<stage> moving pointer on the same digest; a
// cross-registry release additionally carries the immutable :<version> tag to
// the target. Entries without a candidate_tag are skipped.
//
// Same-repository destinations use reg.CopyTag (the shared digest carries
// the signature). A cross-repository / cross-registry destination needs
// sigCopier to carry the registry-attached signature; when sigCopier is nil
// such a promotion is refused rather than silently dropping the signature.
func PromoteToStage(ctx context.Context, reg Registry, sigCopier SignatureCopier, entries []Entry, releaseTag string, stage Stage) error {
	if err := stage.Validate(); err != nil {
		return err
	}

	for idx, entry := range entries {
		if err := entry.ValidateForStage(stage, releaseTag); err != nil {
			return fmt.Errorf("imageledger: entry %d: %w", idx, err)
		}

		if entry.CandidateTag == "" {
			continue
		}

		if err := promoteEntry(ctx, reg, sigCopier, entry, stage); err != nil {
			return fmt.Errorf("imageledger: entry %d: %w", idx, err)
		}
	}

	return nil
}

// promoteEntry verifies the candidate serves the recorded digest, then
// copies it to each of the stage's destination tags, re-verifying the
// digest after each copy. The release stage reproduces the historical
// final+moving promotion; a named stage targets the stage's moving tag.
// Same-repo destinations use CopyTag; cross-repo destinations use the
// signature copier (required) so the registry-attached signature travels.
func promoteEntry(ctx context.Context, reg Registry, sigCopier SignatureCopier, entry Entry, stage Stage) error {
	if err := verifyRefDigest(ctx, reg, entry.CandidateTag, entry.Digest); err != nil {
		return fmt.Errorf("candidate %s: %w", entry.CandidateTag, err)
	}

	for _, dest := range stage.destinations(entry) {
		if err := copyToDest(ctx, reg, sigCopier, entry.CandidateTag, dest); err != nil {
			return err
		}

		if err := verifyRefDigest(ctx, reg, dest, entry.Digest); err != nil {
			return fmt.Errorf("after promote, %s: %w", dest, err)
		}
	}

	return nil
}

// copyToDest routes a single promotion copy: a same-repository move uses
// CopyTag (the shared digest already carries the signature); a cross-
// repository / cross-registry move uses the signature copier so the
// registry-attached signature travels with the image. A cross-repo move
// without a configured copier is refused rather than silently dropping the
// signature.
func copyToDest(ctx context.Context, reg Registry, sigCopier SignatureCopier, source, dest string) error {
	if container.StripTag(source) == container.StripTag(dest) {
		if err := reg.CopyTag(ctx, source, dest); err != nil {
			return fmt.Errorf("copy %s -> %s: %w", source, dest, err)
		}

		return nil
	}

	if sigCopier == nil {
		return fmt.Errorf(
			"cross-repository promotion %s -> %s requires signature portability (cosign copy); none configured: %w",
			source, dest, errs.ErrUsage,
		)
	}

	if err := sigCopier.CopyWithSignatures(ctx, source, dest); err != nil {
		return fmt.Errorf("copy-with-signatures %s -> %s: %w", source, dest, err)
	}

	return nil
}

// verifyRefDigest resolves ref and confirms it serves want, returning a
// validation error on mismatch.
func verifyRefDigest(ctx context.Context, reg DigestResolver, ref, want string) error {
	got, err := reg.ResolveDigest(ctx, ref)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", ref, err)
	}

	if got != want {
		return fmt.Errorf("%s resolves to %s, want %s: %w", ref, got, want, errs.ErrValidation)
	}

	return nil
}
