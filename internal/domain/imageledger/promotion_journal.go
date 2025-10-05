// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// PromotionRecord is one rollback-journal line for release-tag promotion. It
// records the pre-promotion registry state that cannot be reconstructed from the
// release image ledger after a failed publish: whether the immutable final tag
// already existed, and what digest the optional moving tag pointed at before the
// release moved it.
type PromotionRecord struct {
	Version              int    `json:"version"`
	SourceRef            string `json:"source_ref"`
	FinalTag             string `json:"final_tag"`
	MovingTag            string `json:"moving_tag,omitempty"`
	CandidateTag         string `json:"candidate_tag,omitempty"`
	FinalExisted         bool   `json:"final_existed"`
	PreviousMovingDigest string `json:"previous_moving_digest,omitempty"`
}

// PromotionJournalVersion rejects journals written before fail-closed
// destination reads and retry-safe preservation. Those legacy records used the
// same fields but could encode an unavailable tag as absent.
const PromotionJournalVersion = 1

// PromotionRollbackRegistry is the registry surface needed to undo a journaled
// release promotion: resolve tags, delete newly-created tags, and restore a
// previous moving tag by copying its old digest back.
type PromotionRollbackRegistry interface {
	CleanupRegistry
	CopyTag(ctx context.Context, source, dest string) error
}

// PlanReleasePromotionRollback validates release-tag promotion and captures the
// rollback state before any tag is moved. It only supports the release stage
// using ledger-carried release tags, the sign-before-publish contract where the
// immutable final tag and its previous moving-tag digest cannot be recovered
// from the ledger alone once the promotion has run.
func PlanReleasePromotionRollback(ctx context.Context, reg DigestResolver, entries []Entry, releaseTag string, stage Stage) ([]PromotionRecord, error) {
	if !stage.IsRelease() || !stage.UseEntryReleaseTags {
		return nil, fmt.Errorf("imageledger: promotion journal requires release stage with ledger release tags: %w", errs.ErrUsage)
	}

	if err := stage.Validate(); err != nil {
		return nil, err
	}

	records := make([]PromotionRecord, 0, len(entries))

	for idx, entry := range entries {
		if err := entry.ValidateForStage(stage, releaseTag); err != nil {
			return nil, fmt.Errorf("imageledger: entry %d: %w", idx, err)
		}

		if entry.CandidateTag == "" && !stage.AllowDigestRefFallback {
			continue
		}

		record, err := planPromotionRecord(ctx, reg, entry, stage)
		if err != nil {
			return nil, fmt.Errorf("imageledger: entry %d: %w", idx, err)
		}

		records = append(records, record)
	}

	if err := validateUniquePromotionTargets(records); err != nil {
		return nil, err
	}

	return records, nil
}

func planPromotionRecord(ctx context.Context, reg DigestResolver, entry Entry, stage Stage) (PromotionRecord, error) {
	source, err := promotionSource(ctx, reg, entry, stage.AllowDigestRefFallback)
	if err != nil {
		return PromotionRecord{}, err
	}

	finalTag, movingTag, err := releaseDestinationTags(stage, entry)
	if err != nil {
		return PromotionRecord{}, err
	}

	record := PromotionRecord{
		Version:      PromotionJournalVersion,
		SourceRef:    journalSourceRef(source, entry.Digest),
		FinalTag:     finalTag,
		MovingTag:    movingTag,
		CandidateTag: entry.CandidateTag,
	}

	got, present, err := resolveDigestIfPresent(ctx, reg, record.FinalTag)
	if err != nil {
		return PromotionRecord{}, fmt.Errorf("plan immutable final tag: %w", err)
	}

	if present {
		record.FinalExisted = true
		if got != entry.Digest {
			return PromotionRecord{}, immutableFinalMismatch(record.FinalTag, got, entry.Digest)
		}
	}

	if record.MovingTag != "" {
		got, present, err = resolveDigestIfPresent(ctx, reg, record.MovingTag)
		if err != nil {
			return PromotionRecord{}, fmt.Errorf("plan moving tag: %w", err)
		}

		if present {
			record.PreviousMovingDigest = got
		}
	}

	return record, nil
}

