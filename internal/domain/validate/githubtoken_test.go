// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/domain/validate"
)

func TestClassifyGitHubToken_KnownPrefixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		given string
		want  validate.GitHubTokenKind
	}{
		{"fine_grained_pat", "github_pat_AAAA", validate.GitHubTokenFineGrained},
		{"app_token", "ghs_AAAAAA", validate.GitHubTokenApp},
		{"classic_pat", "ghp_AAAAAA", validate.GitHubTokenClassic},
		{"empty_is_unknown", "", validate.GitHubTokenUnknown},
		{"random_is_unknown", "random", validate.GitHubTokenUnknown},
		{"gitlab_pat_is_unknown", "glpat-AAAA", validate.GitHubTokenUnknown},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, validate.ClassifyGitHubToken(testCase.given))
		})
	}
}
