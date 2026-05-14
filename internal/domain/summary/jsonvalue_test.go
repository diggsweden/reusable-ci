// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/domain/summary"
)

func TestExtractTargetResult_KnownAndMissingKeys(t *testing.T) {
	t.Parallel()
	const doc = `{"stage":"build","result":"failure","ran":true,"targets":{"npm":"success","maven":"failure"}}`

	tests := []struct {
		name  string
		given string
		want  string
	}{
		{"npm_returns_success", "npm", "success"},
		{"maven_returns_failure", "maven", "failure"},
		{"missing_key_returns_skipped", "missing", "skipped"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, summary.ExtractTargetResult(doc, testCase.given))
		})
	}
}

func TestExtractTargetResult_EmptyOrBadInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		given string
	}{
		{"empty_string", ""},
		{"whitespace_only", "   "},
		{"not_json", "not-json"},
		{"targets_is_array_not_object", `{"targets":[]}`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, "skipped", summary.ExtractTargetResult(testCase.given, "anything"))
		})
	}
}
