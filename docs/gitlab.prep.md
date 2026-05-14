<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# GitLab CI Catalog — Remaining Prep

A short, current list of what is still missing before a first GitLab
CI/CD Catalog component can ship. Live status, not history.

For what's already delivered, see [`gitlab-completed.md`](gitlab-completed.md).
For architecture and design rules, see [`gitlabsupportplan.md`](gitlabsupportplan.md).

## Remaining prep

### 1. Workflows that don't (and won't) run inside the runtime image

The runtime image is the default delivery model for build / publish /
security / orchestrator jobs. A small set of workflows stay on bare
runners with a hard reason — none of them block GitLab prep.

- **`publish-container.yml`** and **`publish-dev-container.yml`** — need
  a Docker daemon for `docker buildx`. A `container:` job can't compose
  with the daemon it would need to talk to.
- **`self-runtime-container.yml`** — builds the runtime image itself.
  Its `publish-merge` job sparse-checks-out `scripts/` so the in-script
  metadata helper (`compute-image-metadata.sh`) is available.
- **`security-openssf-scorecard.yml`** — uses the third-party
  `ossf/scorecard-action` Docker action, which doesn't compose with a
  `container:` parent. Scorecard is GitHub-only by design (the score is
  a GitHub-repo measure), so this isn't a porting candidate either —
  it stays on GHA and is skipped on GitLab.
- **macOS workflows** (`build-xcode-ios.yml`, `publish-apple-appstore.yml`)
  — Linux runtime image not applicable. They use the
  `scripts-ref` + `scripts-archive-url` pattern; see
  [`gitlab-completed.md`](gitlab-completed.md) for details.

### 2. Implement the GitLab provider stubs

Provider dispatch is wired up. Three stub call sites still error
cleanly when `CI_PLATFORM=gitlab`. The first GitLab Catalog component
that needs each will replace its stub with a real `glab` / GitLab REST
API implementation:

- `scripts/release/providers/gitlab.sh` — `create_release`
- `scripts/validate/providers/gitlab.sh` — `validate_token`,
  `validate_bot_permissions`
- `scripts/container/compute-image-metadata.sh` — the `gitlab` branch in
  `resolve_context()` (read `CI_COMMIT_TAG` / `CI_COMMIT_BRANCH` /
  `CI_MERGE_REQUEST_IID` / `CI_COMMIT_SHA` / `CI_PROJECT_PATH`).

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

Outputs: portable SARIF + `opengrep-results.gitlab-sast.json` published
via `artifacts:reports:sast`. The producer script already emits both
files.

### 6. GitLab consumer example

At least one example under `examples/` showing:

- `include: component` from `diggsweden/reusable-ci`
- minimal variable / input wiring
- expected artifacts and reports

## Immediate next step

Implement `security-opengrep` as the first Catalog component. The
producer script already runs in the runtime image and emits both SARIF
and the GitLab-native report; the YAML adapter just needs to wire input
variables to the script and surface `gitlab-sast.json` via
`artifacts:reports:sast`.