// ValidatePromotionJournalIntent checks that an existing journal describes
// exactly the promotion being retried. The pre-promotion state fields are not
// recomputed: preserving those original values is the reason the journal is
// reused instead of overwritten after a partial promotion.
func ValidatePromotionJournalIntent(records []PromotionRecord, entries []Entry, releaseTag string, stage Stage) error {
	if err := validatePromotionJournalStage(stage); err != nil {
		return err
	}

	recordIdx := 0

	for entryIdx, entry := range entries {
		if err := entry.ValidateForStage(stage, releaseTag); err != nil {
			return fmt.Errorf("imageledger: entry %d: %w", entryIdx, err)
		}

		if entry.CandidateTag == "" && !stage.AllowDigestRefFallback {
			continue
		}

		if recordIdx >= len(records) {
			return fmt.Errorf("imageledger: promotion journal is missing entry %d: %w", entryIdx, errs.ErrValidation)
		}

		if err := validatePromotionJournalEntry(records[recordIdx], entry, releaseTag, stage); err != nil {
			return fmt.Errorf("imageledger: promotion journal entry %d does not match ledger entry %d: %w", recordIdx, entryIdx, err)
		}

		recordIdx++
	}

	if recordIdx != len(records) {
		return fmt.Errorf("imageledger: promotion journal has %d unexpected entr(y/ies): %w", len(records)-recordIdx, errs.ErrValidation)
	}

	return validateUniquePromotionTargets(records)
}

func validatePromotionJournalStage(stage Stage) error {
	if !stage.IsRelease() || !stage.UseEntryReleaseTags {
		return fmt.Errorf("imageledger: promotion journal requires release stage with ledger release tags: %w", errs.ErrUsage)
	}

	return stage.Validate()
}

func validatePromotionJournalEntry(record PromotionRecord, entry Entry, releaseTag string, stage Stage) error {
	if _, err := validatePromotionRecord(record, releaseTag); err != nil {
		return err
	}

	finalTag, movingTag, err := releaseDestinationTags(stage, entry)
	if err != nil {
		return err
	}

	if record.FinalTag != finalTag || record.MovingTag != movingTag || record.CandidateTag != entry.CandidateTag {
		return fmt.Errorf("tags differ: %w", errs.ErrValidation)
	}

	digest, err := recordDigest(record)
	if err != nil {
		return err
	}

	if digest != entry.Digest || !journalSourceMatchesEntry(record.SourceRef, entry, stage.AllowDigestRefFallback) {
		return fmt.Errorf("source differs: %w", errs.ErrValidation)
	}

	return nil
}

func journalSourceMatchesEntry(source string, entry Entry, allowDigestRefFallback bool) bool {
	if entry.CandidateTag != "" && source == journalSourceRef(entry.CandidateTag, entry.Digest) {
		return true
	}

	return allowDigestRefFallback && source == entry.Ref
}

// releaseDestinationTags selects the promotion's final and moving tags from
// the stage destinations by their immutability, not slice position: the
// immutable destination is the final tag, the mutable one the moving tag.
func releaseDestinationTags(stage Stage, entry Entry) (string, string, error) {
	var finalTag, movingTag string

	for _, dest := range stage.destinations(entry) {
		switch {
		case dest.Immutable && finalTag == "":
			finalTag = dest.Ref
		case !dest.Immutable && movingTag == "":
			movingTag = dest.Ref
		}
	}

	if finalTag == "" {
		return "", "", fmt.Errorf("imageledger: release promotion has no immutable final destination: %w", errs.ErrValidation)
	}

	return finalTag, movingTag, nil
}

func journalSourceRef(source, digest string) string {
	if strings.Contains(source, "@sha256:") {
		return source
	}

	return source + "@" + digest
}

// MarshalPromotionJournal renders records as JSON Lines: one self-describing
// rollback record per line, so a partial write still yields replayable records.
func MarshalPromotionJournal(records []PromotionRecord) ([]byte, error) {
	var out bytes.Buffer

	for _, record := range records {
		line, err := json.Marshal(record)
		if err != nil {
			return nil, fmt.Errorf("imageledger: marshal promotion journal: %w", err)
		}

		out.Write(line)
		out.WriteByte('\n')
	}

	return out.Bytes(), nil
}

// ParsePromotionJournal decodes the JSON Lines rollback journal written by
// PlanReleasePromotionRollback/MarshalPromotionJournal.
func ParsePromotionJournal(data []byte) ([]PromotionRecord, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var records []PromotionRecord

	lineNo := 0

	for scanner.Scan() {
		lineNo++

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// The journal decides which tags rollback deletes, so a member this
		// build does not know (a misspelt final_existed reads as false) or a
		// repeated one (last wins silently) is refused, not read past.
		if err := rejectDuplicateMembers([]byte(line)); err != nil {
			return nil, fmt.Errorf("imageledger: parse promotion journal line %d: %w", lineNo, err)
		}

		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.DisallowUnknownFields()

		var record PromotionRecord
		if err := decoder.Decode(&record); err != nil {
			return nil, fmt.Errorf("imageledger: parse promotion journal line %d: %w: %w", lineNo, err, errs.ErrMalformedInput)
		}

		records = append(records, record)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("imageledger: scan promotion journal: %w", err)
	}

	return records, nil
}

