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
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
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
	case isForgejoTarget():
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
	case isForgejoRunner():
		return provider.RunnerForgejo
	case ciFlag("GITHUB_ACTIONS"):
		return provider.RunnerGitHub
	case ciFlag("GITLAB_CI"):
		return provider.RunnerGitLab
	default:
		return provider.RunnerLocal
	}
}

// isForgejoTarget reports whether the FORGE API to talk to is Forgejo/Gitea.
//
// This is a question about the destination, so naming one is enough:
// $FORGEJO_SERVER_URL or $FORGEJO_REPOSITORY set on ANY runner means "talk
// to Forgejo", which is exactly how a GitHub-hosted repo publishes to a
// Forgejo instance.
func isForgejoTarget() bool {
	if isForgejoRunner() {
		return true
	}

	if !ciFlag("GITHUB_ACTIONS") {
		return false
	}

	return os.Getenv("FORGEJO_SERVER_URL") != "" || os.Getenv("FORGEJO_REPOSITORY") != ""
}

// isForgejoRunner reports whether we are EXECUTING on a Forgejo/Gitea runner.
//
// Split from isForgejoTarget because the two questions have different answers.
// $FORGEJO_SERVER_URL is why: a Forgejo runner sets it, but so does a workflow
// on ANY runner that publishes to Forgejo. Read as identity it makes a GitHub
// runner claim to be Forgejo, suppressing the GitHub annotations the run should
// emit (RunnerKind picks the output dialect) and making $GITHUB_TOKEN look like
// a Forgejo credential.
//
// runcontext owns the marker names and the rule; this just binds it to the
// process environment.
func isForgejoRunner() bool {
	return runcontext.ForgejoRunner(os.Getenv)
}

// ciFlag reports whether the named CI marker env var is exactly "true",
// the value GitHub/Forgejo/GitLab runners set on their respective
// markers (GITHUB_ACTIONS / GITLAB_CI).
func ciFlag(name string) bool {
	return os.Getenv(name) == "true"
}
