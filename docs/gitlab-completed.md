<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# GitLab Support — Completed Prep

What's already delivered toward GitLab CI/CD Catalog support. For what's
left, see [`gitlab.prep.md`](gitlab.prep.md). For architecture and design
rules, see [`gitlabsupportplan.md`](gitlabsupportplan.md).

This doc captures the *state achieved* — not a changelog. Move items here
as they land; remove them when the next refactor invalidates them.

## Shared-core platform-neutralisation

- **1a — Logging helpers.** `ci_log_error/warning` in `output.sh`; all scripts migrated. Zero bare `::error::` outputs outside `output.sh`.
- **1b — `CI_PLATFORM` detection.** The Go provider/platform layer resolves `github`, `gitlab`, or `local` and normalises the equivalent CI context (`CI_COMMIT`, `CI_REPO`, `CI_BRANCH`, `CI_PR_BASE_REF`, `CI_REF_NAME`, `CI_RUN_URL`, `CI_RESULTS_DIR`, etc.) from the platform's native variables.
- **1c — URL helpers.** `ci_release_url` / `ci_packages_url` / `ci_docs_url` in `output.sh`; hardcoded URLs removed from summary/validate scripts; `> [!WARNING]` admonitions normalised to `> **Warning:**`.
- **1d — Variable renames.** GitHub-specific names made platform-neutral across scripts, workflows, tests, and docs:
  - `SHOULD_CREATE_GITHUB_RELEASE` → `SHOULD_CREATE_RELEASE`
  - `USE_GITHUB_TOKEN` → `USE_CI_TOKEN` (incl. `workflow_call` input `use-github-token` → `use-ci-token`)
  - `GITHUB_REGISTRY` → `CI_REGISTRY`
  - `PUBLISH_MAVEN_GITHUB_RESULT` → `PUBLISH_MAVEN_REGISTRY_RESULT`
- **1e — Provider dispatch.** GitHub-specific behaviour is isolated behind `scripts/*/providers/{github,gitlab}.sh` keyed on `CI_PLATFORM`:
  - `scripts/release/create-release.sh` (entrypoint) → `providers/github.sh` (`gh release …`)
  - `scripts/validate/token.sh` (entrypoint) → `providers/github.sh` (`curl` against `api.github.com`)
  - `scripts/validate/bot-permissions.sh` (entrypoint) → `providers/github.sh` (`gh api`)
  - GitLab provider stubs error cleanly until the first GitLab Catalog component implements them.
- **1f — Inline workflow logic extracted.** Six inline shell blocks lifted into `scripts/build/`, `scripts/publish/`, `scripts/release/`, `scripts/validate/`; five workflow YAMLs updated to call the extracted scripts.
- **1g — File-based stage manifests.** `.ci-results/<stage>-result.json` is written through the manifest sink rooted at `CI_RESULTS_DIR`. All six stage workflows have a matching `Upload <stage> stage manifests` step. Dual-write with CI output sinks is intentional — same-job step consumers use the platform's scalar sink, cross-job/cross-platform consumers read the manifest file.

## Portable security/SBOM tooling

Producers emit *both* the GitHub-consumed format (SARIF) and the GitLab-native
report file. Platform YAML decides which to surface.

- **OpenGrep SAST.** `scripts/security/run-opengrep.sh` emits JSON, SARIF, plain text, and `opengrep-results.gitlab-sast.json`.
- **Trivy dependency scan.** `scripts/security/scan-dependencies.sh` produces JSON once, then derives SARIF via `trivy convert` and the GitLab dependency-scanning report via `scripts/security/trivy-to-gitlab-dep.sh`. `gl-dependency-scanning-report.json` is uploaded as artifact in `security-dependency-review.yml`.
- **Trivy container scan.** `publish-container.yml` runs Trivy once in JSON mode; SARIF and the GitLab container-scanning report are derived from that JSON via `trivy convert` and `scripts/security/trivy-to-gitlab-container.sh`. `gl-container-scanning-report.json` is uploaded per-container/per-arch.
- **Syft SBOM.** `scripts/sbom/` produces CycloneDX + SPDX. CycloneDX is exactly what GitLab `artifacts:reports:cyclonedx` ingests — no GitLab-specific transform needed.
- **Tool installers.** `scripts/bootstrap/install-{opengrep,syft,trivy,yq}.sh` pinned via Renovate datasource comments.

## Runtime container image

`containers/runtime/Containerfile` (built and published by
`.github/workflows/self-runtime-container.yml`):

- `reusable-ci-runtime-base` — `bash`, `ca-certificates`, `curl`, `git`,
  `gnupg`, `gzip`, `jq`, `tar`, `unzip`, `xz`, `zip`, `yq`, `git-cliff`,
  `gh`, `glab`. Bundles `templates/` at `/opt/reusable-ci/templates/` so
  `generate-changelog.yml` no longer needs a templates checkout. Covers
  config / plan / release / orchestrator / changelog / signing /
  provider-dispatch jobs on both GitHub and GitLab.
- `reusable-ci-runtime` — base + `opengrep`, `syft`, `trivy`. For security
  and SBOM jobs.

