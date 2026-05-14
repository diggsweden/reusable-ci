// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package platform_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/internal/platform"
	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
)

func TestDetect_GitHub(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("GITHUB_ACTIONS", "true")
	env.Setenv("GITLAB_CI", "")
	if got := platform.Detect(); got != provider.PlatformGitHub {
		t.Errorf("Detect() = %q, want %q", got, provider.PlatformGitHub)
	}
}

func TestDetect_GitLab(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("GITHUB_ACTIONS", "")
	env.Setenv("GITLAB_CI", "true")
	if got := platform.Detect(); got != provider.PlatformGitLab {
		t.Errorf("Detect() = %q, want %q", got, provider.PlatformGitLab)
	}
}

func TestDetect_Local(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("GITHUB_ACTIONS", "")
	env.Setenv("GITLAB_CI", "")
	if got := platform.Detect(); got != provider.PlatformLocal {
		t.Errorf("Detect() = %q, want %q", got, provider.PlatformLocal)
	}
}

func TestDetect_GitHubWinsOverGitLab(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("GITHUB_ACTIONS", "true")
	env.Setenv("GITLAB_CI", "true")
	if got := platform.Detect(); got != provider.PlatformGitHub {
		t.Errorf("Detect() = %q, want GitHub when both set", got)
	}
}
