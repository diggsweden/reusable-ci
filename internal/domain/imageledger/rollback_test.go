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

type fakePromotionRollbackRegistry struct {
	digests         map[string]string
	resolveErr      map[string]error
	resolveSequence map[string][]resolveResult
	copied          []string
	deleted         []string
	events          []string
}

type resolveResult struct {
	digest string
	err    error
}

func (r *fakePromotionRollbackRegistry) ResolveDigest(_ context.Context, ref string) (string, error) {
	r.events = append(r.events, "resolve:"+ref)

	if sequence := r.resolveSequence[ref]; len(sequence) > 0 {
		result := sequence[0]
		r.resolveSequence[ref] = sequence[1:]

		return result.digest, result.err
	}

	if err := r.resolveErr[ref]; err != nil {
		return "", err
	}

	dig, ok := r.digests[ref]
	if !ok {
		return "", errs.ErrMissingInput
	}

	return dig, nil
}

func (r *fakePromotionRollbackRegistry) CopyTag(_ context.Context, source, dest string) error {
	r.copied = append(r.copied, source+"->"+dest)
	r.events = append(r.events, "copy:"+source+"->"+dest)
	r.digests[dest] = r.digests[source]

	return nil
}

func (r *fakePromotionRollbackRegistry) DeleteTag(_ context.Context, ref string) error {
	r.deleted = append(r.deleted, ref)
	r.events = append(r.events, "delete:"+ref)
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

	_, err := imageledger.PlanReleasePromotionRollback(context.Background(), reg, []imageledger.Entry{e}, "v1.2.3", imageledger.Stage{Name: "release", UseEntryReleaseTags: true})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("existing final tag with another digest must be refused, got %v", err)
	}
}

func TestPlanReleasePromotionRollback_StopsOnUncertainDestinationLookup(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"final", "moving"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			e := candidateEntry()
			e.MovingTag = "codeberg.org/itiquette/gommitlint:rust"

			ref := e.FinalTag
			if field == "moving" {
				ref = e.MovingTag
			}

			reg := &fakePromotionRollbackRegistry{
				digests:    map[string]string{e.CandidateTag: goodDigest},
				resolveErr: map[string]error{ref: errs.ErrDependencyUnavailable},
			}

			_, err := imageledger.PlanReleasePromotionRollback(context.Background(), reg, []imageledger.Entry{e}, "v1.2.3", imageledger.Stage{Name: "release", UseEntryReleaseTags: true})
			if !errors.Is(err, errs.ErrDependencyUnavailable) {
				t.Fatalf("uncertain %s lookup must stop journal planning, got %v", field, err)
			}
		})
	}
}

func TestRollbackReleasePromotion_RestoresMovingAndDeletesNewFinal(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	e.MovingTag = "codeberg.org/itiquette/gommitlint:rust"
	previous := "sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	record := imageledger.PromotionRecord{
		Version:              imageledger.PromotionJournalVersion,
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
		Version:      imageledger.PromotionJournalVersion,
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
		Version:      imageledger.PromotionJournalVersion,
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

func TestRollbackReleasePromotion_StopsOnUncertainDestinationLookup(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"final", "moving"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			e := candidateEntry()
			e.MovingTag = "codeberg.org/itiquette/gommitlint:rust"
			record := imageledger.PromotionRecord{
				Version:      imageledger.PromotionJournalVersion,
				SourceRef:    e.CandidateTag + "@" + goodDigest,
				FinalTag:     e.FinalTag,
				MovingTag:    e.MovingTag,
				CandidateTag: e.CandidateTag,
			}

			ref := e.FinalTag
			if field == "moving" {
				ref = e.MovingTag
			}

			reg := &fakePromotionRollbackRegistry{
				digests: map[string]string{
					e.FinalTag:  goodDigest,
					e.MovingTag: goodDigest,
				},
				resolveErr: map[string]error{ref: errs.ErrDependencyUnavailable},
			}

			err := imageledger.RollbackReleasePromotion(context.Background(), reg, []imageledger.PromotionRecord{record}, "v1.2.3")
			if !errors.Is(err, errs.ErrDependencyUnavailable) {
				t.Fatalf("uncertain %s lookup must stop rollback, got %v", field, err)
			}

			if len(reg.copied) != 0 || len(reg.deleted) != 0 {
				t.Fatalf("rollback mutated registry after uncertain %s lookup: copied=%v deleted=%v", field, reg.copied, reg.deleted)
			}
		})
	}
}

