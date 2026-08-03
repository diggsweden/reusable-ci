// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger

import (
	"context"
	"fmt"
)

// RollbackStage undoes a stage's promotion after a failure: for every entry that
// was promoted (has a candidate_tag), it deletes that stage's destination
// tag(s) — the same destinations promote wrote — but only when the tag
// currently serves the entry's recorded digest, so a pre-existing or
// third-party tag is never touched. A tag that is absent or serves a
// different digest is skipped. Rather than keeping a separate promotions
// journal, it derives "what was promoted" from the ledger and the stage,
// and confirms each digest before deleting.
//
// The build-time immutable :<version> tag in the SOURCE repository is never a
// stage destination, so it is never deleted here. A cross-registry release's
// COPY of that tag on the target registry IS a destination, and rolling it
// back (when it still serves the recorded digest) undoes the copy.
func RollbackStage(ctx context.Context, reg CleanupRegistry, entries []Entry, releaseTag string, stage Stage) error {
	if err := stage.Validate(); err != nil {
		return err
	}

	for idx, entry := range entries {
		if err := entry.ValidateForStage(stage, releaseTag); err != nil {
			return fmt.Errorf("imageledger: entry %d: %w", idx, err)
		}

		if entry.CandidateTag == "" {
			continue // never promoted via a candidate → nothing to roll back
		}

		if err := rollbackEntry(ctx, reg, entry, stage); err != nil {
			return fmt.Errorf("imageledger: entry %d: %w", idx, err)
		}
	}

	return nil
}

// rollbackEntry deletes each of the stage's destination tags that still
// serves the entry's digest. Unresolvable or non-matching tags are left
// alone.
func rollbackEntry(ctx context.Context, reg CleanupRegistry, entry Entry, stage Stage) error {
	for _, dest := range stage.destinations(entry) {
		got, err := reg.ResolveDigest(ctx, dest.Ref)
		if err != nil {
			continue // absent / unreadable → nothing to undo
		}

		if got != entry.Digest {
			continue // not the image we promoted → do not touch
		}

		if err := reg.DeleteTag(ctx, dest.Ref); err != nil {
			return fmt.Errorf("rollback %s: %w", dest.Ref, err)
		}
	}

	return nil
}
