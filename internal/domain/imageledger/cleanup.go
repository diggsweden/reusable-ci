// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger

import (
	"context"
	"fmt"
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
	// Pre-check: refuse to delete the candidate unless the promoted final
	// tag already serves the digest — leave the candidate as a safety net.
	if err := verifyRefDigest(ctx, reg, entry.FinalTag, entry.Digest); err != nil {
		return fmt.Errorf("promoted final tag %s unverified; leaving candidate %s in place: %w", entry.FinalTag, entry.CandidateTag, err)
	}

	if err := reg.DeleteTag(ctx, entry.CandidateTag); err != nil {
		return fmt.Errorf("delete candidate %s: %w", entry.CandidateTag, err)
	}

	// Post-check: deletion must not have disturbed the promoted tag.
	if err := verifyRefDigest(ctx, reg, entry.FinalTag, entry.Digest); err != nil {
		return fmt.Errorf("after candidate cleanup, promoted final tag %s no longer serves %s: %w", entry.FinalTag, entry.Digest, err)
	}

	return nil
}
