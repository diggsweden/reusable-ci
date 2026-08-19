// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
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

	if err := imageledger.RollbackStage(context.Background(), reg, []imageledger.Entry{e}, "v1.2.3", imageledger.Stage{}); err != nil {
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

	if err := imageledger.RollbackStage(context.Background(), reg, []imageledger.Entry{e}, "", imageledger.Stage{Name: "dev"}); err != nil {
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

	if err := imageledger.RollbackStage(context.Background(), reg, []imageledger.Entry{e}, "v1.2.3", imageledger.Stage{}); err != nil {
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

	if err := imageledger.RollbackStage(context.Background(), reg, []imageledger.Entry{promoted, noCandidate}, "v1.2.3", imageledger.Stage{}); err != nil {
		t.Fatal(err)
	}

	if len(reg.deleted) != 0 {
		t.Errorf("nothing to roll back, got %v", reg.deleted)
	}
}

type fakePromotionRollbackRegistry struct {
	digests map[string]string
	copied  []string
	deleted []string
}

func (r *fakePromotionRollbackRegistry) ResolveDigest(_ context.Context, ref string) (string, error) {
	dig, ok := r.digests[ref]
	if !ok {
		return "", errors.New("ref not found") //nolint:err113 // test mock error
	}

	return dig, nil
}

func (r *fakePromotionRollbackRegistry) CopyTag(_ context.Context, source, dest string) error {
	r.copied = append(r.copied, source+"->"+dest)
	r.digests[dest] = r.digests[source]

	return nil
}

func (r *fakePromotionRollbackRegistry) DeleteTag(_ context.Context, ref string) error {
	r.deleted = append(r.deleted, ref)
	delete(r.digests, ref)

	return nil
}

func TestPlanReleasePromotionRollback_RecordsPrePromotionState(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	e.MovingTag = "codeberg.org/itiquette/gommitlint:rust"
	previous := "sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	reg := &fakePromotionRollbackRegistry{digests: map[string]string{
		e.CandidateTag: goodDigest,
		e.MovingTag:    previous,
	}}

	records, err := imageledger.PlanReleasePromotionRollback(context.Background(), reg, []imageledger.Entry{e}, "v1.2.3", imageledger.Stage{Name: "release", UseEntryReleaseTags: true})
	if err != nil {
		t.Fatalf("PlanReleasePromotionRollback: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}

	record := records[0]
	if record.SourceRef != e.CandidateTag+"@"+goodDigest {
		t.Errorf("SourceRef = %q", record.SourceRef)
	}

	if record.FinalExisted {
		t.Error("FinalExisted = true, want false")
	}

	if record.PreviousMovingDigest != previous {
		t.Errorf("PreviousMovingDigest = %q", record.PreviousMovingDigest)
	}
}

func TestPlanReleasePromotionRollback_DigestRefFallbackRecordsDigestRefSource(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	reg := &fakePromotionRollbackRegistry{digests: map[string]string{e.Ref: goodDigest}}

	records, err := imageledger.PlanReleasePromotionRollback(context.Background(), reg, []imageledger.Entry{e}, "v1.2.3", imageledger.Stage{Name: "release", UseEntryReleaseTags: true, AllowDigestRefFallback: true})
	if err != nil {
		t.Fatalf("PlanReleasePromotionRollback with fallback: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}

	if records[0].SourceRef != e.Ref {
		t.Errorf("SourceRef = %q, want digest-pinned ref %q", records[0].SourceRef, e.Ref)
	}
}

func TestPlanReleasePromotionRollback_RefusesExistingFinalDifferentDigest(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	other := "sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	reg := &fakePromotionRollbackRegistry{digests: map[string]string{
		e.CandidateTag: goodDigest,
		e.FinalTag:     other,
	}}

	err := func() error {
		_, planErr := imageledger.PlanReleasePromotionRollback(context.Background(), reg, []imageledger.Entry{e}, "v1.2.3", imageledger.Stage{Name: "release", UseEntryReleaseTags: true})

		return planErr
	}()
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("existing final tag with another digest must be refused, got %v", err)
	}
}

func TestRollbackReleasePromotion_RestoresMovingAndDeletesNewFinal(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	e.MovingTag = "codeberg.org/itiquette/gommitlint:rust"
	previous := "sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	record := imageledger.PromotionRecord{
		SourceRef:            e.CandidateTag + "@" + goodDigest,
		FinalTag:             e.FinalTag,
		MovingTag:            e.MovingTag,
		CandidateTag:         e.CandidateTag,
		FinalExisted:         false,
		PreviousMovingDigest: previous,
	}
	reg := &fakePromotionRollbackRegistry{digests: map[string]string{
		e.FinalTag:  goodDigest,
		e.MovingTag: goodDigest,
		"codeberg.org/itiquette/gommitlint@" + previous: previous,
	}}

	if err := imageledger.RollbackReleasePromotion(context.Background(), reg, []imageledger.PromotionRecord{record}, "v1.2.3"); err != nil {
		t.Fatalf("RollbackReleasePromotion: %v", err)
	}

	if !slices.Equal(reg.copied, []string{"codeberg.org/itiquette/gommitlint@" + previous + "->" + e.MovingTag}) {
		t.Errorf("restored moving tag copy mismatch: %v", reg.copied)
	}

	if !slices.Equal(reg.deleted, []string{e.FinalTag}) {
		t.Errorf("expected new final tag deleted, got %v", reg.deleted)
	}

	if got := reg.digests[e.MovingTag]; got != previous {
		t.Errorf("moving tag digest after rollback = %q, want %q", got, previous)
	}
}

