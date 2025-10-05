// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package summary_test

import (
	"encoding/json"
	"testing"
)

func stageResultJSON(t *testing.T, stage string, targets map[string]string) string {
	t.Helper()

	ran := false
	result := "skipped"

	for _, status := range targets {
		switch status {
		case "failure":
			result = "failure"
			ran = true
		case "cancelled":
			if result != "failure" {
				result = "cancelled"
			}

			ran = true
		case "success":
			if result == "skipped" {
				result = "success"
			}

			ran = true
		case "skipped":
		default:
			t.Fatalf("unknown fixture result %q", status)
		}
	}

	body, err := json.Marshal(map[string]any{
		"version": 1,
		"stage":   stage,
		"result":  result,
		"ran":     ran,
		"targets": targets,
	})
	if err != nil {
		t.Fatal(err)
	}

	return string(body)
}
