// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// SetSARIFCategory stamps every run's `automationDetails.id` with the
// category, so GitHub/Forgejo Code Scanning treats each category as a
// DISTINCT analysis. This is the mechanism the codeql-action uses — the
// analysis is keyed by `automationDetails.id`, NOT by the upload's
// `tool_name`. Without it, two matrix legs (e.g. an image scan and a repo
// scan) on the same commit silently overwrite each other.
//
// Rules:
//   - Empty category is a no-op (returns the body unchanged).
//   - A run with a pre-existing non-empty id is left alone — a producer
//     that already declared its analysis identity is authoritative.
//   - With more than one run, the run index is appended (`<category>/<i>`)
//     so the runs stay distinct within one document while still resolving
//     to the same category prefix.
//
// Operates on a generic `any` decoded by encoding/json, so every other
// SARIF field is preserved untouched.
func SetSARIFCategory(body []byte, category string) ([]byte, error) {
	if category == "" {
		return body, nil
	}

	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse SARIF: %w", err)
	}

	root, ok := doc.(map[string]any)
	if !ok {
		return body, nil // not an object — pass through unmodified
	}

	runs, _ := root["runs"].([]any)
	for index, run := range runs {
		r, ok := run.(map[string]any)
		if !ok {
			continue
		}

		setRunAutomationID(r, automationID(category, index, len(runs)))
	}

	return json.Marshal(root)
}

// automationID is the category for a single run, suffixed with the run
// index only when a document carries more than one run.
func automationID(category string, index, runCount int) string {
	if runCount > 1 {
		return category + "/" + strconv.Itoa(index)
	}

	return category
}

// setRunAutomationID fills run.automationDetails.id unless the run already
// declares a non-empty id.
func setRunAutomationID(run map[string]any, id string) {
	details, _ := run["automationDetails"].(map[string]any)
	if details != nil {
		if existing, ok := details["id"].(string); ok && existing != "" {
			return
		}
	}

	if details == nil {
		details = map[string]any{}
	}

	details["id"] = id
	run["automationDetails"] = details
}
