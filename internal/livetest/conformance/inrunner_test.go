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
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"

	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

func TestInRunner_DetectsItsOwnRuntime(t *testing.T) {
	for _, kind := range livetest.ForgesMeeting(t, forgesClaiming(t, alwaysValidatesTokens, "an artifact store"), livetest.NeedsInRunner) {

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

// PAR-RUN-2: the product, running inside a real job, reports the runtime it is
// actually in.
//
// PAR-RUN-1 proved the forges present distinguishable runtimes. This proves
// reusable-ci draws the right conclusion from that, which is the claim anyone
// actually depends on — the runner dialect decides whether annotations render,
// whether step summaries land anywhere, and which output-file convention is
// written.
//
// The binary reaches the job as a release asset on the scratch repository, which
// needs no new machinery: PAR-REL-1/2 already prove assets round-trip
// byte-identically on both forges, the repositories are public so the job fetches
// without a credential, and nothing about it is lab-specific — the same shape
// works on k3s and against a real forge.
func TestInRunner_ProductDetectsItsRunner(t *testing.T) {
	const tag = "v0.0.1-inrunner"

	for _, kind := range livetest.ForgesMeeting(t, forgesClaiming(t, alwaysValidatesTokens, "releases"), livetest.NeedsInRunner) {

		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			repo := livetest.NewScratchRepo(t, target, "inrunner-product")

			livetest.PrepareTag(t, target, repo, tag)
			livetest.PublishBinaryAsset(t, target, repo, tag, t.TempDir())

			assetURL := livetest.ReleaseAssetURL(t, target, repo, tag, "reusable-ci")

			// The job asserts and fails itself, so the run's conclusion is the
			// result; the expected dialect is passed in rather than derived in
			// the job, because what is under test is the product's answer, not
			// the fixture's cleverness.
			conclusion := livetest.RunWorkflow(t, target, repo, "detect-runner",
				productProbe(kind, assetURL, expectedRunner(kind)))
			if conclusion != "success" {
				t.Errorf("%s: the product's runner detection concluded %q inside a real job — it is reporting the wrong runtime, so annotations and step summaries go to the wrong place",
					kind, conclusion)
			}
		})
	}
}

// expectedRunner is the dialect each forge's runner must be recognised as. The
// interesting one is Forgejo: it is NOT github, despite presenting GITHUB_*.
func expectedRunner(kind provider.Platform) string {
	if kind == provider.PlatformGitLab {
		return "gitlab"
	}

	return "forgejo"
}

func productProbe(kind provider.Platform, assetURL, want string) string {
	if kind == provider.PlatformGitLab {
		return `detect:
  image: ` + livetest.ProbeImage + `
  script:
    - |
      ` + indent(livetest.ProbePrelude(assetURL), 6) + `
      run_product doctor --json > report.json || true
      cat report.json
      grep -qE '"runner":[[:space:]]*"` + want + `"' report.json
`
	}

	return `on: [push]
jobs:
  detect:
    runs-on: ubuntu-latest
    steps:
      - name: the product reports its own runner
        run: |
          ` + indent(livetest.ProbePrelude(assetURL), 10) + `
          run_product doctor --json > report.json || true
          cat report.json
          grep -qE '"runner":[[:space:]]*"` + want + `"' report.json
`
}

