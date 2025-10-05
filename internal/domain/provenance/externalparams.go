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

// MergeExternalParameters merges caller-declared extras into ext. Engine-owned
// names are reserved across every profile, including profiles that omit a name,
// and already-present predicate fields can never be shadowed. A collision fails
// with ErrValidation rather than overriding.
func MergeExternalParameters(ext, extras map[string]any) error {
	for key, value := range extras {
		if reservedExternalParameter(key) {
			return fmt.Errorf("provenance key %q is reserved for an engine-computed externalParameters field: %w", key, errs.ErrValidation)
		}

		if _, taken := ext[key]; taken {
			return fmt.Errorf("provenance key %q collides with a computed externalParameters field: %w", key, errs.ErrValidation)
		}

		ext[key] = value
	}

	return nil
}

func reservedExternalParameter(key string) bool {
	switch key {
	case "source", "ref", "workflow", "image", "flavor", "base_input_id", "base":
		return true
	default:
		return false
	}
}
