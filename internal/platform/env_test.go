// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

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

func TestDetect_Forgejo_WinsOverGitHubMasquerade(t *testing.T) {
	// Forgejo's act_runner sets GITHUB_ACTIONS=true; the Forgejo signal
	// must be probed first so the forge axis resolves to Forgejo, not
	// GitHub (otherwise release/SARIF calls would hit api.github.com).
	cases := map[string]string{
		"FORGEJO_ACTIONS":    "true",
		"GITEA_ACTIONS":      "true",
		"FORGEJO_SERVER_URL": "https://codeberg.org",
		"FORGEJO_REPOSITORY": "itiquette/repo",
		"FORGEJO_OUTPUT":     "/tmp/out",
	}
	for signal, value := range cases {
		t.Run(signal, func(t *testing.T) {
			env := testenv.New(t)
			env.Setenv("GITHUB_ACTIONS", "true")
			env.Setenv("GITLAB_CI", "")
			env.Setenv(signal, value)

			if got := platform.Detect(); got != provider.PlatformForgejo {
				t.Errorf("Detect() = %q, want Forgejo with %s set", got, signal)
			}
			// The runner axis stays GHA-compatible for Forgejo.
			if got := platform.DetectRunner(); got != provider.RunnerGHA {
				t.Errorf("DetectRunner() = %q, want RunnerGHA for Forgejo", got)
			}
		})
	}
}

func TestDetectRunner(t *testing.T) {
	cases := []struct {
		name   string
		github string
		gitlab string
		want   provider.RunnerKind
	}{
		{"github", "true", "", provider.RunnerGHA},
		{"gitlab", "", "true", provider.RunnerGitLab},
		{"local", "", "", provider.RunnerLocal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := testenv.New(t)
			env.Setenv("GITHUB_ACTIONS", tc.github)
			env.Setenv("GITLAB_CI", tc.gitlab)

			if got := platform.DetectRunner(); got != tc.want {
				t.Errorf("DetectRunner() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDetect_ProviderOverride(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("GITHUB_ACTIONS", "true") // would auto-detect GitHub
	env.Setenv("GITLAB_CI", "")
	env.Setenv("REUSABLE_CI_PROVIDER", "forgejo")

	if got := platform.Detect(); got != provider.PlatformForgejo {
		t.Errorf("Detect() = %q, want Forgejo from override", got)
	}
}

func TestDetect_ProviderOverride_InvalidFallsBackToAuto(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("GITHUB_ACTIONS", "true")
	env.Setenv("GITLAB_CI", "")
	env.Setenv("REUSABLE_CI_PROVIDER", "bananas")

	if got := platform.Detect(); got != provider.PlatformGitHub {
		t.Errorf("Detect() = %q, want GitHub when override invalid", got)
	}
}

func TestDetectRunner_Override(t *testing.T) {
	env := testenv.New(t)
	env.Setenv("GITHUB_ACTIONS", "")
	env.Setenv("GITLAB_CI", "")
	env.Setenv("REUSABLE_CI_RUNNER", "gha-compatible")

	if got := platform.DetectRunner(); got != provider.RunnerGHA {
		t.Errorf("DetectRunner() = %q, want RunnerGHA from override", got)
	}
}