// PAR-RUN-3: the annotation dialect follows the runner, not the workflow syntax.
//
// GitHub workflow commands (::error::, ::warning::, ::group::) are rendered by
// GitHub and by nothing else. Forgejo implements the same workflow *syntax* while
// rendering none of them, so a runner mis-detected as GitHub emits `::error::`
// into a log that shows it verbatim — the diagnostic becomes noise, and the
// Annotations pane a maintainer looks at stays empty.
//
// That is the concrete damage PAR-RUN-2's detection prevents, so this asserts it
// where it actually happens: in a job, in the log, on a forge that is not GitHub.
// The claim is deliberately negative — no GitHub dialect here — because each
// non-GitHub forge renders its own way and prescribing the positive form would
// pin the wrong thing.
func TestInRunner_NoGitHubAnnotationsOnOtherForges(t *testing.T) {
	const tag = "v0.0.2-annotations"

	for _, kind := range livetest.ForgesMeeting(t, forgesClaiming(t, alwaysValidatesTokens, "releases"), livetest.NeedsInRunner) {

		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			repo := livetest.NewScratchRepo(t, target, "inrunner-annot")

			livetest.PrepareTag(t, target, repo, tag)
			livetest.PublishBinaryAsset(t, target, repo, tag, t.TempDir())

			assetURL := livetest.ReleaseAssetURL(t, target, repo, tag, "reusable-ci")

			conclusion := livetest.RunWorkflow(t, target, repo, "annotation-dialect",
				annotationProbe(kind, assetURL))
			if conclusion != "success" {
				t.Errorf("%s: run concluded %q — the product emitted GitHub workflow commands on a runner that does not render them, so its diagnostics reach the log as literal text",
					kind, conclusion)
			}
		})
	}
}

// annotationProbe drives a verb that reports through the annotator and fails the
// job if GitHub's dialect appears.
//
// `doctor` is the vehicle because it always has something to say and never
// mutates anything, so the probe stays about the dialect. Its exit status is
// ignored: whether this lab passes a health check is not the claim.
func annotationProbe(kind provider.Platform, assetURL string) string {
	check := `./reusable-ci doctor > out.txt 2>&1 || true
cat out.txt

# A negative assertion over an empty file proves nothing, and a verb that
# printed nothing would pass it. Require output before judging its dialect.
test -s out.txt

if grep -qE '::(error|warning|notice|group|endgroup)::' out.txt; then
  echo "FAIL: GitHub workflow commands emitted on a runner that does not render them"
  exit 1
fi`

	if kind == provider.PlatformGitLab {
		return `detect:
  image: ` + livetest.ProbeImage + `
  script:
    - |
      ` + indent(livetest.TrustLabCA(), 6) + `
    - curl -fsSL -o reusable-ci "` + assetURL + `"
    - chmod +x reusable-ci
    - |
      ` + strings.ReplaceAll(check, "\n", "\n      ") + `
`
	}

	return `on: [push]
jobs:
  detect:
    runs-on: ubuntu-latest
    steps:
      - name: the annotation dialect must match the runner
        run: |
          ` + strings.ReplaceAll(check, "\n", "\n          ") + `
`
}

// indent re-indents a multi-line shell block so it survives being spliced into
// YAML, where a stray column changes meaning.
func indent(block string, spaces int) string {
	pad := strings.Repeat(" ", spaces)

	return strings.ReplaceAll(block, "\n", "\n"+pad)
}

// PAR-RUN-4: a step summary reaches a reader on every runner.
//
// Only GitHub has a summary pane. Forgejo documents no job-summary variable and
// renders none (go-gitea/gitea#27898), so the product routes its summary to the
// job log; GitLab has no native equivalent and takes the file the pipeline
// nominates. Three destinations for one intent — and the intent is what matters,
// because a summary that goes nowhere is indistinguishable from one never
// written, and both look like success.
//
// The verb is chosen for having something to say: `report build go` renders a
// table from its flags alone, so the probe cannot pass on an empty document. The
// assertion is on that content, not on byte count, for the same reason.
func TestInRunner_StepSummaryReachesAReader(t *testing.T) {
	const tag = "v0.0.3-summary"

	for _, kind := range livetest.ForgesMeeting(t, forgesClaiming(t, alwaysValidatesTokens, "releases"), livetest.NeedsInRunner) {

		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			repo := livetest.NewScratchRepo(t, target, "inrunner-summary")

			livetest.PrepareTag(t, target, repo, tag)
			livetest.PublishBinaryAsset(t, target, repo, tag, t.TempDir())

			assetURL := livetest.ReleaseAssetURL(t, target, repo, tag, "reusable-ci")

			conclusion := livetest.RunWorkflow(t, target, repo, "step-summary",
				summaryProbe(kind, assetURL))
			if conclusion != "success" {
				t.Errorf("%s: run concluded %q — the step summary reached no reader, so a job that reported one produced nothing anybody sees",
					kind, conclusion)
			}
		})
	}
}