func TestRollbackReleasePromotion_ValidatesWholeJournalBeforeMutation(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	valid := imageledger.PromotionRecord{
		Version:      imageledger.PromotionJournalVersion,
		SourceRef:    e.CandidateTag + "@" + goodDigest,
		FinalTag:     e.FinalTag,
		CandidateTag: e.CandidateTag,
	}
	invalid := valid
	invalid.SourceRef = "not-a-digest-pinned-ref"

	reg := &fakePromotionRollbackRegistry{digests: map[string]string{e.FinalTag: goodDigest}}

	err := imageledger.RollbackReleasePromotion(context.Background(), reg, []imageledger.PromotionRecord{valid, invalid}, "v1.2.3")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("invalid later record must fail validation, got %v", err)
	}

	if len(reg.copied) != 0 || len(reg.deleted) != 0 {
		t.Fatalf("journal must be fully valid before mutation: copied=%v deleted=%v", reg.copied, reg.deleted)
	}
}

func TestRollbackReleasePromotion_RejectsLegacyJournalBeforeMutation(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	legacy := imageledger.PromotionRecord{
		SourceRef:    e.CandidateTag + "@" + goodDigest,
		FinalTag:     e.FinalTag,
		CandidateTag: e.CandidateTag,
	}
	reg := &fakePromotionRollbackRegistry{digests: map[string]string{e.FinalTag: goodDigest}}

	err := imageledger.RollbackReleasePromotion(context.Background(), reg, []imageledger.PromotionRecord{legacy}, "v1.2.3")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("legacy unversioned journal must be refused, got %v", err)
	}

	if len(reg.copied) != 0 || len(reg.deleted) != 0 {
		t.Fatalf("legacy journal mutated registry: copied=%v deleted=%v", reg.copied, reg.deleted)
	}
}

func TestRollbackReleasePromotion_RechecksTargetImmediatelyBeforeDelete(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	other := "sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	record := imageledger.PromotionRecord{
		Version:      imageledger.PromotionJournalVersion,
		SourceRef:    e.CandidateTag + "@" + goodDigest,
		FinalTag:     e.FinalTag,
		CandidateTag: e.CandidateTag,
	}
	reg := &fakePromotionRollbackRegistry{
		digests: map[string]string{},
		resolveSequence: map[string][]resolveResult{
			e.FinalTag: {{digest: goodDigest}, {digest: other}},
		},
	}

	err := imageledger.RollbackReleasePromotion(context.Background(), reg, []imageledger.PromotionRecord{record}, "v1.2.3")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("changed final tag must be refused, got %v", err)
	}

	if len(reg.deleted) != 0 {
		t.Fatalf("changed final tag was deleted: %v", reg.deleted)
	}
}

func TestRollbackReleasePromotion_PreflightsEveryRestoreSourceBeforeMutation(t *testing.T) {
	t.Parallel()

	first := candidateEntry()
	second := candidateEntry()
	second.Ref = "codeberg.org/itiquette/gommitlint-static@" + goodDigest
	second.CandidateTag = "codeberg.org/itiquette/gommitlint-static:staging-v1.2.3"
	second.FinalTag = "codeberg.org/itiquette/gommitlint-static:v1.2.3"
	second.MovingTag = "codeberg.org/itiquette/gommitlint-static:stable"
	previous := "sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	records := []imageledger.PromotionRecord{
		{
			Version:      imageledger.PromotionJournalVersion,
			SourceRef:    first.CandidateTag + "@" + goodDigest,
			FinalTag:     first.FinalTag,
			CandidateTag: first.CandidateTag,
		},
		{
			Version:              imageledger.PromotionJournalVersion,
			SourceRef:            second.CandidateTag + "@" + goodDigest,
			FinalTag:             second.FinalTag,
			MovingTag:            second.MovingTag,
			CandidateTag:         second.CandidateTag,
			FinalExisted:         true,
			PreviousMovingDigest: previous,
		},
	}
	reg := &fakePromotionRollbackRegistry{digests: map[string]string{
		first.FinalTag:   goodDigest,
		second.FinalTag:  goodDigest,
		second.MovingTag: goodDigest,
	}}

	err := imageledger.RollbackReleasePromotion(context.Background(), reg, records, "v1.2.3")
	if !errors.Is(err, errs.ErrMissingInput) {
		t.Fatalf("missing later restore source must stop rollback, got %v", err)
	}

	if len(reg.copied) != 0 || len(reg.deleted) != 0 {
		t.Fatalf("rollback mutated before all restore sources were available: copied=%v deleted=%v", reg.copied, reg.deleted)
	}
}