The presence of both `gh` and `glab` is intentional: provider dispatch
keeps platform-specific calls behind `providers/{github,gitlab}.sh`, but
the runtime image carries both CLIs so either provider works inside the
same image. The unused CLI is dead weight on the other platform — the
symmetric cost of supporting both.

Built and verified on PRs (matrix over both stages, smoke-tested), published
to GHCR on push to `main`, on semver tags, and on a weekly schedule. Tag
pushes always rebuild regardless of which files the tag commit touched, so
release tags reliably get a matching `:vX.Y.Z` image.

### Consumers — workflows that now run inside the runtime image

- `security-opengrep.yml`, `security-dependency-review.yml` — full image
  (need `opengrep` / `trivy` on PATH).
- `release-build-stage.yml`, `release-prepare-stage.yml`,
  `release-publish-stage.yml`, `release-dev-build-stage.yml`,
  `release-dev-publish-stage.yml`, `pullrequest-quality-stage.yml` — base
  image for the summary/aggregator jobs (just need `bash`, `jq`, `yq`).
  `release-dev-publish-stage.yml`'s `generate-dev-sboms` job uses the full
  image (needs `syft`).
- `release-orchestrator.yml`, `release-dev-orchestrator.yml`,
  `pullrequest-orchestrator.yml` — base image for their `*-summary` jobs.
- `generate-changelog.yml` — base image (needs `git-cliff`).
- `validate-release-prerequisites.yml` — base image (needs `gh` for the
  bot-permission check, `gpg` for tag-signature verification).
- `release-create-github.yml` — full image (needs `syft` for SBOM
  generation alongside `gh` and `gpg`).
- `release-orchestrator.yml`'s `parse-config`, `release-dev-orchestrator.yml`'s
  `setup-dev`, and `pullrequest-orchestrator.yml`'s `compose-pr-interface` —
  base image. Bootstrap jobs run inside the runtime container; the
  `parse-config` `scripts-ref` output is now resolved via
  `git ls-remote` (no full checkout needed) so still-on-script-checkout
  downstream stages keep their stable commit pin.

### macOS workflows — different distribution model

macOS runners (on either GitHub or GitLab) run as VMs, not Linux
containers. The runtime image doesn't apply. `build-xcode-ios.yml` and
`publish-apple-appstore.yml` instead take two inputs:

- `scripts-ref` — version of reusable-ci scripts to fetch (branch, tag,
  or SHA). Pin to the same value as the runtime-image tag for coherence.
- `scripts-archive-url` — URL template with `{ref}` substitution.
  Defaults to `https://github.com/diggsweden/reusable-ci/archive/{ref}.tar.gz`.
  Override to a mirror URL (e.g. a GitLab mirror) for sovereign
  environments that should not reach github.com.

The fetch step `curl`s the archive and extracts it under `.github-shared/`,
preserving the `bash .github-shared/scripts/...` paths existing macOS
scripts use.

In every migrated job: the `Checkout reusable-ci scripts` step is gone,
scripts are read from `/opt/reusable-ci/scripts/`, and tool installs are
skipped (tools are baked in).

Each migrated workflow has a `runtime-image` input with a default
appropriate for its jobs. External consumers pinning to a specific
release should override `runtime-image` at every entry point they call
(orchestrators, stages, or leaves). Pin to a digest or to the `:vX.Y.Z`
tag matching the workflow ref.

## Third-party action minimisation

Twenty-two distinct external GitHub Actions in `.github/workflows/` are
down to fourteen — eight removed in favour of portable scripts. Each
replacement ships as a script so the future GitLab CI adapter calls the
same code. Policy codified in
[`workflow-design-policy.md` § Third-Party Action Rules](workflow-design-policy.md#third-party-action-rules).

The single most consequential drop:

- **`docker/metadata-action`** → `scripts/container/compute-image-metadata.sh`.
  The action encoded GitHub event semantics (`github.ref`, `ref_type`,
  `event_name`, PR refs, repo metadata via the GitHub API) into a bag of
  tag rules. The replacement reads provider context via the Go provider
  adapters, so GitHub/GitLab/local normalization stays in one place. Go tests cover
  every supported tag type (raw / ref-branch / ref-tag / ref-pr /
  semver-{version,major.minor,major} / sha with `{{branch}}` template),
  `enable=` gating, primary-version selection by priority, OCI labels,
  JSON shape, and all error paths.

The rest:

- **`crazy-max/ghaction-import-gpg`** → `scripts/release/import-gpg-key.sh` +
  `cleanup-gpg-key.sh`. Source-analysed at the pinned SHA; behaviour
  preserved (armored/base64 key detection, mode-0600 tempfile, gpg-agent
  passphrase preset against every subkey keygrip via stdin-not-argv,
  optional `git config` writes, post-step keyring delete + agent kill).
  Integration-tagged Go tests run against a real throwaway GPG key in an isolated
  `GNUPGHOME`. The post-step cleanup is now an explicit `if: always()`
  step at the end of each of the four workflows that imported keys.
- **`stefanzweifel/git-auto-commit-action`** →
  `scripts/version/commit-and-push.sh`. Go tests cover successful
  commit + push, no-op when nothing changes, signoff line, multi-line
  message, multi-path `:(glob)` pathspec, required-env validation. GPG
  signing flows through global git config written by the preceding GPG
  import step.
- **`Swatinem/rust-cache`** → plain `actions/cache` in `sbom-cargo.yml`.
  The wrapper's value (target/ pruning, multi-path defaults,
  workspace-aware key) didn't apply — the workflow doesn't compile, only
  runs `cargo cyclonedx`. Plain cache pinned to `${CARGO_HOME}/registry`
  keeps the metadata-lookup speedup.
