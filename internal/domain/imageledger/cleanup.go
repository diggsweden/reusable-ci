// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// TagDeleter removes a tag reference from a registry. Implemented by a
// registry adapter (skopeo / a forge package API); the domain depends
// only on this port so the cleanup flow stays forge-agnostic and
// fake-testable.
type TagDeleter interface {
	DeleteTag(ctx context.Context, ref string) error
}

// CleanupRegistry is what Cleanup needs: resolve a ref's digest and
// delete a tag.
type CleanupRegistry interface {
	DigestResolver
	TagDeleter
}

// Cleanup deletes each entry's staging candidate tag after a successful
// promotion. The safety contract:
//
//   - Pre-check: the promoted final_tag must already serve the recorded
//     digest. If it can't be verified, the candidate is LEFT IN PLACE
//     (deleting it would be unsafe — the promotion may not have landed).
//   - Only the candidate *tag* is removed; the digest and its signatures
//     remain, referenced by the promoted final tag.
//   - Post-check: after deletion, the promoted final_tag must still serve
//     the digest, so a botched delete that disturbs the release is caught.
//
// Entries without a candidate_tag are skipped (nothing to clean up).
func Cleanup(ctx context.Context, reg CleanupRegistry, entries []Entry, releaseTag string) error {
	for idx, entry := range entries {
		if err := entry.Validate(releaseTag); err != nil {
			return fmt.Errorf("imageledger: entry %d: %w", idx, err)
		}

		if entry.CandidateTag == "" {
			continue
		}

		if err := cleanupEntry(ctx, reg, entry); err != nil {
			return fmt.Errorf("imageledger: entry %d: %w", idx, err)
		}
	}

	return nil
}

func cleanupEntry(ctx context.Context, reg CleanupRegistry, entry Entry) error {
	candidatePresent, err := refServesDigestIfPresent(ctx, reg, entry.CandidateTag, entry.Digest)
	if err != nil {
		return fmt.Errorf("candidate tag %s: %w", entry.CandidateTag, err)
	}

	promotedReady, err := promotedTagsServeDigest(ctx, reg, entry)
	if err != nil {
		return err
	}

	if !promotedReady {
		return nil
	}

	if !candidatePresent {
		return nil
	}

	if err := reg.DeleteTag(ctx, entry.CandidateTag); err != nil {
		return fmt.Errorf("delete candidate %s: %w", entry.CandidateTag, err)
	}

	return verifyPromotedTagsAfterCleanup(ctx, reg, entry)
}

// promotedTagsServeDigest is the pre-delete safety check: the promoted
// final tag — and the moving tag, when the entry has one — must serve
// the recorded digest before the candidate may be deleted.
func promotedTagsServeDigest(ctx context.Context, reg CleanupRegistry, entry Entry) (bool, error) {
	finalReady, err := refServesDigestIfPresent(ctx, reg, entry.FinalTag, entry.Digest)
	if err != nil {
		return false, fmt.Errorf("promoted final tag %s: %w", entry.FinalTag, err)
	}

	if !finalReady {
		return false, nil
	}

	if entry.MovingTag == "" {
		return true, nil
	}

	movingReady, err := refServesDigestIfPresent(ctx, reg, entry.MovingTag, entry.Digest)
	if err != nil {
		return false, fmt.Errorf("promoted moving tag %s: %w", entry.MovingTag, err)
	}

	return movingReady, nil
}

// verifyPromotedTagsAfterCleanup is the post-delete check: deleting the
// candidate must not have disturbed the promoted final (and moving) tag.
func verifyPromotedTagsAfterCleanup(ctx context.Context, reg CleanupRegistry, entry Entry) error {
	if err := verifyRefDigest(ctx, reg, entry.FinalTag, entry.Digest); err != nil {
		return fmt.Errorf("after candidate cleanup, promoted final tag %s no longer serves %s: %w", entry.FinalTag, entry.Digest, err)
	}

	if entry.MovingTag != "" {
		if err := verifyRefDigest(ctx, reg, entry.MovingTag, entry.Digest); err != nil {
			return fmt.Errorf("after candidate cleanup, promoted moving tag %s no longer serves %s: %w", entry.MovingTag, entry.Digest, err)
		}
	}

	return nil
}

func refServesDigestIfPresent(ctx context.Context, reg DigestResolver, ref, want string) (bool, error) {
	got, err := reg.ResolveDigest(ctx, ref)
	if err != nil {
		if errors.Is(err, errs.ErrMissingInput) {
			return false, nil
		}

		return false, fmt.Errorf("resolve %s: %w", ref, err)
	}

	if got != want {
		return false, fmt.Errorf("%s resolves to %s, want %s: %w", ref, got, want, errs.ErrValidation)
	}

	return true, nil
}
