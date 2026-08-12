// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"os"
	"strings"

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
func Platforms() []provider.ForgeAPI {
	return []provider.ForgeAPI{
		provider.ForgeGitHub,
		provider.ForgeGitLab,
		provider.ForgeForgejo,
		provider.ForgeLocal,
	}
}

// LiveForges is the set every live scenario iterates: the forges this suite can
// drive, intersected with the ones the contract actually selected.
//
// Two facts, and both belong here. Which forges the SUITE supports is a code
// fact — GitHub is absent by design, because this tier only mutates disposable
// hosts and there is no disposable GitHub. Which forges the ENVIRONMENT provides
// is the contract's to say, and a contract minted for one forge should not have
// every scenario iterate two.
//
// Before, only the first half was here and the second was discovered late:
// Accept skips inside the subtest with "not selected by LAB_TARGETS", so the
// loop body ran for a forge that was never coming. Deciding it here means a
// scenario iterates exactly what it can drive.
func LiveForges() []provider.ForgeAPI {
	supported := []provider.ForgeAPI{provider.ForgeGitLab, provider.ForgeForgejo}

	selected := map[string]bool{}
	for _, name := range strings.Split(os.Getenv("LAB_TARGETS"), ",") {
		selected[strings.ToLower(strings.TrimSpace(name))] = true
	}

	live := make([]provider.ForgeAPI, 0, len(supported))

	for _, forge := range supported {
		if selected[string(forge)] {
			live = append(live, forge)
		}
	}

	return live
}

// Capabilities reports what a forge claims, built from an adapter with an empty
// environment so the answer reflects the code rather than the operator's shell.
func Capabilities(forge provider.ForgeAPI) (provider.Capabilities, bool) {
	empty := func(string) string { return "" }

	var reporter provider.CapabilityReporter

	switch forge {
	case provider.ForgeGitHub:
		reporter = &github.Provider{Env: empty}
	case provider.ForgeGitLab:
		reporter = &gitlab.Provider{Env: empty}
	case provider.ForgeForgejo:
		reporter = &forgejo.Provider{Env: empty}
	case provider.ForgeLocal:
		reporter = local.New()
	default:
		return provider.Capabilities{}, false
	}

	return reporter.Capabilities(), true
}
