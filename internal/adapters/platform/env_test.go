// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform_test

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/platform"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
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

// TestDetect_Forgejo_WinsOverGitHubMasquerade covers the FORGE axis: any
// Forgejo signal means "talk to Forgejo, not api.github.com". Naming the
// target is enough, which is why $FORGEJO_SERVER_URL alone counts here.
//
// Forgejo's act_runner sets GITHUB_ACTIONS=true, so the Forgejo signal must
// be probed before the GitHub branch.
func TestDetect_Forgejo_WinsOverGitHubMasquerade(t *testing.T) {
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
		})
	}
}

// TestDetectRunner_IdentityIsNotTarget covers the RUNNER axis, which asks a
// different question: not "which forge do I call" but "whose runner am I
// executing on". Only markers a runner injects about ITSELF may answer it.
//
// Answering both with one predicate makes a GitHub runner claim to be a Forgejo
// one as soon as a Forgejo TARGET is named. That has two heads: RunnerKind picks
// the output dialect, so the run silently loses its GitHub annotations, log
// groups and job summaries; and $GITHUB_TOKEN looks like a Forgejo credential to
// the token chain.
func TestDetectRunner_IdentityIsNotTarget(t *testing.T) {
	runnerMarkers := map[string]string{
		"FORGEJO_ACTIONS": "true",
		"GITEA_ACTIONS":   "true",
		// The runner-provided step-output sink: a path only a Forgejo
		// runner has any reason to create.
		"FORGEJO_OUTPUT": "/tmp/out",
	}
	for signal, value := range runnerMarkers {
		t.Run("runner marker "+signal, func(t *testing.T) {
			env := testenv.New(t)
			env.Setenv("GITHUB_ACTIONS", "true")
			env.Setenv("GITLAB_CI", "")
			env.Setenv(signal, value)

			if got := platform.DetectRunner(); got != provider.RunnerForgejo {
				t.Errorf("DetectRunner() = %q, want RunnerForgejo for %s", got, signal)
			}
		})
	}

	// A GitHub-hosted repo publishing to a Forgejo instance sets these. It
	// is still running on GitHub, and must still get GitHub's dialect.
	targetOnly := map[string]string{
		"FORGEJO_SERVER_URL": "https://third-party.example",
		"FORGEJO_REPOSITORY": "owner/repo",
	}
	for signal, value := range targetOnly {
		t.Run("target var "+signal, func(t *testing.T) {
			env := testenv.New(t)
			env.Setenv("GITHUB_ACTIONS", "true")
			env.Setenv("GITLAB_CI", "")
			env.Setenv(signal, value)

			if got := platform.DetectRunner(); got != provider.RunnerGitHub {
				t.Errorf("DetectRunner() = %q, want RunnerGitHub: %s names a TARGET,"+
					" not the runner we execute on", got, signal)
			}

			// ...while the forge axis still correctly targets Forgejo.
			if got := platform.Detect(); got != provider.PlatformForgejo {
				t.Errorf("Detect() = %q, want Forgejo with %s set", got, signal)
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
		{"github", "true", "", provider.RunnerGitHub},
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
	env.Setenv("REUSABLE_CI_RUNNER", "forgejo")

	if got := platform.DetectRunner(); got != provider.RunnerForgejo {
		t.Errorf("DetectRunner() = %q, want RunnerForgejo from override", got)
	}
}
