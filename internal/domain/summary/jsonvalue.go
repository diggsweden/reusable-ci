// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"encoding/json"
	"strings"
)

// ExtractTargetResult pulls a target's result string out of a stage-
// result-envelope JSON document. The bash uses a substring search to
// avoid a jq dep; we use encoding/json for accuracy and fall back to
// "skipped" on any failure or missing key — matching the bash's
// ci_json_value default.
//
//	{"stage":"build","targets":{"npm":"success", … }}  + key="npm" → "success"
//	missing key / malformed JSON / empty input          → "skipped"
func ExtractTargetResult(stageResultJSON, key string) string {
	if strings.TrimSpace(stageResultJSON) == "" {
		return string(ResultSkipped)
	}
	var doc struct {
		Targets map[string]string `json:"targets"`
	}
	if err := json.Unmarshal([]byte(stageResultJSON), &doc); err != nil {
		return string(ResultSkipped)
	}
	if v, ok := doc.Targets[key]; ok {
		return v
	}
	return string(ResultSkipped)
}
