<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# SBOM generation

reusable-ci produces Software Bills of Materials at multiple points in the pipeline. This page tells you what you get by default, how to change that, and how it works under the hood.

## Quick start

By default a release enables all SBOM layers supported by each artefact: Build (from the language's CycloneDX tool), Analyzed-artifact (Syft on the built artefact or extracted binary), and Analyzed-container when the artefact feeds a published container image. Dev builds skip SBOM generation by default for speed.

If those defaults are right for you, **you don't need to configure anything** — leave `sboms` unset everywhere.

To change them, set the `sboms` field. That's the entire user-facing surface.

## The `sboms` field

A single string field, accepted in two places:

- **Per-artefact** in `artifacts.yml` — what kinds of SBOMs that artefact produces during build/container stages.
- **Per-orchestrator-call** (input on `release-orchestrator.yml`, `release-dev-orchestrator.yml`, `release-create-github.yml`) — a global cap for release/dev SBOM aggregation.

The per-artefact value controls build-time Build SBOMs and container SBOM
generation. The orchestrator value is resolved against the pipeline-wide union
and controls what the release/dev aggregation step includes. It does not rewrite
each artefact's parsed `effective-sboms`.

### Accepted values

| Value | Produces |
|---|---|
| `all` | `build` + `analyzed-artifact` + `analyzed-container` |
| `none` | nothing |
| `build` | CISA Build SBOM only (cyclonedx plugin during build) |
| `analyzed-artifact` | Syft scan of the built artefact only |
| `analyzed-container` | Syft scan of the pushed container only |
| `build,analyzed-artifact` | any comma-list of the three layer names |

`all` and `none` are shortcuts and cannot be combined with layer names. Whitespace around commas is tolerated. Unknown tokens are rejected at parse time.

### Defaults

| Where | Default | Why |
|---|---|---|
| Per-artefact (`artifacts.yml`) | `all` for SBOM-capable ecosystems (`maven`, `npm`, `gradle`, `gradle-android`, `cargo`, `go`, `python`; `python` is schema-reserved but does not have workflows yet); `none` for `xcode-ios` and `meta` | Match historical "SBOMs on by default for buildable types" |
| Release orchestrator (`release.sboms`) | `all` | Release fires on tag push, once per release; SBOMs expected for compliance |
| Release-dev orchestrator (`sboms`) | `none` | Dev fires per-PR; SBOMs add 30–60 s/run that most reviews don't need |

The asymmetric defaults are deliberate. Override either side explicitly when your case differs.

### Examples

```yaml
# artifacts.yml — default behaviour (equivalent to sboms: all for maven)
artifacts:
  - name: my-app
    project-type: maven

# Compliance-minimum: CISA Build SBOM only
artifacts:
  - name: my-lib
    project-type: maven
    sboms: build

# Explicit opt-out (rare; this artefact contributes no SBOMs)
artifacts:
  - name: my-toy
    project-type: maven
    sboms: none

# Release aggregation cap: include only Build SBOMs in the release SBOM bundle
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@<sha>
    with:
      artifacts-config: .reusable-ci/artifacts.yml
      release.sboms: build      # release-orchestrator dot-prefix convention

# Dev flow: turn on full SBOM generation for testing
jobs:
  release-dev:
    uses: diggsweden/reusable-ci/.github/workflows/release-dev-orchestrator.yml@<sha>
    with:
      artifacts-config: .reusable-ci/artifacts.yml
      sboms: all                # release-dev-orchestrator uses flat names
```

The orchestrator-input naming asymmetry (`release.sboms` vs `sboms`) is a pre-existing convention: `release-orchestrator.yml` groups its inputs with a `release.` prefix (`release.signartifacts`, `release.draft`, …); the dev orchestrator does not. SBOMs follow the local convention of each orchestrator.

> **Mandatory by default:** Build SBOM generation is mandatory. Each builder runs the cyclonedx plugin without `continue-on-error`, so a tool failure fails the workflow. Opt out explicitly with `enable-build-sbom: false` per builder, or with `release.sboms: none` at the orchestrator. There is no silent SBOM-missing release. See [Failure semantics](#failure-semantics).

## CISA SBOM types — what we produce

| CISA type | Layer name | Produced by | Accuracy |
|---|---|---|---|
| **Build** | `build` | Ecosystem cyclonedx plugin running during the real build | **Highest — fully resolved dependency graph** |
| Analyzed (artifact) | `analyzed-artifact` | Syft scanning the built JAR/tgz/wheel/binary post-build; Gradle Android APK/AAB scanning is not generated today | Good — inferred from artefact contents |
| Analyzed (container) | `analyzed-container` | Syft scanning the pushed container image in `publish-container.yml` | Good — inferred from image layers |
| Design / Deployed / Runtime | — | Not generated; require non-CI information (architecture, live infra, runtime agents) | n/a |

### Why Build SBOM is special

Source-layer Syft scans are not generated by the Go port. They see declared dependencies but not the resolved version graph — transitive pins, exclusions, and scope filtering are invisible. Build-layer generation uses ecosystem-native CycloneDX tooling where available; Go uses `reusable-ci build go sbom`, backed by `cyclonedx-gomod`, for a module-level Build SBOM without compiling every platform.

### Per-language Build tools

| Ecosystem | Tool | Workflow | SBOM artefact |
|---|---|---|---|
| Maven | `cyclonedx-maven-plugin` (`makeAggregateBom`) | `build-maven.yml` | `maven-build-sbom` |
| Gradle (JVM) | `cyclonedx-gradle-plugin` (init-script) | `build-gradle-app.yml` | `gradle-build-sbom` (or `<artifact-name>-sbom` when overridden) |
| Gradle (Android) | same as above | `build-gradle-android.yml` | per matrix-variant name |
| npm | `@cyclonedx/cyclonedx-npm` (via `npx`) | `build-npm.yml` | `npm-build-sbom` |
| Cargo | `cargo-cyclonedx` (`--all` for workspaces) | `build-cargo.yml` (artifact-first) or `sbom-cargo.yml` (container-first, publish stage; lockfile-derived) | `cargo-build-sbom` or `<artifact-name>-cargo-build-sbom` |
| Go | `reusable-ci build go sbom` (`cyclonedx-gomod`) | `build-go.yml` (artifact-first) or `sbom-go.yml` (container-first) | `go-build-sbom` or `<artifact-name>-go-build-sbom` |

The build SBOM lives in its own upload artefact — separate from the code artefact (`<artifact-name>-build-artifacts`, `<artifact-name>-build-sbom`, etc.; direct single-workflow callers keep defaults such as `maven-build-artifacts`). A broken SBOM plugin can't take down the code upload. Tool versions are pinned and tracked by Renovate via `# renovate: datasource=...` comments.

### Cargo SBOM placement follows build-mode

Cargo is dual-mode (same as Go). Where the Build SBOM is emitted depends on `config.build-mode`:

- **`build-mode: artifact-first`** — `build-cargo.yml` cross-compiles the
  binary AND emits the Build SBOM inline (one `cargo cyclonedx` step against
  the lockfile). Matches the build-go.yml shape. The SBOM and the binaries
  upload as sibling artefacts from the same build job.
- **`build-mode: container-first`** — `sbom-cargo.yml` runs at publish stage
  alongside the container build. It deliberately does **not** invoke `cargo
  build` or `cargo test` — the actual compile happens once inside the
  Containerfile (`linux/amd64,linux/arm64` via native split-runner builds),
  with the lockfile-derived SBOM shipping next to the image.

Workspace-level `cargo test` lives in a caller-owned workflow either way, since
workspace features can't be expressed per-artefact; reusable-ci's artefact-first
build runs `cargo test --locked --all-targets` as a release-build sanity gate
(opt-out via `skip-tests`), not as a substitute for the caller's PR tests.

For default cargo projects, the lockfile-derived SBOM is identical to a
build-observed SBOM since `Cargo.lock` is the fully-resolved graph; projects
using `[target.'cfg(...)'.dependencies]` may see crates listed that aren't in a
given arch's binary.

This split — artefact-first ecosystems (maven/npm/gradle/go/cargo artifact-first) emit SBOMs as a byproduct of `build-<lang>.yml`; container-first ecosystems (cargo/go container-first) emit SBOMs from `sbom-<lang>.yml` while the actual compile lives in the Containerfile — is documented end-to-end in **[docs/ecosystems.md](ecosystems.md)**, including the per-ecosystem capability matrix.

## How it works internally

### Per-artefact vs pipeline-level

The per-artefact `sboms` value drives both **build-time plugin execution** and **release-bundle inclusion**:

- Each builder workflow (`build-maven.yml`, `build-gradle-app.yml`, `build-gradle-android.yml`, `build-npm.yml`, `build-go.yml`) accepts a `enable-build-sbom: bool` input. `release-build-stage.yml` derives this from `contains(matrix.artifact["effective-sboms"], 'build')` per artefact.
- If `build` is in the artefact's effective sboms, the cyclonedx plugin step runs. Otherwise it's skipped entirely — saves CI time, no upload, no downstream artefact. The orchestrator-level `sboms` input caps aggregate release/dev SBOM packaging later; it does not change this build-time decision.
- Analyzed-artifact and analyzed-container layers are gated similarly at release-time aggregation in `release-create-github.yml`.

Setting `sboms: none` on an artefact really means "skip everything for this artefact" — both the build-time plugin and the release-bundle inclusion. Useful for toy artefacts in a monorepo that you don't want spending CI minutes on.

When called directly (not from `release-build-stage.yml`), each builder defaults `enable-build-sbom: true` for backward compatibility — direct callers always get a Build SBOM unless they explicitly say otherwise.

`sbom-cargo.yml` and `sbom-go.yml` accept the same `enable-build-sbom` input for parity with the other builders, but since the SBOM step is the only thing they do, setting it to `false` from the orchestrator path turns the matrix entry into a no-op. Direct callers can either skip the input (defaults to true) or pass `enable-build-sbom: false` to suppress generation explicitly.

### Container scanning is derived, not separately gated

Container SBOM scanning (`analyzed-container` layer) is **derived** from source-artefact `sboms` — `publish-container.yml` runs syft on the pushed image if and only if any of the container's source artefacts (`from: [a, b, …]`) has `analyzed-container` in its effective sboms. With the default `sboms: all` on a buildable artefact, that's always true.

To skip the container scan: set the source artefact's `sboms` to exclude `analyzed-container` (e.g. `sboms: build,analyzed-artifact`).

This replaces the v2.x `containers[].enable-sbom: bool` field, which is no longer recognized in v3 (silently ignored — hard cutover, no alias). See CHANGELOG for migration.

> **Note — release-dev handles container SBOMs at container publish time.** When dev `sboms` includes `analyzed-container` (or `all`), `release-dev-publish-stage.yml` passes that through to `publish-dev-container.yml`. The later `reusable-ci sbom generate` step explicitly excludes `analyzed-container` because it only handles artifact-level layers (`build`, `analyzed-artifact`).

Dev SBOM aggregation uses the dev orchestrator's single-project control plane. Multi-artifact release SBOM packaging is handled by the production release orchestrator.

### Source layer is not generated

CISA Source SBOMs are not part of the Go implementation or the user-facing `sboms` enum. Rationale:

- **Build SBOM is strictly richer** — the cyclonedx plugin participates in the real build and sees the resolved graph. A Source SBOM adds nothing on top.
- **Analyzed-artifact overlaps** for ecosystems without a richer Build SBOM workflow (Python reserved today) — Syft on the built binary reads the same metadata as a source scan, plus what actually shipped.

### Aggregation in release flows

`reusable-ci sbom generate` consolidates per-stack SBOMs into release files using a single CISA-aligned naming pattern. The short commit SHA is injected for traceability when run inside a git repo:

```text
<project>-<version>-<short-sha>-build-sbom.cyclonedx.json
<artefact-basename>-<short-sha>-analyzed-jar-sbom.{cyclonedx,spdx}.json          # Maven / Gradle (JVM)
<artefact-basename>-<short-sha>-analyzed-tararchive-sbom.{cyclonedx,spdx}.json   # npm
<artefact-basename>-<short-sha>-analyzed-binary-sbom.{cyclonedx,spdx}.json       # Go / Rust
<artefact-basename>-<short-sha>-analyzed-wheel-sbom.{cyclonedx,spdx}.json        # Python
<project>-<version>-<short-sha>-analyzed-container-sbom.{cyclonedx,spdx}.json
```

`<artefact-basename>` is derived from the actual scanned file (so multi-jar projects get unique names per jar). `build` SBOMs use `<project>-<version>` instead — there is one Build SBOM per project, not per output file. Container scans have a single artefact-type so the analyzed-container layer omits the further modifier.

The aggregator looks for `bom.json` under `./release-artifacts/` (the destination of `actions/download-artifact` with `merge-multiple: true`) and picks the aggregate match — the one with the smallest path depth — so the root project BOM wins over per-module ones. It ignores vendored BOMs under `node_modules/` and compile caches under `target/`. Consumers using a non-root `working-directory` are handled automatically because the lookup uses a path pattern, not fixed locations.

## Failure semantics

SBOM generation is mandatory by default. Each builder runs the cyclonedx plugin
without `continue-on-error`, so a tool failure (broken plugin, missing
lockfile, unreadable manifest) fails the workflow on the spot. This matches
the deterministic-pipeline contract: a passing pipeline implies a complete
release artifact set, including SBOMs.

Two explicit opt-outs exist for adopters who don't need SBOM generation on a
specific run:

- **Per-builder:** pass `enable-build-sbom: false` when calling a builder
  workflow directly. The step is skipped (`steps.sbom.outcome == 'skipped'`),
  the upload is skipped, and the status report records "skipped" in the step
  summary. No silent failure.
- **At the orchestrator:** set `release.sboms: none` (or omit the `build`
  layer from a comma-list) and the orchestrator will pass
  `enable-build-sbom: false` through the build-stage matrix.

Both opt-outs are visible in the workflow input and traceable in the run UI —
"no SBOM" is a recorded decision, not a silent gap.

## Verifying a Build SBOM was produced

Each build workflow writes a one-line status to its GitHub step summary:

```text
### Build SBOM
- ✅ CycloneDX (aggregate): `server/target/bom.json`
```

To verify without clicking into the run, the SBOM appears as its own artefact (`maven-build-sbom`, etc.). Download and inspect:

```bash
gh run download <run-id> --name maven-build-sbom
jq '.metadata.component.name, (.components | length)' bom.json
```

## Testing

```bash
go test ./internal/domain/sbom ./internal/app/sbom ./internal/cli/commands/sbom ./internal/app/build
```

Coverage includes project-type auto-detection, build-layer pickup (working-directory subdirs, multi-module Maven aggregate preference, npm `node_modules/` exclusion), gradle init-script contract, multi-layer runs, and ZIP packaging.
