// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary_test

import (
	"encoding/json"
	"testing"
)

func stageResultJSON(t *testing.T, stage string, targets map[string]string) string {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"version": 1,
		"stage":   stage,
		"result":  "success", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"ran":     true,
		"targets": targets,
	})
	if err != nil {
		t.Fatal(err)
	}

	return string(body)
}