func TestRollbackReleasePromotion_DeletesNewMovingAndKeepsExistingFinal(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	e.MovingTag = "codeberg.org/itiquette/gommitlint:rust"
	record := imageledger.PromotionRecord{
		SourceRef:    e.CandidateTag + "@" + goodDigest,
		FinalTag:     e.FinalTag,
		MovingTag:    e.MovingTag,
		CandidateTag: e.CandidateTag,
		FinalExisted: true,
	}
	reg := &fakePromotionRollbackRegistry{digests: map[string]string{
		e.FinalTag:  goodDigest,
		e.MovingTag: goodDigest,
	}}

	if err := imageledger.RollbackReleasePromotion(context.Background(), reg, []imageledger.PromotionRecord{record}, "v1.2.3"); err != nil {
		t.Fatalf("RollbackReleasePromotion: %v", err)
	}

	if len(reg.copied) != 0 {
		t.Errorf("no restore expected for newly-created moving tag, got %v", reg.copied)
	}

	if !slices.Equal(reg.deleted, []string{e.MovingTag}) {
		t.Errorf("expected only new moving tag deleted, got %v", reg.deleted)
	}

	if _, ok := reg.digests[e.FinalTag]; !ok {
		t.Errorf("existing final tag must be kept")
	}
}

func TestRollbackReleasePromotion_RefusesFinalTagMismatchBeforeDelete(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	other := "sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	record := imageledger.PromotionRecord{
		SourceRef:    e.CandidateTag + "@" + goodDigest,
		FinalTag:     e.FinalTag,
		CandidateTag: e.CandidateTag,
		FinalExisted: false,
	}
	reg := &fakePromotionRollbackRegistry{digests: map[string]string{e.FinalTag: other}}

	err := imageledger.RollbackReleasePromotion(context.Background(), reg, []imageledger.PromotionRecord{record}, "v1.2.3")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("final tag mismatch must be refused, got %v", err)
	}

	if len(reg.deleted) != 0 {
		t.Errorf("must not delete mismatched final tag, got %v", reg.deleted)
	}
}

