// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package provider_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

func TestPlatform_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		given provider.Platform
		want  bool
	}{
		{"github_is_valid", provider.PlatformGitHub, true},
		{"gitlab_is_valid", provider.PlatformGitLab, true},
		{"local_is_valid", provider.PlatformLocal, true},
		{"empty_is_invalid", provider.Platform(""), false},
		{"unknown_provider_is_invalid", provider.Platform("bitbucket"), false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, testCase.given.IsValid())
		})
	}
}

func TestPlatform_String_GitHubIsLowerCase(t *testing.T) {
	t.Parallel()
	require.Equal(t, "github", provider.PlatformGitHub.String())
}
