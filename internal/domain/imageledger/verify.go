// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger

import (
	"context"
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// DigestResolver resolves a registry reference (a tag ref or a
// digest-pinned ref) to the sha256 digest the registry currently serves
// for it. Implemented by a registry adapter; the domain depends only on
// this port so the verify logic is fake-testable without a registry.
type DigestResolver interface {
	ResolveDigest(ctx context.Context, ref string) (string, error)
}

// Verify re-validates the ledger at the trust boundary and checks each
// entry's recorded digest against what the registry actually serves: it
// tries the candidate_tag (promotion source), then final_tag, then the
// digest-pinned ref, and accepts the entry as soon as one resolves to
// the recorded digest. If none do — a build job recorded a digest the
// registry doesn't serve under any claimed ref — the entry is refused
// (rerun the image-producing job). This re-verification at the trust
// boundary is what lets the build and sign stages be separate.
func Verify(ctx context.Context, resolver DigestResolver, entries []Entry, releaseTag string) error {
	for idx, entry := range entries {
		if err := entry.Validate(releaseTag); err != nil {
			return fmt.Errorf("imageledger: entry %d: %w", idx, err)
		}

		if err := verifyEntry(ctx, resolver, entry); err != nil {
			return fmt.Errorf("imageledger: entry %d: %w", idx, err)
		}
	}

	return nil
}

// verifyEntry accepts entry when any of its candidate/final/digest refs
// resolves to entry.Digest. Resolver errors on one ref don't fail the
// entry (a candidate tag may already be cleaned up); only "no ref
// matched" does. Per-ref diagnostics are collected as strings so the
// final error carries context without defining throwaway error types.
func verifyEntry(ctx context.Context, resolver DigestResolver, entry Entry) error {
	var diags []string

	for _, ref := range verifyRefs(entry) {
		got, err := resolver.ResolveDigest(ctx, ref)
		if err != nil {
			diags = append(diags, fmt.Sprintf("resolve %s: %v", ref, err))

			continue
		}

		if got == entry.Digest {
			return nil
		}

		diags = append(diags, fmt.Sprintf("%s resolves to %s", ref, got))
	}

	return fmt.Errorf("no ref resolves to %s (%s): %w", entry.Digest, strings.Join(diags, "; "), errs.ErrValidation)
}

// verifyRefs is the ordered set of refs to probe for an entry's digest:
// the promotion-source candidate first, then the final tag, then the
// digest-pinned ref. Empty optional refs are skipped.
func verifyRefs(entry Entry) []string {
	refs := make([]string, 0, 3)
	if entry.CandidateTag != "" {
		refs = append(refs, entry.CandidateTag)
	}

	refs = append(refs, entry.FinalTag, entry.Ref)

	return refs
}
