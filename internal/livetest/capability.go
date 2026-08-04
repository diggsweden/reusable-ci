// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/forgejo"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/github"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gitlab"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/local"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// Reading what a forge claims needs no forge: Capabilities() is derived from
// which roles an adapter implements, so it is answerable offline. That is why
// this file carries no build tag and no guard — a capability question is not a
// mutation, and the docs-parity test that asks it must run on every commit
// rather than only when a lab happens to be up.

// Platforms is every forge the product has an adapter for, in the order the
// published capability matrix lists them.
func Platforms() []provider.Platform {
	return []provider.Platform{
		provider.PlatformGitHub,
		provider.PlatformGitLab,
		provider.PlatformForgejo,
		provider.PlatformLocal,
	}
}

// LiveForges is the subset a local lab can actually host, and therefore the set
// every live scenario iterates. GitHub is absent by design: this tier only
// mutates disposable hosts.
func LiveForges() []provider.Platform {
	return []provider.Platform{provider.PlatformGitLab, provider.PlatformForgejo}
}

// Capabilities reports what a forge claims, built from an adapter with an empty
// environment so the answer reflects the code rather than the operator's shell.
func Capabilities(kind provider.Platform) (provider.Capabilities, bool) {
	empty := func(string) string { return "" }

	var reporter provider.CapabilityReporter

	switch kind {
	case provider.PlatformGitHub:
		reporter = &github.Provider{Env: empty}
	case provider.PlatformGitLab:
		reporter = &gitlab.Provider{Env: empty}
	case provider.PlatformForgejo:
		reporter = &forgejo.Provider{Env: empty}
	case provider.PlatformLocal:
		reporter = local.New()
	default:
		return provider.Capabilities{}, false
	}

	return reporter.Capabilities(), true
}