// RollbackReleasePromotion restores the registry state captured before a
// release-tag promotion: a moving tag is restored to its previous digest when it
// existed, otherwise deleted; a final tag is deleted only when the journal says
// it was created by this promotion.
func RollbackReleasePromotion(ctx context.Context, reg PromotionRollbackRegistry, records []PromotionRecord, releaseTag string) error {
	digests := make([]string, len(records))
	for idx, record := range records {
		digest, err := validatePromotionRecord(record, releaseTag)
		if err != nil {
			return fmt.Errorf("imageledger: promotion journal entry %d: %w", idx, err)
		}

		digests[idx] = digest
	}

	if err := validateUniquePromotionTargets(records); err != nil {
		return err
	}

	plans := make([]promotionRollbackPlan, 0, len(records))

	for idx, record := range records {
		plan, err := planPromotionRollback(ctx, reg, record, digests[idx])
		if err != nil {
			return fmt.Errorf("imageledger: promotion journal entry %d: %w", idx, err)
		}

		plans = append(plans, plan)
	}

	for idx, plan := range plans {
		if err := executePromotionRollback(ctx, reg, plan); err != nil {
			return fmt.Errorf("imageledger: promotion rollback plan %d: %w", idx, err)
		}
	}

	return nil
}

func validateUniquePromotionTargets(records []PromotionRecord) error {
	owners := make(map[string]int, len(records)*2)

	for idx, record := range records {
		targets := []string{record.MovingTag}
		if !record.FinalExisted {
			targets = append(targets, record.FinalTag)
		}

		for _, target := range targets {
			if target == "" {
				continue
			}

			if previous, exists := owners[target]; exists {
				return fmt.Errorf("imageledger: promotion journal entries %d and %d both mutate %s: %w", previous, idx, target, errs.ErrValidation)
			}

			owners[target] = idx
		}
	}

	return nil
}

type promotionRollbackPlan struct {
	record         PromotionRecord
	digest         string
	rollbackMoving bool
	restoreSource  string
	deleteFinal    bool
}

func validatePromotionRecord(record PromotionRecord, releaseTag string) (string, error) {
	if record.Version != PromotionJournalVersion {
		return "", fmt.Errorf("imageledger: promotion journal version must be %d, got %d: %w", PromotionJournalVersion, record.Version, errs.ErrValidation)
	}

	digest, err := recordDigest(record)
	if err != nil {
		return "", err
	}

	entry := Entry{
		Ref:          record.SourceRef,
		Digest:       digest,
		FinalTag:     record.FinalTag,
		MovingTag:    record.MovingTag,
		CandidateTag: record.CandidateTag,
	}
	if err := entry.Validate(releaseTag); err != nil {
		return "", err
	}

	if record.PreviousMovingDigest != "" && !container.ValidDigest(record.PreviousMovingDigest) {
		return "", fmt.Errorf("imageledger: previous_moving_digest must be sha256:<64 hex>: %q: %w", record.PreviousMovingDigest, errs.ErrValidation)
	}

	return digest, nil
}

func planPromotionRollback(ctx context.Context, reg PromotionRollbackRegistry, record PromotionRecord, digest string) (promotionRollbackPlan, error) {
	plan := promotionRollbackPlan{record: record, digest: digest}

	rollbackMoving, restoreSource, err := planMovingTagRollback(ctx, reg, record, digest)
	if err != nil {
		return promotionRollbackPlan{}, err
	}

	plan.rollbackMoving = rollbackMoving
	plan.restoreSource = restoreSource

	deleteFinal, err := planFinalTagRollback(ctx, reg, record, digest)
	if err != nil {
		return promotionRollbackPlan{}, err
	}

	plan.deleteFinal = deleteFinal

	return plan, nil
}

func planMovingTagRollback(ctx context.Context, reg PromotionRollbackRegistry, record PromotionRecord, digest string) (bool, string, error) {
	if record.MovingTag == "" {
		return false, "", nil
	}

	got, present, err := resolveDigestIfPresent(ctx, reg, record.MovingTag)
	if err != nil {
		return false, "", fmt.Errorf("check moving tag before rollback: %w", err)
	}

	if !present || got != digest || record.PreviousMovingDigest == "" {
		return present && got == digest, "", nil
	}

	restoreSource := container.StripTag(record.MovingTag) + "@" + record.PreviousMovingDigest
	if err := verifyRefDigest(ctx, reg, restoreSource, record.PreviousMovingDigest); err != nil {
		return false, "", fmt.Errorf("check previous moving-tag manifest before rollback: %w", err)
	}

	return true, restoreSource, nil
}

