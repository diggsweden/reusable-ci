<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# GitLab CI Catalog — Remaining Prep

A short list of what is still missing before a first GitLab CI/CD
Catalog component can ship. The shared-core groundwork (provider
adapter, dual-emit security tools, runtime images, manifest sink) is
in place; this file tracks the YAML adapter work that's not yet done.
For architecture and design rules, see
[`gitlabsupportplan.md`](gitlabsupportplan.md).

## Remaining prep

### 1. Workflows that don't (and won't) run inside the runtime image

The runtime image is the default delivery model for build / publish /
security / orchestrator jobs. A small set of workflows stay on bare
runners with a hard reason — none of them block GitLab prep.

- **`publish-container.yml`** and **`publish-dev-container.yml`** — need
  a Docker daemon for `docker buildx`. A `container:` job can't compose
  with the daemon it would need to talk to.
- **`self-runtime-container.yml`** — builds the runtime image itself.
  Its merge job builds and invokes the current `reusable-ci` binary because
  the runtime image being tested is the output of the workflow.
- **`security-openssf-scorecard.yml`** — uses the third-party
  `ossf/scorecard-action` Docker action, which doesn't compose with a
  `container:` parent. Scorecard is GitHub-only by design (the score is
  a GitHub-repo measure), so this isn't a porting candidate either —
  it stays on GHA and is skipped on GitLab.
- **macOS workflows** (`build-xcode-ios.yml`, `publish-apple-appstore.yml`)
  — Linux runtime image not applicable. They install the Go binary on the
  macOS VM from `reusable-ci-binary-ref`.

### 2. Finish GitLab provider depth when components need it

Provider adapters are wired into the Go binary. Some GitLab behavior is still
minimal and should be completed when a Catalog component depends on it:

- GitLab job-token auth should use `JOB-TOKEN`; PAT/project/group tokens use
  `PRIVATE-TOKEN`.
- GitLab release asset handling needs real upload/link semantics, likely via
  the Generic Package Registry plus release asset links.
- Registry/package validation should be verified against real GitLab runners
  before promising it as a Catalog contract.

### 3. Catalog structure

- top-level `templates/` directory with the Catalog components
- per-component metadata layout for Catalog publication
- release path that publishes Catalog components on semver tags

### 4. In-repo Catalog testing pipeline

- repo-root `.gitlab-ci.yml`
- consume each component from `$CI_COMMIT_SHA` to test the just-pushed
  code
- verify produced artifacts and reports

### 5. First Catalog component: `security-opengrep`

Smallest useful first component. Minimum input contract:

- `stage`
- `job_name`
- `working_directory`
- `opengrep_config` (rules)
- `fail_on_severity`

Outputs: portable SARIF + `opengrep-results.gitlab-sast.json` published via
`artifacts:reports:sast`. `reusable-ci security scan opengrep` already emits
both files.

### 6. GitLab consumer example

At least one example under `examples/` showing:

- `include: component` from `diggsweden/reusable-ci`
- minimal variable / input wiring
- expected artifacts and reports

## Immediate next step

Implement `security-opengrep` as the first Catalog component. The producer
command already runs in the runtime image and emits both SARIF and the
GitLab-native report; the YAML adapter just needs to wire input variables to
`reusable-ci security scan opengrep` and surface `gitlab-sast.json` via
`artifacts:reports:sast`.