- **`jdx/mise-action`** → `scripts/bootstrap/install-mise.sh`. Used in
  `lint-misc.yml` and `self-pullrequest.yml`'s test job. Since mise
  installs `just` from `.mise.toml`, **`extractions/setup-just`** became
  redundant and was dropped at the same time.
- **`italia/publiccode-parser-action`** → `scripts/bootstrap/install-publiccode-parser.sh`.
  CLI baked into the runtime base image.
- **`docker/setup-qemu-action`** → dropped. `self-runtime-container.yml`'s
  `publish` job split into `publish-arch` (matrix `{target × platform}`
  on native runners — `ubuntu-24.04` for amd64, `ubuntu-24.04-arm` for
  arm64) and `publish-merge` (assembles the manifest list via
  `docker buildx imagetools create`). Mirrors the pattern
  `publish-container.yml` already used. No emulation, faster wall-clock.

The runtime base image grew to include `mise` and `publiccode-parser`
alongside the existing `yq` / `git-cliff` / `gh` / `glab`. Renovate pins
each via the matching `# renovate: datasource=...` comment in its
install helper.

The remaining fourteen actions are tiered in `gitlab.prep.md`. Most are
GitHub platform infrastructure (`actions/checkout`, `upload-artifact`,
`download-artifact`, `cache`) that GitLab handles via native YAML
keywords — not by a port. The rest are GitHub-only by design
(`actions/attest-sbom`, `slsa-github-generator`, `ossf/scorecard-action`,
`step-security/harden-runner`).

## Per-toolchain runtime images

The Containerfile's multi-stage layout publishes seven images, all built
and verified by `self-runtime-container.yml` on every PR:

- `reusable-ci-runtime-base` — bash, ca-certificates, curl, git, gnupg,
  gzip, jq, tar, unzip, xz, zip, yq, git-cliff, gh, glab, mise,
  publiccode-parser. Used by config / plan / release / orchestrator /
  changelog / signing / provider-dispatch / publiccode-lint jobs.
- `reusable-ci-runtime` — base + opengrep, syft, trivy. Security and
  SBOM jobs.
- `reusable-ci-runtime-rust-stable` — base + Rust stable + cargo-cyclonedx.
- `reusable-ci-runtime-node-24` — base + Node 24 LTS + npm + corepack.
- `reusable-ci-runtime-java-25` — base + Eclipse Temurin JDK 25 +
  Maven + Gradle.
- `reusable-ci-runtime-android-35` — java + Android cmdline-tools +
  platform-tools + build-tools 35 + android-35.

Build / publish workflows (`build-maven`, `build-npm`,
`build-gradle-app`, `build-gradle-android`, `publish-maven-central`,
`publish-dev-npm`, `publish-google-play`, `sbom-cargo`,
`version-bump`, …) all run inside the appropriate per-toolchain image
via the `runtime-image` input. No `actions/setup-{java,node}` per build
job; no per-job toolchain installs.

`actions/setup-java` and `actions/setup-node` survive only inside
`publish-maven-github.yml`, which is intrinsically GitHub-only (it
publishes *to* GitHub Packages).

## Internal simplifications

No external impact, but worth recording so the cleanup isn't re-litigated:

- Removed unused `ci_log_notice()` from `output.sh` (zero callers).
- Merged `resolve-file-pattern.sh` into `get-file-pattern.sh` (dual-mode: positional args + env vars).
- Merged `validate-auth-configuration.sh` into `validate-auth.sh` (dual-mode: positional args + env vars).
- Extracted `scripts/ci/stage-result.sh` — shared aggregation helpers used by all six stage-result scripts.
- Merged `create-and-sign-sbom-zip.sh` into `create-sbom-zip.sh` (signing via optional `SIGN_ARTIFACTS` / `GPG_KEY_ID` env vars).
- One stray `printf … >> "${GITHUB_OUTPUT:-/dev/null}"` in `scripts/version/move-tag.sh` migrated to `ci_output`.

## Remaining GitHub coupling (deliberate, not prep)

- `scripts/container/validate-namespace.sh` — hardcoded `ghcr.io` check. Security validation is registry-specific by design; not a coupling to clean up.
- `scripts/security/upload-sarif.sh` — GitHub Code Scanning by design. GitLab consumes `gitlab-sast.json` declaratively via `artifacts:reports:sast`; there's nothing for a `providers/gitlab.sh` to do here.
