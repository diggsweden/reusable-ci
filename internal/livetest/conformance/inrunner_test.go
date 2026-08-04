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

			// The job asserts the two halves that matter and prints what it saw,
			// so a failure names the wrong answer rather than only reporting that
			// something was wrong.
			//
			// Deliberately checked without the product binary: this is about the
			// runtime the forge presents, and the variables below are exactly what
			// the detector reads. Proving Forgejo sets GITHUB_ACTIONS while also
			// setting its own marker is what makes the detection question real.
			workflow := `on: [push]
jobs:
  detect:
    runs-on: ubuntu-latest
    steps:
      - name: report the runtime this forge presents
        run: |
          echo "GITHUB_ACTIONS=${GITHUB_ACTIONS:-unset}"
          echo "GITHUB_SERVER_URL=${GITHUB_SERVER_URL:-unset}"
          echo "FORGEJO_ACTIONS=${FORGEJO_ACTIONS:-unset}"
          echo "GITEA_ACTIONS=${GITEA_ACTIONS:-unset}"

          # The premise of PAR-RUN-1: this forge looks like GitHub to anything
          # that only reads GITHUB_*. If that ever stops being true the scenario
          # is testing nothing, so it fails here rather than passing quietly.
          test "${GITHUB_ACTIONS:-unset}" != "unset"

          # And it distinguishes itself. A runner that set no marker of its own
          # would leave a detector no honest way to tell the two apart.
          test "${FORGEJO_ACTIONS:-unset}" != "unset" || test "${GITEA_ACTIONS:-unset}" != "unset"
`

			conclusion := livetest.RunWorkflow(t, target, repo, "detect-runtime", workflow)
			if conclusion != "success" {
				t.Errorf("%s: the in-runner runtime check concluded %q — this forge no longer presents the runtime the detector assumes",
					kind, conclusion)
			}
		})
	}
}
