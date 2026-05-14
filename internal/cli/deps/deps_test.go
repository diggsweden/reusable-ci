// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package deps_test

import (
	"context"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
)

func TestBuild_DetectsPlatformAndWiresDependencies(t *testing.T) {
	tests := []struct {
		name   string
		github bool
		gitlab bool
		want   provider.Platform
	}{
		{name: "github", github: true, want: provider.PlatformGitHub},
		{name: "gitlab", gitlab: true, want: provider.PlatformGitLab},
		{name: "local", want: provider.PlatformLocal},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			env := testenv.New(t)
			env.Setenv("GITHUB_ACTIONS", boolEnv(testCase.github))
			env.Setenv("GITLAB_CI", boolEnv(testCase.gitlab))

			d, err := deps.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if d.Platform != testCase.want {
				t.Errorf("Platform = %q, want %q", d.Platform, testCase.want)
			}
			if d.Provider == nil {
				t.Fatal("Provider nil")
			}
			if got := d.Provider.Name(); got != testCase.want {
				t.Errorf("Provider.Name = %q, want %q", got, testCase.want)
			}
			if d.OutputSink == nil {
				t.Errorf("OutputSink nil")
			}
		})
	}
}

func boolEnv(b bool) string {
	if b {
		return "true"
	}
	return ""
}
