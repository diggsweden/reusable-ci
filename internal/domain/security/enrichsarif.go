// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// EnrichGitHubSARIF rewrites a SARIF document body so every result has
// `partialFingerprints.primaryLocationLineHash`. GitHub's Code Scanning
// uses that field to dedupe results across re-runs; absent it, every
// alert gets a fresh ID on every push.
//
// Rules:
//   - Pre-existing primaryLocationLineHash values are left alone.
//   - When absent, fingerprints["matchBasedId/v1"] wins when present.
//   - Otherwise the identity is a canonical JSON tuple of ruleId,
//     first-location.uri, startLine and message.text, so delimiters in one
//     field cannot be mistaken for boundaries between fields.
//
// The function works on a generic `any` decoded by encoding/json so it
// preserves the SARIF document's other fields untouched.
func EnrichGitHubSARIF(body []byte) ([]byte, error) {
	doc, err := decodeSARIF(body)
	if err != nil {
		return nil, fmt.Errorf("parse SARIF: %w", err)
	}

	root, ok := doc.(map[string]any)
	if !ok {
		// Not an object — pass through unmodified.
		return body, nil
	}

	runs, _ := root["runs"].([]any)
	for _, run := range runs {
		r, ok := run.(map[string]any)
		if !ok {
			continue
		}

		results, _ := r["results"].([]any)
		for _, res := range results {
			result, ok := res.(map[string]any)
			if !ok {
				continue
			}

			enrichResult(result)
		}
	}

	return json.Marshal(root)
}

// enrichResult mutates a SARIF result entry in place, adding the
// partialFingerprints.primaryLocationLineHash field when absent.
func enrichResult(result map[string]any) {
	pf, _ := result["partialFingerprints"].(map[string]any)
	if pf != nil {
		if existing, ok := pf["primaryLocationLineHash"].(string); ok && existing != "" {
			return // already enriched
		}
	}

	hash := matchBasedID(result)
	if hash == "" {
		hash = composeFallbackHash(result)
	}

	if pf == nil {
		pf = map[string]any{}
	}

	pf["primaryLocationLineHash"] = hash
	result["partialFingerprints"] = pf
}

// matchBasedID returns the result's fingerprints["matchBasedId/v1"]
// string if present, otherwise "". This is the preferred dedupe key
// when SARIF producers emit it.
func matchBasedID(result map[string]any) string {
	fp, ok := result["fingerprints"].(map[string]any)
	if !ok {
		return ""
	}

	v, _ := fp["matchBasedId/v1"].(string)

	return v
}

// composeFallbackHash builds the unambiguous synthetic fingerprint
// when matchBasedId is unavailable.
func composeFallbackHash(result map[string]any) string {
	ruleID, _ := result["ruleId"].(string)
	if ruleID == "" {
		ruleID = "rule"
	}

	uri, startLine := extractURIAndStartLine(result)

	msgText := ""

	if msg, ok := result["message"].(map[string]any); ok {
		if t, ok := msg["text"].(string); ok {
			msgText = t
		}
	}

	return sarifResultIdentity(ruleID, uri, startLine, msgText)
}

func sarifResultIdentity(ruleID, uri string, line int, message string) string {
	// A string-only tuple is always JSON-encodable. Existing supplied
	// fingerprints are preserved; only newly generated fallbacks use this shape.
	body, _ := json.Marshal([]string{ruleID, uri, strconv.Itoa(line), message}) //nolint:errchkjson // fixed string-only tuple cannot produce a marshal error.

	return string(body)
}

var errTrailingSARIF = errors.New("SARIF must contain exactly one JSON value")

// decodeSARIF retains numeric spellings while enforcing the same single-value
// boundary as json.Unmarshal. Both document rewriters use this decoder.
func decodeSARIF(body []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var doc any
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: %w", err, errs.ErrMalformedInput)
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("%w: %w", errTrailingSARIF, errs.ErrMalformedInput)
		}

		return nil, fmt.Errorf("%w: %w", err, errs.ErrMalformedInput)
	}

	return doc, nil
}

// extractURIAndStartLine pulls the artifact URI and starting line from the
// first physicalLocation in a SARIF result. Missing/typed-wrong nodes
// fall through to the "unknown" / 0 defaults.
//
//nolint:cyclop // SARIF extraction: one branch per location/region/snippet position.
func extractURIAndStartLine(result map[string]any) (string, int) {
	locations, ok := result["locations"].([]any)
	if !ok || len(locations) == 0 {
		return "unknown", 0 //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}

	loc, ok := locations[0].(map[string]any)
	if !ok {
		return "unknown", 0
	}

	phys, ok := loc["physicalLocation"].(map[string]any)
	if !ok {
		return "unknown", 0
	}

	uri := "unknown"

	if art, ok := phys["artifactLocation"].(map[string]any); ok {
		if u, ok := art["uri"].(string); ok && u != "" {
			uri = u
		}
	}

	startLine := 0

	if region, ok := phys["region"].(map[string]any); ok {
		startLine = sarifLine(region["startLine"])
	}

	return uri, startLine
}

func sarifLine(raw any) int {
	switch value := raw.(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		if line, err := strconv.Atoi(value.String()); err == nil {
			return line
		}
		// JSON integers may also use decimal or exponent notation. Retain
		// the interpretation used before decoding with UseNumber.
		if number, err := value.Float64(); err == nil {
			return int(number)
		}
	}

	return 0
}
