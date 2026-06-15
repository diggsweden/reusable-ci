// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger_test

import (
	"context"
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/imageledger"
)

// releasePointer is the release stage's moving pointer for the test entry's
// base (codeberg.org/itiquette/gommitlint).
const releasePointer = "codeberg.org/itiquette/gommitlint:release"

func TestRollback_DeletesStagePointerServingTheDigest(t *testing.T) {
	t.Parallel()

	e := candidateEntry()

	reg := &fakeCleanupRegistry{digests: map[string]string{
		releasePointer: goodDigest, // promoted pointer serves our digest → delete
		e.FinalTag:     goodDigest, // immutable :<version> — must NOT be touched
	}}

	if err := imageledger.Rollback(context.Background(), reg, []imageledger.Entry{e}, "v1.2.3", imageledger.Stage{}); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}

	if !slices.Equal(reg.deleted, []string{releasePointer}) {
		t.Errorf("expected only the :release pointer rolled back (never the immutable %s), got %v", e.FinalTag, reg.deleted)
	}
}

func TestRollback_NamedStageDeletesItsPointer(t *testing.T) {
	t.Parallel()

	const devPointer = "codeberg.org/itiquette/gommitlint:dev"

	e := candidateEntry()
	reg := &fakeCleanupRegistry{digests: map[string]string{devPointer: goodDigest}}

	if err := imageledger.Rollback(context.Background(), reg, []imageledger.Entry{e}, "", imageledger.Stage{Name: "dev"}); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}

	if !slices.Equal(reg.deleted, []string{devPointer}) {
		t.Errorf("expected the :dev pointer rolled back, got %v", reg.deleted)
	}
}

func TestRollback_LeavesTagsNotServingOurDigest(t *testing.T) {
	t.Parallel()

	e := candidateEntry()

	other := "sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	// the :release pointer exists but serves a DIFFERENT image — not ours.
	reg := &fakeCleanupRegistry{digests: map[string]string{releasePointer: other}}

	if err := imageledger.Rollback(context.Background(), reg, []imageledger.Entry{e}, "v1.2.3", imageledger.Stage{}); err != nil {
		t.Fatal(err)
	}

	if len(reg.deleted) != 0 {
		t.Errorf("must not delete a tag serving a different digest, got %v", reg.deleted)
	}
}

func TestRollback_SkipsAbsentAndNonPromotedEntries(t *testing.T) {
	t.Parallel()

	promoted := candidateEntry() // :release pointer absent in registry → nothing to undo
	noCandidate := validEntry()  // never promoted
	reg := &fakeCleanupRegistry{digests: map[string]string{}}

	if err := imageledger.Rollback(context.Background(), reg, []imageledger.Entry{promoted, noCandidate}, "v1.2.3", imageledger.Stage{}); err != nil {
		t.Fatal(err)
	}

	if len(reg.deleted) != 0 {
		t.Errorf("nothing to roll back, got %v", reg.deleted)
	}
}
