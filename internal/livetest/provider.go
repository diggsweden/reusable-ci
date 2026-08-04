// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// Provider builds the product's real adapter for a Target — the same type the
// CLI wires in production, so a scenario exercises the true adapter rather than
// a stand-in.
//
// Both adapters expose Env and APIBaseOverride as injection seams, so the
// target is supplied *through* them rather than by mutating the process
// environment. That matters more than convenience: scenarios for two forges run
// in the same process, and a suite that reached them via os.Setenv could not
// hold two targets at once without the providers reading each other's tokens.
// The repo is bound at construction because the adapters disagree about where
// it comes from: GitLab's UploadReleaseAsset reads $CI_PROJECT_PATH while its
// CreateRelease takes the repo as an argument, and Forgejo's context reads
// $FORGEJO_REPOSITORY. A scenario should not have to know that, so the kit
// answers every spelling from one place.
func Provider(tb TB, target Target, repo string) provider.Provider {
	tb.Helper()
	requireAccepted(tb, target)

	env := targetEnv(target, repo)

	switch target.Kind {
	case provider.PlatformForgejo:
		return &forgejo.Provider{Env: env, APIBaseOverride: target.BaseURL()}
	case provider.PlatformGitLab:
		return &gitlab.Provider{Env: env, APIBaseOverride: target.BaseURL()}
	case provider.PlatformGitHub, provider.PlatformLocal:
		tb.Fatalf("livetest: platform %q is not a live-forge target in this tier", target.Kind)
	default:
		tb.Fatalf("livetest: unknown platform %q", target.Kind)
	}

	return nil
}

// targetEnv is the adapter's whole view of the environment: the token and
// server for this target and nothing else.
//
// It is a closed map, not a fallback onto os.Getenv, so a variable that happens
// to be set in the operator's shell — a real GITHUB_TOKEN, a CI_JOB_TOKEN from
// some other pipeline — cannot reach the adapter and cannot be sent to a lab
// host. Every key an adapter may consult is answered here or answered empty.
func targetEnv(target Target, repo string) func(string) string {
	slug := target.Owner + "/" + repo
	values := map[string]string{}

	switch target.Kind {
	case provider.PlatformForgejo:
		values["FORGEJO_TOKEN"] = target.Token
		values["GITEA_TOKEN"] = target.Token
		values["FORGEJO_SERVER_URL"] = target.BaseURL()
		values["FORGEJO_REPOSITORY"] = slug
	case provider.PlatformGitLab:
		values["GITLAB_TOKEN"] = target.Token
		values["CI_SERVER_URL"] = target.BaseURL()
		values["CI_PROJECT_PATH"] = slug
	case provider.PlatformGitHub, provider.PlatformLocal:
	}

	return func(key string) string { return values[key] }
}

// RepoSlug is the owner/repo form both adapters address a repository by.
func RepoSlug(target Target, repo string) string { return target.Owner + "/" + repo }

func requireAccepted(tb TB, target Target) {
	tb.Helper()

	if !target.accepted {
		tb.Fatalf("livetest: refusing to act through a target the destructive guard never accepted")
	}
}
