// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package platform detects which CI platform the binary is running on
// based on environment variables. Thin lookup — no business logic, no
// I/O beyond os.Getenv.
//
// Detection has two orthogonal axes, mirroring internal/domain/provider:
//
//   - Detect()       → provider.Platform   (which forge API to call)
//   - DetectRunner() → provider.RunnerKind (which runner conventions to emit)
//
// Forgejo Actions sets GITHUB_ACTIONS=true while exposing a Gitea/Forgejo
// forge API *and* a distinct runner dialect (it renders neither GitHub
// workflow commands nor job summaries — go-gitea/gitea#27898 — and uses
// $FORGEJO_* sinks). So both axes treat Forgejo as its own value, probed
// ahead of the GitHub branch. Both axes honour an explicit override env
// var so a runner masquerading as another forge can always be corrected.
package platform

import (
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// Override env vars. The root --provider / --runner flags bridge their
// argv values into these so detection has a single source of truth.
const (
	envProviderOverride = "REUSABLE_CI_PROVIDER"
	envRunnerOverride   = "REUSABLE_CI_RUNNER"
)

// Detect returns the active forge API (provider.Platform).
//
// Order matters: Forgejo masquerades as GitHub (it sets
// GITHUB_ACTIONS=true), so the Forgejo/Gitea signal is probed BEFORE the
// GitHub branch. An explicit REUSABLE_CI_PROVIDER override wins over all
// auto-detection.
//
//	REUSABLE_CI_PROVIDER set & valid → that platform
//	Forgejo/Gitea signal present     → Forgejo
//	GITHUB_ACTIONS=true              → GitHub
//	GITLAB_CI=true                   → GitLab
//	else                             → Local
func Detect() provider.Platform {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv(envProviderOverride))); v != "" && v != "auto" {
		if p := provider.Platform(v); p.IsValid() {
			return p
		}
	}

	switch {
	case isForgejo():
		return provider.PlatformForgejo
	case ciFlag("GITHUB_ACTIONS"):
		return provider.PlatformGitHub
	case ciFlag("GITLAB_CI"):
		return provider.PlatformGitLab
	default:
		return provider.PlatformLocal
	}
}

// DetectRunner returns the active runner conventions (provider.RunnerKind).
//
// Forgejo/Gitea Actions sets GITHUB_ACTIONS=true but is a *distinct* runner
// dialect — it does not render GitHub workflow commands and exposes its own
// $FORGEJO_* sinks — so the Forgejo signal is probed BEFORE the GitHub
// branch (same precedence as Detect). An explicit REUSABLE_CI_RUNNER
// override wins over auto-detection.
//
//	REUSABLE_CI_RUNNER set & valid          → that runner
//	GITHUB_ACTIONS=true + Forgejo/Gitea signal → RunnerForgejo
//	GITHUB_ACTIONS=true                     → RunnerGitHub
//	GITLAB_CI=true                          → RunnerGitLab
//	else                                    → RunnerLocal
func DetectRunner() provider.RunnerKind {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv(envRunnerOverride))); v != "" && v != "auto" {
		if r := provider.RunnerKind(v); r.IsValid() {
			return r
		}
	}

	switch {
	case isForgejo():
		return provider.RunnerForgejo
	case ciFlag("GITHUB_ACTIONS"):
		return provider.RunnerGitHub
	case ciFlag("GITLAB_CI"):
		return provider.RunnerGitLab
	default:
		return provider.RunnerLocal
	}
}

// isForgejo reports whether the runner is Forgejo (or Gitea) Actions
// rather than GitHub. Forgejo's act_runner sets GITHUB_ACTIONS=true and
// mirrors the runner context into FORGEJO_* env vars; Gitea sets
// GITEA_ACTIONS=true. We treat any of those signals — alongside the
// GitHub-compatible runner — as Forgejo.
func isForgejo() bool {
	if !ciFlag("GITHUB_ACTIONS") {
		return false
	}

	switch {
	case truthy(os.Getenv("FORGEJO_ACTIONS")):
		return true
	case truthy(os.Getenv("GITEA_ACTIONS")):
		return true
	case os.Getenv("FORGEJO_SERVER_URL") != "":
		return true
	case os.Getenv("FORGEJO_REPOSITORY") != "":
		return true
	case os.Getenv("FORGEJO_OUTPUT") != "":
		return true
	default:
		return false
	}
}

// ciFlag reports whether the named CI marker env var is exactly "true",
// the value GitHub/Forgejo/GitLab runners set on their respective
// markers (GITHUB_ACTIONS / GITLAB_CI).
func ciFlag(name string) bool {
	return os.Getenv(name) == "true"
}

// truthy reports whether a string holds a conventional truthy value.
func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