// summaryProbe writes a summary and checks the destination that runner is
// designed to use.
//
// GitLab is handed a CI_SUMMARY_FILE because GitLab has no native summary and
// the product writes to the file the pipeline nominates; asserting on a file the
// pipeline never named would be testing the fixture. Forgejo is deliberately
// given none — the claim there is exactly that the summary falls back to the job
// log rather than vanishing.
func summaryProbe(kind provider.Platform, assetURL string) string {
	const report = `run_product report build go --binary-name demo \
  --module example.com/demo --platforms linux/amd64 --version v1.0.0`

	if kind == provider.PlatformGitLab {
		return `detect:
  image: ` + livetest.ProbeImage + `
  variables:
    CI_SUMMARY_FILE: summary.md
  script:
    - |
      ` + indent(livetest.ProbePrelude(assetURL), 6) + `
      ` + indent(report, 6) + `
      cat summary.md
      grep -q 'Go Build Summary' summary.md
`
	}

	return `on: [push]
jobs:
  detect:
    runs-on: ubuntu-latest
    steps:
      - name: the summary must reach the job log when the runner renders none
        run: |
          ` + indent(livetest.ProbePrelude(assetURL), 10) + `

          # No GITHUB_STEP_SUMMARY is exported: the claim is the fallback.
          unset GITHUB_STEP_SUMMARY
          ` + indent(report, 10) + ` > summary-log.txt
          cat summary-log.txt
          grep -q 'Go Build Summary' summary-log.txt
`
}

// PAR-RUN-5: step outputs land where the runner will actually read them.
//
// This is the third convention the runner dialect decides, after annotations and
// summaries, and the only one whose failure is completely silent. A summary that
// goes nowhere is invisible; an annotation in the wrong dialect is at least
// visible as noise. But an output written to a file the runner does not consume
// produces no error anywhere — the job succeeds, and the next step reads an empty
// variable and carries on with a default. That is a wrong release, not a failed
// one.
//
// The two forges disagree about where outputs go, which is the whole point:
//
//   - Forgejo's native variable is $FORGEJO_OUTPUT, with $GITHUB_OUTPUT kept as a
//     compatibility alias since Forgejo Runner 7.0.0, so the product prefers the
//     native name and falls back to the alias;
//   - GitLab has no per-step output file at all. Values reach later jobs through
//     a dotenv report, so the product writes to the path the pipeline nominates
//     in $CI_OUTPUT and the pipeline declares it as artifacts:reports:dotenv.
//
// They also disagree about the *key*, which is the part a consumer trips over.
// GitLab dotenv names must be upper snake case, so the product translates its
// lower-hyphenated keys on the way out: `version-no-v` is written `VERSION_NO_V`
// on GitLab and stays `version-no-v` on Forgejo. Parity here is therefore not
// "the same bytes in both files" — it is the same value, under the name each
// runner can actually resolve. A scenario asserting one spelling on both forges
// would be asserting a bug.
//
// `release resolve metadata` is the verb because it is pure computation — no
// forge call, no network — so a failure is about the sink and nothing else. The
// version is distinctive so a match cannot come from anything already in the file.
func TestInRunner_StepOutputsReachTheRunnersOutputFile(t *testing.T) {
	const tag = "v0.0.6-outputs"

	for _, kind := range livetest.ForgesMeeting(t, forgesClaiming(t, alwaysValidatesTokens, "releases"), livetest.NeedsInRunner) {

		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)
			repo := livetest.NewScratchRepo(t, target, "inrunner-output")

			livetest.PrepareTag(t, target, repo, tag)
			livetest.PublishBinaryAsset(t, target, repo, tag, t.TempDir())

			assetURL := livetest.ReleaseAssetURL(t, target, repo, tag, "reusable-ci")

			conclusion := livetest.RunWorkflow(t, target, repo, "step-outputs",
				outputFileProbe(kind, assetURL))
			if conclusion != "success" {
				t.Errorf("%s: run concluded %q — step outputs did not reach the file this runner reads, so a later step sees an empty value and silently uses its default",
					kind, conclusion)
			}
		})
	}
}

