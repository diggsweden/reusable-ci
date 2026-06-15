// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/imageledger"
)

// fakeCleanupRegistry resolves digests and records/applies deletes.
type fakeCleanupRegistry struct {
	digests map[string]string
	deleted []string
	delErr  error
}

func (r *fakeCleanupRegistry) ResolveDigest(_ context.Context, ref string) (string, error) {
	d, ok := r.digests[ref]
	if !ok {
		return "", errors.New("ref not found") //nolint:err113 // test mock error
	}

	return d, nil
}

func (r *fakeCleanupRegistry) DeleteTag(_ context.Context, ref string) error {
	if r.delErr != nil {
		return r.delErr
	}

	r.deleted = append(r.deleted, ref)
	delete(r.digests, ref)

	return nil
}

func TestCleanup_DeletesCandidateWhenFinalVerified(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	reg := &fakeCleanupRegistry{digests: map[string]string{
		e.FinalTag:     goodDigest, // promoted final serves the digest
		e.CandidateTag: goodDigest,
	}}

	if err := imageledger.Cleanup(context.Background(), reg, []imageledger.Entry{e}, "v1.2.3"); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}

	if !slices.Equal(reg.deleted, []string{e.CandidateTag}) {
		t.Errorf("expected candidate deleted, got %v", reg.deleted)
	}
}

func TestCleanup_LeavesCandidateWhenFinalUnverified(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	// Final tag does NOT serve the digest (promotion not confirmed).
	reg := &fakeCleanupRegistry{digests: map[string]string{e.CandidateTag: goodDigest}}

	if err := imageledger.Cleanup(context.Background(), reg, []imageledger.Entry{e}, "v1.2.3"); err == nil {
		t.Fatal("cleanup should refuse when the promoted final tag is unverified")
	}

	if len(reg.deleted) != 0 {
		t.Errorf("candidate must be left in place when final unverified, got deleted=%v", reg.deleted)
	}
}

func TestCleanup_SkipsEntryWithoutCandidate(t *testing.T) {
	t.Parallel()

	e := validEntry() // no candidate
	reg := &fakeCleanupRegistry{digests: map[string]string{}}

	if err := imageledger.Cleanup(context.Background(), reg, []imageledger.Entry{e}, "v1.2.3"); err != nil {
		t.Fatalf("entry without candidate should be skipped, got %v", err)
	}

	if len(reg.deleted) != 0 {
		t.Errorf("nothing should be deleted, got %v", reg.deleted)
	}
}
