// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package validate

import (
	"fmt"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// RefTypeError is returned when the trigger's ref type doesn't match
// what the release flow requires. Callers that surface user-facing
// guidance (e.g. "push a signed tag") format the message themselves —
// this type only carries the structured failure data.
type RefTypeError struct {
	Got provider.RefType // observed ref type
	Ref string           // raw ref ("refs/heads/main", "refs/tags/v1.0.0", …)
}

// Error implements error.
func (e *RefTypeError) Error() string {
	return fmt.Sprintf("Release workflow must be triggered by pushing a tag (got %q, ref %q)", e.Got, e.Ref)
}

// RequireTagRefType validates that refType is the tag type. Returns a
// *RefTypeError on mismatch so callers can render their own guidance.
func RequireTagRefType(refType provider.RefType, ref string) error {
	if refType == "" {
		return fmt.Errorf("Usage: validate ref-type <ref-type> <ref-name> [ref]: %w", errs.ErrUsage)
	}
	if refType != provider.RefTypeTag {
		return &RefTypeError{Got: refType, Ref: ref}
	}
	return nil
}