// outputFileProbe runs an output-writing verb and reads the runner's own output
// file back.
//
// The Forgejo half also settles which variable wins. The runner sets both names,
// so the probe compares the two paths rather than assuming: when they resolve to
// the same file the question is moot and it says so, and when they differ the
// native $FORGEJO_OUTPUT is required to be the one written. Asserting a
// preference that the runner's own configuration makes unobservable would be
// testing the fixture.
func outputFileProbe(kind provider.Platform, assetURL string) string {
	const resolve = `run_product release resolve metadata \
  --version v9.9.9-parrun5 --repository livetest/outputs`

	if kind == provider.PlatformGitLab {
		// GitLab does not provide an output file; the pipeline nominates one,
		// which is the documented contract rather than a fixture convenience.
		return `detect:
  image: ` + livetest.ProbeImage + `
  variables:
    CI_OUTPUT: build.env
  script:
    - |
      ` + indent(livetest.ProbePrelude(assetURL), 6) + `
      ` + indent(resolve, 6) + `

      echo "--- $CI_OUTPUT ---"
      cat "$CI_OUTPUT"

      # Upper snake case, because that is what GitLab dotenv accepts and what a
      # later job will reference as $VERSION_NO_V.
      grep -q '^VERSION=v9.9.9-parrun5$' "$CI_OUTPUT"
      grep -q '^VERSION_NO_V=9.9.9-parrun5$' "$CI_OUTPUT"
      grep -q '^PROJECT_NAME=outputs$' "$CI_OUTPUT"
`
	}

	return `on: [push]
jobs:
  detect:
    runs-on: ubuntu-latest
    steps:
      - name: outputs must land in the file this runner reads
        run: |
          ` + indent(livetest.ProbePrelude(assetURL), 10) + `

          echo "FORGEJO_OUTPUT=${FORGEJO_OUTPUT:-unset}"
          echo "GITHUB_OUTPUT=${GITHUB_OUTPUT:-unset}"

          if [ -z "${FORGEJO_OUTPUT:-}" ] && [ -z "${GITHUB_OUTPUT:-}" ]; then
            echo "FAIL: this runner provided no step-output file under either name"
            exit 1
          fi

          ` + indent(resolve, 10) + `

          # The native name is preferred; the alias is the fallback. Which file to
          # read back is therefore the same decision the product just made.
          target="${FORGEJO_OUTPUT:-$GITHUB_OUTPUT}"
          echo "--- $target ---"
          cat "$target"

          grep -q '^version=v9.9.9-parrun5$' "$target"
          grep -q '^version-no-v=9.9.9-parrun5$' "$target"
          grep -q '^project-name=outputs$' "$target"

          # When the runner points both names at one file the preference is
          # unobservable, and claiming to have proven it would be a lie.
          if [ -n "${FORGEJO_OUTPUT:-}" ] && [ -n "${GITHUB_OUTPUT:-}" ]; then
            if [ "$FORGEJO_OUTPUT" = "$GITHUB_OUTPUT" ]; then
              echo "NOTE: both names point at one file; native-vs-alias preference is not observable here"
            elif grep -q '^version=v9.9.9-parrun5$' "$GITHUB_OUTPUT"; then
              echo "FAIL: the value went to the \$GITHUB_OUTPUT alias while \$FORGEJO_OUTPUT is set"
              exit 1
            fi
          fi
`
}
