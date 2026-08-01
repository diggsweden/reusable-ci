<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Workflow Design Policy

This document defines the preferred structure for GitHub Actions workflows in this repository.

## Goals

- keep public workflow contracts stable
- keep orchestrators readable
- avoid mega-workflows
- move implementation logic out of YAML when it becomes noisy
- make future workflow refactors predictable and safe

## Public And Internal Boundaries

- Public high-level entry points should stay stable.
- Internal helper workflows may still be used directly by advanced consumers, but they should be treated as lower-level building blocks.
- Do not rename public orchestrator workflow files casually.

## Preferred Workflow Shape

For large orchestrators, prefer this structure:

1. `parse-config`
2. validation and release-preparation stages
3. stage-level execution jobs such as `execute-build-stage` and `execute-publish-stage`
4. release creation
5. summary jobs

The workflow should read like a pipeline, not a policy engine.

For large release flows, the public orchestrator should act as a control plane and delegate ecosystem fanout to stage-level reusable workflows.

## Planned Workflow Structure

Long term, the workflow layer should be organized by responsibility:

- public orchestrators
  - `release-orchestrator.yml`
  - `release-snapshot-orchestrator.yml`
  - `pullrequest-orchestrator.yml`
- stage helpers
  - `release-prepare-stage.yml`
  - `release-build-stage.yml`
  - `release-publish-stage.yml`
  - `release-snapshot-build-stage.yml`
  - `release-snapshot-publish-stage.yml`
  - `pullrequest-quality-stage.yml`
- validation helpers
  - `validate-*`
- release helpers
  - `release-*`
- build helpers
  - `build-*`
- publish helpers
  - `publish-*`
- quality and security helpers
  - `lint-*`
  - `security-*`

Orchestrators should remain thin and orchestration-focused.
Helper workflows should do the lower-level build, publish, release, validation, lint, and security work.

Stage-level reusable workflows are preferred when they make the public orchestrator materially easier to scan without turning contracts into giant generic input maps.
This applies to the lighter snapshot flow too, even when it intentionally skips the full production policy layer.

## What Belongs In Planning Logic

Planning logic is for global decisions such as:

- release creation gating
- latest-release decisions
- draft-release decisions
- version-bump gating
- authorization gating
- SBOM/signing policy decisions
- target membership and run gates for stage-level workflows
- compact matrix item payloads derived from `artifacts.yml`

Planning logic should not absorb GitHub Actions topology or ecosystem command
arguments. It may own matrix item contracts when those items are policy-derived
or when naming/download contracts must stay consistent across stages.

## What Should Stay Inline

Keep these inline in workflows:

- per-artifact build defaults
- per-container publish defaults
- ecosystem-specific arguments
- one-off workflow-call wiring that is clearer inline

## Script Extraction Rules

Move shell logic into scripts when one or more of these are true:

- the shell block is long enough to hide workflow intent
- the same shell pattern appears in multiple workflows
- the logic is easier to test as a script

Do not extract shell just to reduce line count.

## Shared Logic Structure

Treat the `reusable-ci` binary as the real module system for workflow
implementation logic.

Guideline:

- workflows decide what runs
- `internal/app` decides how logic is computed or reported
- `internal/domain` holds provider-neutral rules and data shapes
- `internal/adapters` owns external tools and platform APIs
- `internal/cli/commands` is the only workflow-facing command surface

## Bootstrap Script Rules

The remaining shell scripts are runtime-image build-time installers under
`scripts/bootstrap/`. For those scripts:

- use `set -euo pipefail`
- keep scripts single-purpose
- prefer helper functions plus `main()`
- keep side effects explicit
- add targeted Go black-box tests when practical

## Anchor Rules

- use anchors only when they remove real duplication
- keep anchors local and obvious
- avoid anchors for permission blocks
- avoid YAML merge-key patterns
- inline low-value anchors when they add more indirection than value

## Reusable Workflow Rules

- only pass declared `workflow_call` inputs
- keep public contracts stable unless intentionally versioned
- prefer explicit job flow over generic abstraction
- prefer small stage contracts over many per-target cross-stage outputs
- prefer structured stage payloads when they reduce top-level dependency sprawl
- when a workflow declares a signing / package / API secret in its
  `workflow_call.secrets:` block, it MUST run `reusable-ci validate
  event-context` as the first step of every secret-touching job
  (right after `Install reusable-ci binary` on plain runners; right
  after `Harden runner` on container-runtime jobs). The
  `TestPrivilegedWorkflowsHaveEventContextGuard` test enforces this
  invariant — a forwarder-only workflow whose secret-using jobs are
  all delegated `uses:` calls may be added to the test's
  `forwarderWorkflows` allowlist with an explanatory commit message.

## Third-Party Action Rules

**Goal — node-less, supply-chain-minimal, forge-portable.** Behaviour lives in
the `reusable-ci` Go binary (and a few audited bootstrap scripts), not in
Marketplace JavaScript actions. Three things follow from one move:

- **Supply chain:** every action removed is one fewer third-party release,
  Renovate edge, and opaque step running with the workflow's secrets.
- **Forge portability:** a CLI verb runs the same on GitHub Actions and Forgejo
  Actions; a GitHub-Marketplace action does not. The verb is the only path that
  works on both.
