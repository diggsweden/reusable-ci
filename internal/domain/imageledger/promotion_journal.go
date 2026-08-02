// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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
	SourceRef            string `json:"source_ref"`
	FinalTag             string `json:"final_tag"`
	MovingTag            string `json:"moving_tag,omitempty"`
	CandidateTag         string `json:"candidate_tag,omitempty"`
	FinalExisted         bool   `json:"final_existed"`
	PreviousMovingDigest string `json:"previous_moving_digest,omitempty"`
}

// PromotionRollbackRegistry is the registry surface needed to undo a journaled
// release promotion: resolve tags, delete newly-created tags, and restore a
// previous moving tag by copying its old digest back.
type PromotionRollbackRegistry interface {
	CleanupRegistry
	CopyTag(ctx context.Context, source, dest string) error
}

// PlanPromotionJournal validates release-tag promotion and captures rollback
// state before any tag is moved. It intentionally only supports release-stage
// promotion using ledger-carried release tags, matching Forgejo CI's sign-before-
// publish contract.
func PlanPromotionJournal(ctx context.Context, reg DigestResolver, entries []Entry, releaseTag string, stage Stage) ([]PromotionRecord, error) {
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

	return records, nil
}

func planPromotionRecord(ctx context.Context, reg DigestResolver, entry Entry, stage Stage) (PromotionRecord, error) {
	source, err := promotionSource(ctx, reg, entry, stage.AllowDigestRefFallback)
	if err != nil {
		return PromotionRecord{}, err
	}

	dests := stage.destinations(entry)
	if len(dests) == 0 {
		return PromotionRecord{}, fmt.Errorf("imageledger: release promotion has no destination tags: %w", errs.ErrValidation)
	}

	record := PromotionRecord{
		SourceRef:    journalSourceRef(source, entry.Digest),
		FinalTag:     dests[0],
		CandidateTag: entry.CandidateTag,
	}
	if len(dests) > 1 {
		record.MovingTag = dests[1]
	}

	if got, err := reg.ResolveDigest(ctx, record.FinalTag); err == nil {
		record.FinalExisted = true
		if got != entry.Digest {
			return PromotionRecord{}, immutableFinalMismatch(record.FinalTag, got, entry.Digest)
		}
	}

	if record.MovingTag != "" {
		if got, err := reg.ResolveDigest(ctx, record.MovingTag); err == nil {
			record.PreviousMovingDigest = got
		}
	}

	return record, nil
}

func journalSourceRef(source, digest string) string {
	if strings.Contains(source, "@sha256:") {
		return source
	}

	return source + "@" + digest
}

// MarshalPromotionJournal renders records as JSON Lines, matching the existing
// Forgejo CI rollback journal shape so the migration can be staged safely.
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
// PlanPromotionJournal/MarshalPromotionJournal.
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

		var record PromotionRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("imageledger: parse promotion journal line %d: %w", lineNo, errs.ErrMalformedInput)
		}

		records = append(records, record)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("imageledger: scan promotion journal: %w", err)
	}

	return records, nil
}

// RollbackPromotionJournal restores the registry state captured before a
// release-tag promotion: a moving tag is restored to its previous digest when it
// existed, otherwise deleted; a final tag is deleted only when the journal says
// it was created by this promotion.
func RollbackPromotionJournal(ctx context.Context, reg PromotionRollbackRegistry, records []PromotionRecord, releaseTag string) error {
	seen := map[string]bool{}

	for idx, record := range records {
		key := record.SourceRef + "\x00" + record.FinalTag + "\x00" + record.MovingTag
		if seen[key] {
			continue
		}

		seen[key] = true

		if err := rollbackPromotionRecord(ctx, reg, record, releaseTag); err != nil {
			return fmt.Errorf("imageledger: promotion journal entry %d: %w", idx, err)
		}
	}

	return nil
}

func rollbackPromotionRecord(ctx context.Context, reg PromotionRollbackRegistry, record PromotionRecord, releaseTag string) error {
	digest, err := recordDigest(record)
	if err != nil {
		return err
	}

	entry := Entry{
		Ref:          record.SourceRef,
		Digest:       digest,
		FinalTag:     record.FinalTag,
		MovingTag:    record.MovingTag,
		CandidateTag: record.CandidateTag,
	}
	if err := entry.Validate(releaseTag); err != nil {
		return err
	}

	if record.PreviousMovingDigest != "" && !container.ValidDigest(record.PreviousMovingDigest) {
		return fmt.Errorf("imageledger: previous_moving_digest must be sha256:<64 hex>: %q: %w", record.PreviousMovingDigest, errs.ErrValidation)
	}

	if err := rollbackMovingTag(ctx, reg, record, digest); err != nil {
		return err
	}

	return rollbackFinalTag(ctx, reg, record, digest)
}

// rollbackMovingTag undoes a promotion's moving-tag move: when the moving
// tag currently serves the promoted digest, it is restored to its previous
// digest (when the journal recorded one) or deleted (when the promotion
// created it). A moving tag that does not resolve, or serves some other
// digest, is left untouched — the promotion never moved it, or something
// else owns it now.
func rollbackMovingTag(ctx context.Context, reg PromotionRollbackRegistry, record PromotionRecord, digest string) error {
	if record.MovingTag == "" {
		return nil
	}

	if got, err := reg.ResolveDigest(ctx, record.MovingTag); err != nil || got != digest {
		return nil //nolint:nilerr // unresolvable moving tag means the promotion never moved it; nothing to roll back.
	}

	if record.PreviousMovingDigest == "" {
		if err := reg.DeleteTag(ctx, record.MovingTag); err != nil {
			return fmt.Errorf("delete new moving tag %s: %w", record.MovingTag, err)
		}

		return nil
	}

	source := container.StripTag(record.MovingTag) + "@" + record.PreviousMovingDigest
	if err := reg.CopyTag(ctx, source, record.MovingTag); err != nil {
		return fmt.Errorf("restore moving tag %s from %s: %w", record.MovingTag, source, err)
	}

	if err := verifyRefDigest(ctx, reg, record.MovingTag, record.PreviousMovingDigest); err != nil {
		return fmt.Errorf("after moving-tag restore, %s: %w", record.MovingTag, err)
	}

	return nil
}

// rollbackFinalTag deletes the immutable final tag only when the journal
// says this promotion created it, and refuses when the tag now serves a
// different digest than the failed promotion pushed.
func rollbackFinalTag(ctx context.Context, reg PromotionRollbackRegistry, record PromotionRecord, digest string) error {
	if record.FinalExisted {
		return nil
	}

	if got, err := reg.ResolveDigest(ctx, record.FinalTag); err == nil && got != digest {
		return fmt.Errorf("refusing to delete failed release final tag %s: resolves to %s, want %s: %w", record.FinalTag, got, digest, errs.ErrValidation)
	}

	if err := reg.DeleteTag(ctx, record.FinalTag); err != nil {
		return fmt.Errorf("delete new final tag %s: %w", record.FinalTag, err)
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