func planFinalTagRollback(ctx context.Context, reg PromotionRollbackRegistry, record PromotionRecord, digest string) (bool, error) {
	if record.FinalExisted {
		return false, nil
	}

	got, present, err := resolveDigestIfPresent(ctx, reg, record.FinalTag)
	if err != nil {
		return false, fmt.Errorf("check final tag before rollback: %w", err)
	}

	if present && got != digest {
		return false, fmt.Errorf("refusing to delete failed release final tag %s: resolves to %s, want %s: %w", record.FinalTag, got, digest, errs.ErrValidation)
	}

	return present, nil
}

func executePromotionRollback(ctx context.Context, reg PromotionRollbackRegistry, plan promotionRollbackPlan) error {
	if err := executeMovingTagRollback(ctx, reg, plan); err != nil {
		return err
	}

	if !plan.deleteFinal {
		return nil
	}

	if err := confirmRollbackTarget(ctx, reg, plan.record.FinalTag, plan.digest); err != nil {
		return fmt.Errorf("final tag changed after rollback preflight: %w", err)
	}

	if err := reg.DeleteTag(ctx, plan.record.FinalTag); err != nil {
		return fmt.Errorf("delete new final tag %s: %w", plan.record.FinalTag, err)
	}

	return nil
}

func executeMovingTagRollback(ctx context.Context, reg PromotionRollbackRegistry, plan promotionRollbackPlan) error {
	if !plan.rollbackMoving {
		return nil
	}

	if plan.restoreSource != "" {
		if err := verifyRefDigest(ctx, reg, plan.restoreSource, plan.record.PreviousMovingDigest); err != nil {
			return fmt.Errorf("recheck previous moving-tag manifest before restore: %w", err)
		}
	}

	if err := confirmRollbackTarget(ctx, reg, plan.record.MovingTag, plan.digest); err != nil {
		return fmt.Errorf("moving tag changed after rollback preflight: %w", err)
	}

	return rollbackMovingTag(ctx, reg, plan.record, plan.restoreSource)
}

func confirmRollbackTarget(ctx context.Context, reg DigestResolver, ref, digest string) error {
	got, present, err := resolveDigestIfPresent(ctx, reg, ref)
	if err != nil {
		return err
	}

	if !present || got != digest {
		return fmt.Errorf("%s no longer serves %s: %w", ref, digest, errs.ErrValidation)
	}

	return nil
}

// rollbackMovingTag undoes a promotion's moving-tag move: when the moving
// tag currently serves the promoted digest, it is restored to its previous
// digest (when the journal recorded one) or deleted (when the promotion
// created it). A moving tag that does not resolve, or serves some other
// digest, is left untouched — the promotion never moved it, or something
// else owns it now.
func rollbackMovingTag(ctx context.Context, reg PromotionRollbackRegistry, record PromotionRecord, source string) error {
	if record.PreviousMovingDigest == "" {
		if err := reg.DeleteTag(ctx, record.MovingTag); err != nil {
			return fmt.Errorf("delete new moving tag %s: %w", record.MovingTag, err)
		}

		return nil
	}

	if err := reg.CopyTag(ctx, source, record.MovingTag); err != nil {
		// Restoring a moving tag assumes the registry still serves the manifest
		// it used to point at. That assumption is not portable: some forges
		// (Forgejo) drop a manifest the moment no tag references it, so the
		// previous release survives only while its own immutable :<version> tag
		// does. Name that, rather than passing up a bare registry error the
		// reader cannot act on.
		if errors.Is(err, errs.ErrMissingInput) {
			return fmt.Errorf("restore moving tag %s: the registry no longer serves %s — a manifest is kept only while some tag references it, so the previous release's immutable version tag must still exist: %w",
				record.MovingTag, source, err)
		}

		return fmt.Errorf("restore moving tag %s from %s: %w", record.MovingTag, source, err)
	}

	if err := verifyRefDigest(ctx, reg, record.MovingTag, record.PreviousMovingDigest); err != nil {
		return fmt.Errorf("after moving-tag restore, %s: %w", record.MovingTag, err)
	}

	return nil
}

func recordDigest(record PromotionRecord) (string, error) {
	_, digest, ok := strings.Cut(record.SourceRef, "@")
	if !ok || !container.ValidDigest(digest) {
		return "", fmt.Errorf("imageledger: source_ref must be pinned by @sha256:<64 hex>: %q: %w", record.SourceRef, errs.ErrValidation)
	}

	return digest, nil
}
