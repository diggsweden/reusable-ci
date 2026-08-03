// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger

import (
	"context"
	"fmt"
)

// RollbackStage undoes a stage's promotion after a failure: for every entry that
// was promoted (has a candidate_tag), it deletes that stage's pointer tag(s) —
// the same <base>:<stage> destinations promote wrote — but only when the tag
// currently serves the entry's recorded digest, so a pre-existing or
// third-party tag is never touched, and the immutable :<version> tag (applied
// once at build, never a stage destination) is never deleted. A tag that is
// absent or serves a different digest is skipped. Rather than keeping a
// separate promotions journal, it derives "what was promoted" from the ledger
// and the stage, and confirms each digest before deleting.
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

// rollbackEntry deletes each of the stage's destination pointer tags that
// still serves the entry's digest. Unresolvable or non-matching tags are left
// alone. The immutable :<version> tag is not a stage destination, so it is
// never a deletion target.
func rollbackEntry(ctx context.Context, reg CleanupRegistry, entry Entry, stage Stage) error {
	for _, dest := range stage.destinations(entry) {
		got, err := reg.ResolveDigest(ctx, dest)
		if err != nil {
			continue // absent / unreadable → nothing to undo
		}

		if got != entry.Digest {
			continue // not the image we promoted → do not touch
		}

		if err := reg.DeleteTag(ctx, dest); err != nil {
			return fmt.Errorf("rollback %s: %w", dest, err)
		}
	}

	return nil
}
