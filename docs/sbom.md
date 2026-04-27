<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# SBOM generation

reusable-ci produces Software Bills of Materials at multiple points in the pipeline. This page tells you what you get by default, how to change that, and how it works under the hood.

## Quick start

By default a release publishes three CISA SBOM types per artefact: Build (from the language's cyclonedx plugin), Analyzed-artifact (Syft on the built binary), and Analyzed-container (Syft on the pushed image). Dev builds skip SBOM generation by default for speed.

If those defaults are right for you, **you don't need to configure anything** — leave `sboms` unset everywhere.

To change them, set the `sboms` field. That's the entire user-facing surface.

## The `sboms` field

A single string field, accepted in two places:

- **Per-artefact** in `artifacts.yml` — what kinds of SBOMs that artefact produces.
- **Per-orchestrator-call** (input on `release-orchestrator.yml`, `release-dev-orchestrator.yml`, `release-create-github.yml`) — a release-level cap applied to every artefact.

The effective set per artefact is the **intersection** of the two. Empty intersection (cap and per-artefact disagree completely) is a misconfiguration — the pipeline warns and produces no SBOMs rather than failing.

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
| Per-artefact (`artifacts.yml`) | `all` for build-SBOM-supporting ecosystems (maven, npm, gradle, gradle-android, python, go, rust); `none` for `xcode-ios` and `meta` | Match historical "SBOMs on by default for buildable types" |
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

# Release-level cap: even if artefacts request `all`, only Build SBOMs get produced
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@<sha>
    with:
      release.sboms: build      # release-orchestrator dot-prefix convention

# Dev flow: turn on full SBOM generation for testing
jobs:
  release-dev:
    uses: diggsweden/reusable-ci/.github/workflows/release-dev-orchestrator.yml@<sha>
    with:
      project-type: maven
      sboms: all                # release-dev-orchestrator uses flat names
```

The orchestrator-input naming asymmetry (`release.sboms` vs `sboms`) is a pre-existing convention: `release-orchestrator.yml` groups its inputs with a `release.` prefix (`release.signartifacts`, `release.draft`, …); the dev orchestrator does not. SBOMs follow the local convention of each orchestrator.

> ⚠️ **Strict compliance:** Build SBOM generation is advisory by default — each builder runs the cyclonedx plugin with `continue-on-error: true`, so a broken plugin is a yellow warning, not a failed release. If you set `release.sboms: build` for compliance and need the release to **fail** when SBOMs aren't produced, override the per-language builder workflow in your own pipeline and remove that flag. See [Failure semantics](#failure-semantics).

## CISA SBOM types — what we produce

| CISA type | Layer name | Produced by | Accuracy |
|---|---|---|---|
| Source | (internal — not user-selectable) | Syft scanning manifests; superseded by Build for ecosystems that support it | Declared deps only |
| **Build** | `build` | Ecosystem cyclonedx plugin running during the real build | **Highest — fully resolved dependency graph** |
| Analyzed (artifact) | `analyzed-artifact` | Syft scanning the built JAR/tgz/wheel/binary post-build | Good — inferred from artefact contents |
| Analyzed (container) | `analyzed-container` | Syft scanning the pushed container image in `publish-container.yml` | Good — inferred from image layers |
| Design / Deployed / Runtime | — | Not generated; require non-CI information (architecture, live infra, runtime agents) | n/a |

### Why Build SBOM is special

Source-layer Syft scans see declared dependencies but not the resolved version graph — transitive pins, exclusions, and scope filtering are invisible. Build-layer generation runs inside the real build, so it sees exactly the graph the build actually resolved. Each language uses its own ecosystem tool rather than Syft for this layer.

### Per-language Build tools

| Ecosystem | Tool | Workflow | SBOM artefact |
|---|---|---|---|
| Maven | `cyclonedx-maven-plugin` (`makeAggregateBom`) | `build-maven.yml` | `maven-build-sbom` |
| Gradle (JVM) | `cyclonedx-gradle-plugin` (init-script) | `build-gradle-app.yml` | `gradle-build-sbom` (or `<artifact-name>-sbom` when overridden) |
| Gradle (Android) | same as above | `build-gradle-android.yml` | per matrix-variant name |
| npm | `@cyclonedx/cyclonedx-npm` (via `npx`) | `build-npm.yml` | `npm-build-sbom` |
| Cargo (beta) | `cargo-cyclonedx` (`--all` for workspaces) | `build-rust.yml` | `rust-build-sbom` |

The build SBOM lives in its own upload artefact — separate from the code artefact (`maven-build-artifacts`, `npm-build-artifacts`, etc.). A broken SBOM plugin can't take down the code upload. Tool versions are pinned and tracked by Renovate via `# renovate: datasource=...` comments.

### Rust workflow is direct-call only (beta)

`build-rust.yml` is currently SBOM-only — it runs `cargo-cyclonedx` without invoking `cargo build`/`cargo test`. cargo-cyclonedx resolves the dependency graph from `Cargo.lock`, so the SBOM is accurate whether or not a build has been performed. The workflow is **not** wired into `release-orchestrator.yml`; consumers call it directly via `uses:` until a full Rust builder lands. The filename stays stable across that expansion.

## How it works internally

### Per-artefact vs pipeline-level

The per-artefact `sboms` value drives both **build-time plugin execution** and **release-bundle inclusion**:

- Each builder workflow (`build-maven.yml`, `build-gradle-app.yml`, `build-gradle-android.yml`, `build-npm.yml`) accepts a `enable-build-sbom: bool` input. `release-build-stage.yml` derives this from `contains(matrix.artifact["effective-sboms"], 'build')` per artefact.
- If `build` is in the artefact's effective sboms (after intersection with the pipeline cap), the cyclonedx plugin step runs. Otherwise it's skipped entirely — saves CI time, no upload, no downstream artefact.
- Analyzed-artifact and analyzed-container layers are gated similarly at release-time aggregation in `release-create-github.yml`.

Setting `sboms: none` on an artefact really means "skip everything for this artefact" — both the build-time plugin and the release-bundle inclusion. Useful for toy artefacts in a monorepo that you don't want spending CI minutes on.

When called directly (not from `release-build-stage.yml`), each builder defaults `enable-build-sbom: true` for backward compatibility — direct callers always get a Build SBOM unless they explicitly say otherwise.

`build-rust.yml` is intentionally **not** gated this way: it's SBOM-only by design (no `cargo build` step), so gating it would make the entire workflow a no-op. Direct callers control its execution by choosing whether to invoke it at all.

### Container scanning is derived, not separately gated

Container SBOM scanning (`analyzed-container` layer) is **derived** from source-artefact `sboms` — `publish-container.yml` runs syft on the pushed image if and only if any of the container's source artefacts (`from: [a, b, …]`) has `analyzed-container` in its effective sboms. With the default `sboms: all` on a buildable artefact, that's always true.

To skip the container scan: set the source artefact's `sboms` to exclude `analyzed-container` (e.g. `sboms: build,analyzed-artifact`).

This replaces the v2.x `containers[].enable-sbom: bool` field, which is no longer recognized in v3 (silently ignored — hard cutover, no alias). See CHANGELOG for migration.

### Source layer is internal

CISA Source SBOMs exist in `generate-sboms.sh` but are not exposed in the user-facing `sboms` enum. Rationale:

- **Build SBOM is strictly richer** — the cyclonedx plugin participates in the real build and sees the resolved graph. A Source SBOM adds nothing on top.
- **Analyzed-artifact overlaps** for ecosystems without a Build SBOM (Go, Python) — Syft on the built binary reads the same metadata as a source scan, plus what actually shipped.

Internally, `generate-sboms.sh` still accepts `source` if invoked directly, but no orchestrator or release flow passes it.

### Aggregation in release flows

`scripts/sbom/generate-sboms.sh` consolidates per-stack SBOMs into release files using a single CISA-aligned naming pattern. The short commit SHA is injected for traceability when run inside a git repo:

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

All builders run the SBOM step with `continue-on-error: true`: an SBOM tool regression must not fail the real build. A yellow warning in the Actions UI plus a `⚠️` line in the job step summary is the signal to investigate.

If your project requires SBOM generation to be mandatory, override the per-language workflow in your own pipeline and remove `continue-on-error`.

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
bats tests/sbom/
```

Coverage includes project-type auto-detection, build-layer pickup (working-directory subdirs, multi-module Maven aggregate preference, npm `node_modules/` exclusion), gradle init-script contract, multi-layer runs, and ZIP packaging.