- **Security:** auditable Go with tests, primitive port signatures, and secrets
  kept out of argv — rather than transitive JS dependency trees.

The target is **not zero actions** — it is to shrink to the *irreducible* set
and make everything else a verb. The irreducible set is GitHub-platform glue
with no CLI (and no Forgejo) equivalent: `actions/cache` and
`actions/upload-artifact` (GitHub's cache backend and artifact transport).
Signing and SLSA/SBOM attestation are verbs (`container sign`, `container
attest`) — portable and verifiable with cosign on any forge, with no GitHub
attestation-API dependency at all. Everything that wraps a *binary* — including
run artifacts — has a verb.

The binary must be on `PATH` before any verb runs, which is purely a matter of
HOW the job gets it:

- **Jobs running in a reusable-ci runtime image** (`container: <runtime-image>`)
  have the binary **baked in**, so it is present from step 1 — even the *first*
  checkout can be `platform checkout`, no `actions/checkout` at all.
- **Bare-runner jobs** must install it first, and that install needs the repo —
  so they keep exactly **one** `actions/checkout` (sparse `scripts/bootstrap`) to
  bootstrap; every later step is a verb. This, plus reusable-ci's own self-build,
  is the only place `actions/checkout` is genuinely unavoidable.

So the right default for a node-less job is to run it in the runtime image.

Every external action (`uses: <owner>/<repo>@...`) is supply-chain surface
area: a compromised release, an unreviewed transitive dependency, or an
opaque shell step running with the workflow's secrets and `GITHUB_TOKEN`.
Treat each one as an explicit risk decision, not a convenience.

- prefer a direct `reusable-ci` command or a `scripts/bootstrap/install-<tool>.sh`
  helper over a Marketplace action that wraps the same binary
- prefer baking tools into the runtime container over installing them per
  job — fewer downloads, one audit point
- when an action **must** be used, pin to a full commit SHA with a version
  comment, never a floating tag or `@main`
- before adding a new action, check if it duplicates something already
  available via a runtime-container binary, a `scripts/bootstrap/install-*.sh`
  helper, or a few lines of bash
- removing an action is a feature: smaller attack surface, fewer
  Renovate edges, fewer breakages on upstream rewrites
- this principle also feeds the GitLab portability story — anything
  expressible without a Marketplace action runs on both providers
  unchanged

Beyond the irreducible platform set above there are no kept build-system action
exceptions — the image build itself is now a CLI verb (`container build`).
Everything else should justify its own existence on each review.

Replaced in the workflows (node-less, and so forge-portable):
- `docker/setup-buildx-action` + `docker/build-push-action` → `container build`
  (buildah builds; the ggcr adapter pushes tagless by digest — daemonless, no
  BuildKit and no GitHub-marketplace action; the multi-arch index is assembled by
  `container manifest merge`).
- `github/codeql-action/upload-sarif` → `security report upload-sarif` (stamps
  the category into each run's `automationDetails.id`, the field Code Scanning
  keys analyses on).
- `docker/login-action` → `container login` (writes the shared `{"auths":…}`
  config read by docker, podman/buildah, skopeo, and cosign). The one remaining
  use is the provenance job, where the only alternative is to *add*
  `actions/checkout` to bootstrap — no net win.
- `actions/download-artifact` → `artifact download` (node-less, forge-portable).
  Download uses the GitHub REST artifacts API, which a `run:` step *can* reach, so
  the CLI verb stays. Flag mapping via `env:`: `name:`→`$ARTIFACT_NAME`; `path:`→
  `$ARTIFACT_DIR`; `pattern:`→`$ARTIFACT_PATTERN`; `merge-multiple:`→
  `$ARTIFACT_MERGE_MULTIPLE`. The REST call needs auth, and `$GITHUB_TOKEN` is
  **not** auto-present in run steps, so each download step passes
  `GH_TOKEN: ${{ github.token }}`.
- Artifact **upload** stays `actions/upload-artifact` — deliberately **not** a CLI
  verb on GitHub. Upload goes only through the Actions "results" service, gated by
  `ACTIONS_RUNTIME_TOKEN`, which the runner injects into *actions* but **never into
  `run:` steps** — a run-step CLI cannot authenticate (and there is no REST upload
  endpoint). The `reusable-ci artifact upload` verb still exists for Forgejo (whose
  runner *does* expose the runtime token to run steps) and for interop with the v4
  store, but every GitHub workflow uploads via the action. `with:` mapping is 1:1:
  `name`, `path` (glob `*`/`**`/`[set]`/`!exclude`, multi-line), `if-no-files-found`,
  `retention-days`, `include-hidden-files`.

Verb exists, but adoption is gated on a CI proof (not yet swapped):
- `actions/checkout` → `platform checkout` (`--fetch-tags`, `--sparse` cone +
  partial clone, `--path`). Feature-complete; runtime-image jobs can use it even
  for the first checkout. The bare-runner bootstrap checkout stays (see above).

## Validation Expectations

For workflow refactors, run at least:

- `actionlint .github/workflows/*.yml`
- YAML parsing of all workflows
- reusable-workflow input compatibility checks
- `bash -n` for touched bootstrap scripts
- relevant Go tests when binary behavior is added or changed

## Stop Rule

Stop refactoring when abstraction starts making the workflow harder to read.

Maintainability wins come from clearer structure, not from maximum indirection.
