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
// Mirrors scripts/security/enrich-github-sarif.sh end-to-end:
//   - Pre-existing primaryLocationLineHash values are left alone.
//   - When absent, fingerprints["matchBasedId/v1"] wins when present.
//   - Otherwise the hash is composed from
//     ruleId | first-location.uri | startLine | message.text
//     joined with "|" (the same pipe-joined fallback as the bash).
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
		// Not an object — pass through unmodified (matches the bash's
		// behaviour: jq leaves non-object inputs alone).
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
// string if present, otherwise "". Mirrors the bash's first-choice
// fallback.
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

	uri := "unknown"
	startLine := 0
	if locations, ok := result["locations"].([]any); ok && len(locations) > 0 {
		if loc, ok := locations[0].(map[string]any); ok {
			if phys, ok := loc["physicalLocation"].(map[string]any); ok {
				if art, ok := phys["artifactLocation"].(map[string]any); ok {
					if u, ok := art["uri"].(string); ok && u != "" {
						uri = u
					}
				}
				if region, ok := phys["region"].(map[string]any); ok {
					switch v := region["startLine"].(type) {
					case float64:
						startLine = int(v)
					case int:
						startLine = v
					}
				}
			}
		}
	}

	msgText := ""
	if msg, ok := result["message"].(map[string]any); ok {
		if t, ok := msg["text"].(string); ok {
			msgText = t
		}
	}

	return strings.Join([]string{ruleID, uri, strconv.Itoa(startLine), msgText}, "|")
}