func TestRollbackReleasePromotion_RechecksMovingTargetLast(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	e.MovingTag = "codeberg.org/itiquette/gommitlint:stable"
	previous := "sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	source := "codeberg.org/itiquette/gommitlint@" + previous
	record := imageledger.PromotionRecord{
		Version:              imageledger.PromotionJournalVersion,
		SourceRef:            e.CandidateTag + "@" + goodDigest,
		FinalTag:             e.FinalTag,
		MovingTag:            e.MovingTag,
		CandidateTag:         e.CandidateTag,
		FinalExisted:         true,
		PreviousMovingDigest: previous,
	}
	reg := &fakePromotionRollbackRegistry{digests: map[string]string{
		e.FinalTag:  goodDigest,
		e.MovingTag: goodDigest,
		source:      previous,
	}}

	if err := imageledger.RollbackReleasePromotion(context.Background(), reg, []imageledger.PromotionRecord{record}, "v1.2.3"); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	wantTail := []string{
		"resolve:" + source,
		"resolve:" + e.MovingTag,
		"copy:" + source + "->" + e.MovingTag,
		"resolve:" + e.MovingTag,
	}
	if len(reg.events) < len(wantTail) || !slices.Equal(reg.events[len(reg.events)-len(wantTail):], wantTail) {
		t.Fatalf("restore source must be checked before the final target check, copy, and post-copy verification\nevents: %v\nwant tail: %v", reg.events, wantTail)
	}
}

func TestRollbackReleasePromotion_RejectsOverlappingTargetsBeforeMutation(t *testing.T) {
	t.Parallel()

	first := candidateEntry()
	first.MovingTag = "codeberg.org/itiquette/gommitlint:stable"
	second := candidateEntry()
	second.Ref = "codeberg.org/itiquette/gommitlint@" + goodDigest
	second.CandidateTag = "codeberg.org/itiquette/gommitlint:staging-v1.2.3-static"
	second.FinalTag = "codeberg.org/itiquette/gommitlint:v1.2.3-static"
	second.MovingTag = first.MovingTag

	records := []imageledger.PromotionRecord{
		{
			Version:      imageledger.PromotionJournalVersion,
			SourceRef:    first.CandidateTag + "@" + goodDigest,
			FinalTag:     first.FinalTag,
			MovingTag:    first.MovingTag,
			CandidateTag: first.CandidateTag,
		},
		{
			Version:      imageledger.PromotionJournalVersion,
			SourceRef:    second.CandidateTag + "@" + goodDigest,
			FinalTag:     second.FinalTag,
			MovingTag:    second.MovingTag,
			CandidateTag: second.CandidateTag,
		},
	}
	reg := &fakePromotionRollbackRegistry{digests: map[string]string{first.MovingTag: goodDigest}}

	err := imageledger.RollbackReleasePromotion(context.Background(), reg, records, "v1.2.3")
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("overlapping targets must be refused, got %v", err)
	}

	if len(reg.events) != 0 {
		t.Fatalf("overlapping journal reached the registry: %v", reg.events)
	}
}

func TestPlanReleasePromotionRollback_RejectsOverlappingTargets(t *testing.T) {
	t.Parallel()

	first := candidateEntry()
	first.MovingTag = "codeberg.org/itiquette/gommitlint:stable"
	second := candidateEntry()
	second.Ref = "codeberg.org/itiquette/gommitlint@" + goodDigest
	second.CandidateTag = "codeberg.org/itiquette/gommitlint:staging-v1.2.3-static"
	second.FinalTag = "codeberg.org/itiquette/gommitlint:v1.2.3-static"
	second.MovingTag = first.MovingTag
	reg := &fakePromotionRollbackRegistry{digests: map[string]string{
		first.CandidateTag:  goodDigest,
		second.CandidateTag: goodDigest,
	}}

	_, err := imageledger.PlanReleasePromotionRollback(
		context.Background(),
		reg,
		[]imageledger.Entry{first, second},
		"v1.2.3",
		imageledger.Stage{Name: "release", UseEntryReleaseTags: true},
	)
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("overlapping targets must be refused before promotion, got %v", err)
	}
}

