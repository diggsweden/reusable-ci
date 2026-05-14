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
  - `release-dev-orchestrator.yml`
  - `pullrequest-orchestrator.yml`
- stage helpers
  - `release-prepare-stage.yml`
  - `release-build-stage.yml`
  - `release-publish-stage.yml`
  - `release-dev-build-stage.yml`
  - `release-dev-publish-stage.yml`
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
This applies to lighter dev flows too, even when they intentionally skip the full production policy layer.

## What Belongs In Planning Logic

Planning logic is for global decisions such as:

- release creation gating
- latest-release decisions
- draft-release decisions
- version-bump gating
- authorization gating
- SBOM/signing policy decisions

Planning logic should not absorb matrix-specific implementation details.

## What Should Stay Inline

Keep these inline in workflows:

- matrix-specific defaults
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

## Planned Script Structure

Treat scripts as the real module system for workflow implementation logic.

Preferred domains:

- `scripts/config/`
- `scripts/plan/`
- `scripts/release/`
- `scripts/summary/`
- `scripts/validate/`
- `scripts/container/`
- `scripts/version/`

Optional future domains if the repo grows enough:

- `scripts/build/`
- `scripts/publish/`

Guideline:

- workflows decide what runs
- scripts decide how logic is computed or reported

## Bash Script Rules

- use `set -euo pipefail`
- keep scripts single-purpose
- prefer helper functions plus `main()`
- keep side effects explicit
- add tests for new helper scripts when practical

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

## Third-Party Action Rules

Every external action (`uses: <owner>/<repo>@...`) is supply-chain surface
area: a compromised release, an unreviewed transitive dependency, or an
opaque shell step running with the workflow's secrets and `GITHUB_TOKEN`.
Treat each one as an explicit risk decision, not a convenience.

- prefer a direct CLI call or a `scripts/bootstrap/install-<tool>.sh` helper over
  a Marketplace action that wraps the same binary
- prefer baking tools into the runtime container over installing them per
  job — fewer downloads, one audit point
- when an action **must** be used, pin to a full commit SHA with a version
  comment, never a floating tag or `@main`
  (the one exception is `slsa-framework/slsa-github-generator`, which
  requires tag-based pinning for the provenance to be valid)
- before adding a new action, check if it duplicates something already
  available via a runtime-container binary, a `scripts/bootstrap/install-*.sh`
  helper, or a few lines of bash
- removing an action is a feature: smaller attack surface, fewer
  Renovate edges, fewer breakages on upstream rewrites
- this principle also feeds the GitLab portability story — anything
  expressible without a Marketplace action runs on both providers
  unchanged

The exceptions worth keeping are actions that wrap a GitHub-platform
feature with no CLI equivalent (`actions/checkout`, `actions/{up,down}load-artifact`,
`actions/cache`, `actions/attest-sbom`), or a build-system primitive that
would be substantially more code inline (`docker/build-push-action`).
Everything else should justify its own existence on each review.

## Validation Expectations

For workflow refactors, run at least:

- `actionlint .github/workflows/*.yml`
- YAML parsing of all workflows
- reusable-workflow input compatibility checks
- `bash -n` for touched helper scripts
- relevant Bats tests when scripts are added or changed

## Stop Rule

Stop refactoring when abstraction starts making the workflow harder to read.

Maintainability wins come from clearer structure, not from maximum indirection.
