// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package security

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// EnrichGitHubSARIF rewrites a SARIF document body so every result has
// `partialFingerprints.primaryLocationLineHash`. GitHub's Code Scanning
// uses that field to dedupe results across re-runs; absent it, every
// alert gets a fresh ID on every push.
//
// Rules:
//   - Pre-existing primaryLocationLineHash values are left alone.
//   - When absent, fingerprints["matchBasedId/v1"] wins when present.
//   - Otherwise the hash is composed from
//     ruleId | first-location.uri | startLine | message.text
//     joined with "|".
//
// The function works on a generic `any` decoded by encoding/json so it
// preserves the SARIF document's other fields untouched.
func EnrichGitHubSARIF(body []byte) ([]byte, error) {
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
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

// composeFallbackHash builds the pipe-joined synthetic fingerprint
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

	return strings.Join([]string{ruleID, uri, strconv.Itoa(startLine), msgText}, "|")
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
		switch v := region["startLine"].(type) {
		case float64:
			startLine = int(v)
		case int:
			startLine = v
		}
	}

	return uri, startLine
}