func TestPromotionJournal_RoundTripJSONL(t *testing.T) {
	t.Parallel()

	records := []imageledger.PromotionRecord{{
		Version:      imageledger.PromotionJournalVersion,
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

func TestValidatePromotionRecordRepository_RejectsRecordsOutsideTheRepository(t *testing.T) {
	t.Parallel()

	record := imageledger.PromotionRecord{
		Version:      imageledger.PromotionJournalVersion,
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

// TestValidatePromotionJournalIntent_RefusesAJournalThatDoesNotDescribeThisRun
// pins the reuse check a retried promotion relies on. A journal that names
// other tags, another digest, another source or more entries than the ledger
// is not this promotion's pre-state, and restoring from it would move the
// wrong tags.
func TestValidatePromotionJournalIntent_RefusesAJournalThatDoesNotDescribeThisRun(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	e.MovingTag = "codeberg.org/itiquette/gommitlint:rust"
	stage := imageledger.Stage{Name: "release", UseEntryReleaseTags: true}
	record := imageledger.PromotionRecord{
		Version:      imageledger.PromotionJournalVersion,
		SourceRef:    e.CandidateTag + "@" + goodDigest,
		FinalTag:     e.FinalTag,
		MovingTag:    e.MovingTag,
		CandidateTag: e.CandidateTag,
	}

	if err := imageledger.ValidatePromotionJournalIntent([]imageledger.PromotionRecord{record}, []imageledger.Entry{e}, "v1.2.3", stage); err != nil {
		t.Fatalf("the matching journal was refused: %v", err)
	}

	for name, tc := range map[string]struct {
		mutate func(*imageledger.PromotionRecord)
		extra  bool
		want   string
	}{
		"extra record":       {extra: true, want: "unexpected entr"},
		"moving tag differs": {mutate: func(r *imageledger.PromotionRecord) { r.MovingTag = "codeberg.org/itiquette/gommitlint:stable" }, want: "tags differ"},
		"digest differs":     {mutate: func(r *imageledger.PromotionRecord) { r.SourceRef = e.CandidateTag + "@" + otherDigest }, want: "source differs"},
		"source is the digest ref without fallback": {mutate: func(r *imageledger.PromotionRecord) { r.SourceRef = e.Ref }, want: "source differs"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			records := []imageledger.PromotionRecord{record}
			if tc.mutate != nil {
				tc.mutate(&records[0])
			}

			if tc.extra {
				records = append(records, record)
			}

			err := imageledger.ValidatePromotionJournalIntent(records, []imageledger.Entry{e}, "v1.2.3", stage)
			if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want ErrValidation naming %q", err, tc.want)
			}
		})
	}
}

// TestPlanReleasePromotionRollback_RecordsAPreexistingFinalTag pins the fact
// rollback needs most: a final tag that already served the digest before the
// promotion is recorded as existing, so rollback keeps it.
func TestPlanReleasePromotionRollback_RecordsAPreexistingFinalTag(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	reg := &fakePromotionRollbackRegistry{digests: map[string]string{e.CandidateTag: goodDigest, e.FinalTag: goodDigest}}

	records, err := imageledger.PlanReleasePromotionRollback(context.Background(), reg, []imageledger.Entry{e}, "v1.2.3", imageledger.Stage{Name: "release", UseEntryReleaseTags: true})
	if err != nil || len(records) != 1 || !records[0].FinalExisted {
		t.Fatalf("records = %+v, %v; want one record with FinalExisted", records, err)
	}

	if err := imageledger.RollbackReleasePromotion(context.Background(), reg, records, "v1.2.3"); err != nil {
		t.Fatal(err)
	}

	if len(reg.deleted) != 0 {
		t.Errorf("rollback deleted %v; a final tag that existed before the promotion must be kept", reg.deleted)
	}
}

// TestParsePromotionJournal_RefusesUnknownAndRepeatedMembers pins strict
// decoding of the journal rollback acts on. A misspelt final_existed used to
// decode as false, and rollback then deleted a final tag the promotion had
// not created; a repeated member kept its last value unseen.
func TestParsePromotionJournal_RefusesUnknownAndRepeatedMembers(t *testing.T) {
	t.Parallel()

	e := candidateEntry()
	prefix := `{"version":1,"source_ref":"` + e.CandidateTag + `@` + goodDigest + `","final_tag":"` + e.FinalTag + `","candidate_tag":"` + e.CandidateTag + `"`

	for name, line := range map[string]string{
		"misspelt member": prefix + `,"finalExisted":true}`,
		"repeated member": prefix + `,"final_existed":true,"final_existed":false}`,
	} {
		records, err := imageledger.ParsePromotionJournal([]byte(line + "\n"))
		if !errors.Is(err, errs.ErrMalformedInput) || records != nil {
			t.Errorf("%s: ParsePromotionJournal = %+v, %v; want ErrMalformedInput and no records", name, records, err)
		}
	}

	records, err := imageledger.ParsePromotionJournal([]byte(prefix + `,"final_existed":true}` + "\n"))
	if err != nil || len(records) != 1 || !records[0].FinalExisted {
		t.Fatalf("the well-formed line: %+v, %v", records, err)
	}
}
