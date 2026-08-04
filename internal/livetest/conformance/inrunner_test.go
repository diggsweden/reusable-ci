// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// PAR-RUN-1: the product identifies its runtime correctly from inside a job.
//
// This is the assertion no host-run test can make. Forgejo Actions sets GITHUB_*
// variables — GITHUB_ACTIONS, GITHUB_REPOSITORY, GITHUB_OUTPUT — because it
// implements the same workflow syntax. So a detector that keys on those alone
// resolves a Forgejo runner as GitHub, and then emits GitHub's ::error::
// annotations into logs that render them as plain text, writes step summaries
// nowhere, and reports a runner dialect the rest of the product branches on.
//
// Every tier below this one either pins the runner (Tier A does, deliberately,
// for determinism) or asks a forge API that has no opinion about runners. Only a
// real job can answer it, which is the entire reason the lab registers runners.
//
// The check runs INSIDE the job and the job fails itself when wrong, so the run's
// conclusion is the result. That avoids parsing job logs, whose format is each
// forge's private business and differs between them — and it means the failure
// message a maintainer sees is the one the job printed, not one reconstructed
// from a scrape.

import (
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"

	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

func TestInRunner_DetectsItsOwnRuntime(t *testing.T) {
	for _, kind := range forgesClaiming(t, alwaysValidatesTokens, "an artifact store") {
		if !livetest.RunsInRunner(kind) {
			t.Logf("SKIP %s: the in-runner tier does not drive this forge yet", kind)

			continue
		}

		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			repo := livetest.NewScratchRepo(t, target, "inrunner")

			// The workflow itself must differ: these are different runtimes, and
			// pretending otherwise is what this tier exists to disprove. What is
			// held constant is the claim — each forge presents a runtime a
			// detector can identify unambiguously — so the scenario is one
			// assertion expressed in each dialect rather than two tests.
			conclusion := livetest.RunWorkflow(t, target, repo, "detect-runtime", runtimeProbe(kind))
			if conclusion != "success" {
				t.Errorf("%s: the in-runner runtime check concluded %q — this forge no longer presents the runtime the detector assumes",
					kind, conclusion)
			}
		})
	}
}

// runtimeProbe is the same question in each forge's dialect: does this runtime
// identify itself in a way the detector can trust?
//
// The answers differ in a way worth stating. Forgejo sets GITHUB_ACTIONS because
// it implements the same workflow syntax, so it must ALSO set a marker of its
// own or nothing could tell the two apart — the job checks both. GitLab shares
// no vocabulary with Actions, so its claim is the mirror image: it identifies
// itself, and must NOT look like GitHub.
//
// Each probe also pins its own premise. If Forgejo stopped presenting
// GITHUB_ACTIONS, or GitLab started, the detection question would have changed
// shape and the scenario would be worth rewriting rather than quietly passing.
func runtimeProbe(kind provider.Platform) string {
	if kind == provider.PlatformGitLab {
		return `detect:
  script:
    - echo "GITLAB_CI=${GITLAB_CI:-unset}"
    - echo "GITHUB_ACTIONS=${GITHUB_ACTIONS:-unset}"
    - test "${GITLAB_CI:-unset}" != "unset"
    - test "${GITHUB_ACTIONS:-unset}" = "unset"
`
	}

	return `on: [push]
jobs:
  detect:
    runs-on: ubuntu-latest
    steps:
      - name: report the runtime this forge presents
        run: |
          echo "GITHUB_ACTIONS=${GITHUB_ACTIONS:-unset}"
          echo "FORGEJO_ACTIONS=${FORGEJO_ACTIONS:-unset}"
          echo "GITEA_ACTIONS=${GITEA_ACTIONS:-unset}"

          test "${GITHUB_ACTIONS:-unset}" != "unset"
          test "${FORGEJO_ACTIONS:-unset}" != "unset" || test "${GITEA_ACTIONS:-unset}" != "unset"
`
}