func TestPromotionJournal_RoundTripJSONL(t *testing.T) {
	t.Parallel()

	records := []imageledger.PromotionRecord{{
		SourceRef:    "codeberg.org/itiquette/gommitlint:staging-v1.2.3@" + goodDigest,
		FinalTag:     "codeberg.org/itiquette/gommitlint:v1.2.3",
		CandidateTag: "codeberg.org/itiquette/gommitlint:staging-v1.2.3",
	}}

	body, err := imageledger.MarshalPromotionJournal(records)
	if err != nil {
		t.Fatalf("MarshalPromotionJournal: %v", err)
	}

	got, err := imageledger.ParsePromotionJournal(body)
	if err != nil {
		t.Fatalf("ParsePromotionJournal: %v", err)
	}

	if !slices.Equal(got, records) {
		t.Errorf("round trip = %#v, want %#v", got, records)
	}
}

func TestValidatePromotionRecordRepository(t *testing.T) {
	t.Parallel()

	record := imageledger.PromotionRecord{
		SourceRef:    "codeberg.org/itiquette/gommitlint@" + goodDigest,
		FinalTag:     "codeberg.org/itiquette/gommitlint:v1.2.3",
		MovingTag:    "codeberg.org/itiquette/gommitlint:latest",
		CandidateTag: "codeberg.org/itiquette/gommitlint:staging-v1.2.3",
	}

	if err := imageledger.ValidatePromotionRecordRepository(record, "codeberg.org/itiquette/gommitlint"); err != nil {
		t.Fatalf("expected repository accepted: %v", err)
	}

	record.MovingTag = "codeberg.org/evil/gommitlint:latest"
	if err := imageledger.ValidatePromotionRecordRepository(record, "codeberg.org/itiquette/gommitlint"); !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("unexpected repository should be validation error, got %v", err)
	}
}

// TestPlanReleasePromotionRollback_JournalsEveryPromotableEntry covers
// the loop and its skip. Every other journal test passes one entry, so a
// planner that recorded only the first would satisfy them all -- and this
// journal is what a failed publish is rolled back from, so an image with
// no record keeps its release tags after the rollback runs.
//
// An entry with no candidate tag is skipped when the digest-ref fallback
// is off, because there is no source to promote from. Records must line
// up with the entries that were promotable, not with the entries.
func TestPlanReleasePromotionRollback_JournalsEveryPromotableEntry(t *testing.T) {
	t.Parallel()

	first := candidateEntry()

	second := candidateEntry()
	second.Role = "static"
	second.Ref = "codeberg.org/itiquette/gommitlint-static@" + goodDigest
	second.CandidateTag = "codeberg.org/itiquette/gommitlint-static:staging-v1.2.3"
	second.FinalTag = "codeberg.org/itiquette/gommitlint-static:v1.2.3"

	// No candidate tag, and the fallback is off, so this one is skipped.
	skipped := validEntry()
	skipped.Role = "debug"
	skipped.Ref = "codeberg.org/itiquette/gommitlint-debug@" + goodDigest
	skipped.FinalTag = "codeberg.org/itiquette/gommitlint-debug:v1.2.3"

	reg := &fakePromotionRollbackRegistry{digests: map[string]string{
		first.CandidateTag:  goodDigest,
		second.CandidateTag: goodDigest,
	}}

	records, err := imageledger.PlanReleasePromotionRollback(
		context.Background(), reg,
		[]imageledger.Entry{first, skipped, second},
		"v1.2.3",
		imageledger.Stage{Name: "release", UseEntryReleaseTags: true},
	)
	if err != nil {
		t.Fatalf("PlanReleasePromotionRollback: %v", err)
	}

	// Two records, in entry order, for the two promotable entries.
	want := []string{first.CandidateTag, second.CandidateTag}
	if len(records) != len(want) {
		t.Fatalf("records = %d, want %d: %+v", len(records), len(want), records)
	}

	for i, wantCandidate := range want {
		if records[i].CandidateTag != wantCandidate {
			t.Errorf("record %d candidate = %q, want %q", i, records[i].CandidateTag, wantCandidate)
		}
	}

	// The skipped entry must not appear under any field, or a rollback
	// would act on a promotion that never happened.
	for _, r := range records {
		if strings.Contains(r.FinalTag, "debug") || strings.Contains(r.SourceRef, "debug") {
			t.Errorf("skipped entry leaked into a record: %+v", r)
		}
	}
}
