// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provenance

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// Envelopes normalises `cosign verify-attestation` output into a list of DSSE
// envelopes.
//
// cosign emits a bare envelope object when it verified one attestation and a
// JSON array when it verified several, and callers cannot know in advance
// which they will get. Handling only the object shape makes a multi-attestation
// image look like it has no attestation at all, which for a caller deciding
// whether something is still referenced is the dangerous direction to be wrong
// in. Both shapes are normalised here so no caller has to remember.
func Envelopes(body []byte) ([]any, error) {
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse attestation output: %w: %w", err, errs.ErrMalformedInput)
	}

	if envelopes, ok := raw.([]any); ok {
		return envelopes, nil
	}

	return []any{raw}, nil
}

// EnvelopePayload extracts the base64 payload from one DSSE envelope as
// decoded from `cosign verify-attestation` output. The bool is false when
// the value is not an envelope object or carries no payload.
func EnvelopePayload(envelope any) (string, bool) {
	obj, ok := envelope.(map[string]any)
	if !ok {
		return "", false
	}

	payload, ok := obj["payload"].(string)

	return payload, ok && payload != ""
}

// DecodeStatement base64-decodes a DSSE payload and unmarshals the in-toto
// statement it carries into a generic map for field-by-field inspection.
func DecodeStatement(payload string) (map[string]any, error) {
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("decode SLSA provenance payload: %w: %w", err, errs.ErrMalformedInput)
	}

	var statement map[string]any
	if err := json.Unmarshal(decoded, &statement); err != nil {
		return nil, fmt.Errorf("parse SLSA provenance statement: %w: %w", err, errs.ErrMalformedInput)
	}

	return statement, nil
}

// NestedMap walks a decoded statement down the given object keys,
// returning false as soon as a step is missing or not an object.
func NestedMap(root map[string]any, keys ...string) (map[string]any, bool) {
	current := root
	for _, key := range keys {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil, false
		}

		current = next
	}

	return current, true
}

// StatementString reads a string field from a decoded statement object,
// returning "" when the key is absent or not a string.
func StatementString(root map[string]any, key string) string {
	value, _ := root[key].(string)

	return value
}
