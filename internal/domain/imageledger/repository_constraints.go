// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package imageledger

import (
	"fmt"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// ValidateEntryRepository ensures every image ref/tag on an entry stays inside
// the expected repository. Empty expectedRepository disables the check so the
// generic ledger remains registry-agnostic unless a forge boundary opts in.
func ValidateEntryRepository(entry Entry, expectedRepository string) error {
	if expectedRepository == "" {
		return nil
	}

	for _, ref := range []struct {
		field string
		value string
	}{
		{"ref", entry.Ref},
		{finalTagField, entry.FinalTag},
		{movingTagField, entry.MovingTag},
		{candidateTagField, entry.CandidateTag},
	} {
		if err := validateRepositoryRef(ref.field, ref.value, expectedRepository); err != nil {
			return err
		}
	}

	return nil
}

// ValidatePromotionRecordRepository applies the same repository constraint to a
// rollback journal record. Journals are a signer-boundary artifact, so the
// rollback command rechecks them before deleting/restoring tags.
func ValidatePromotionRecordRepository(record PromotionRecord, expectedRepository string) error {
	if expectedRepository == "" {
		return nil
	}

	for _, ref := range []struct {
		field string
		value string
	}{
		{"source_ref", record.SourceRef},
		{finalTagField, record.FinalTag},
		{movingTagField, record.MovingTag},
		{candidateTagField, record.CandidateTag},
	} {
		if err := validateRepositoryRef(ref.field, ref.value, expectedRepository); err != nil {
			return err
		}
	}

	return nil
}

func validateRepositoryRef(field, ref, expectedRepository string) error {
	if ref == "" {
		return nil
	}

	if got := container.StripTagOrDigest(ref); got != expectedRepository {
		return fmt.Errorf("imageledger: %s must be under %s: %q: %w", field, expectedRepository, ref, errs.ErrValidation)
	}

	return nil
}
