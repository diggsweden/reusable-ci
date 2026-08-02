// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provenance

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// ParseExternalParametersJSON parses a caller-declared JSON object of
// extra buildDefinition.externalParameters. Empty input means no extras
// (nil map). Invalid JSON or a non-object document (array, string,
// null, …) is malformed input, rejected before any statement is built
// or any ledger is written.
func ParseExternalParametersJSON(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil //nolint:nilnil // a nil map IS the valid "no extras declared" result; every caller merges/assigns it directly.
	}

	var params map[string]any
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return nil, fmt.Errorf("provenance: external parameters must be a JSON object: %w: %w", err, errs.ErrMalformedInput)
	}

	if params == nil {
		return nil, fmt.Errorf("provenance: external parameters must be a JSON object, got null: %w", errs.ErrMalformedInput)
	}

	return params, nil
}

// MergeExternalParameters merges caller-declared extras into ext with
// every already-present key reserved: the engine's computed facts and a
// base predicate's own fields can never be shadowed by a declared
// document. A collision fails with ErrValidation rather than
// overriding. This is the single merge implementation shared by the
// statement builder and the signer's per-image predicate enrichment.
func MergeExternalParameters(ext, extras map[string]any) error {
	for key, value := range extras {
		if _, taken := ext[key]; taken {
			return fmt.Errorf("provenance key %q collides with a computed externalParameters field: %w", key, errs.ErrValidation)
		}

		ext[key] = value
	}

	return nil
}
