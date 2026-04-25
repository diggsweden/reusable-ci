<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# SBOM generation

reusable-ci produces Software Bills of Materials at multiple points in the pipeline, following CISA's April 2023 SBOM type taxonomy. Different types have different accuracy guarantees; we generate the ones that give real, verifiable data and skip the aspirational ones.

## CISA types — what we produce and how

| CISA type | Layer name | Where generated | Tool | Accuracy |
|---|---|---|---|---|
| Source | `source` | `scripts/sbom/generate-sbom.sh` during release | Syft scanning `pom.xml` / `package.json` / `build.gradle` / etc. | Declared deps only |
| **Build** | `build` | **During the build step** of each language workflow | Ecosystem plugin (cyclonedx-maven-plugin / cyclonedx-gradle-plugin / @cyclonedx/cyclonedx-npm / cargo-cyclonedx) | **Highest — fully resolved dependency tree** |
| Analyzed (artifact) | `analyzed-artifact` | After build, in release workflows | Syft scanning the built JAR/tgz/AAB | Good — inferred from artifact contents |
| Analyzed (container) | `analyzed-container` | In `publish-container.yml` | Syft scanning the published image | Good — inferred from image layers |
| Design | — | Not generated | — | Aspirational, not data-driven |
| Deployed | — | Not generated | — | Environment-specific; out of scope for CI |
| Runtime | — | Not generated | — | Requires runtime instrumentation |

### Why Build SBOM is special

Source-layer Syft scans see declared dependencies but not the resolved version graph — transitive pins, exclusions, and scope filtering are invisible. Build-layer generation runs inside the real build, so it sees exactly the graph the build actually resolved. That's why each language uses its own ecosystem tool rather than Syft for this layer.

## Per-language build tools

| Ecosystem | Tool | Where in pipeline | SBOM artifact | SBOM path inside artifact |
|---|---|---|---|---|
| Maven | `cyclonedx-maven-plugin` (via `makeAggregateBom`) | `.github/workflows/build-maven.yml` | `maven-build-sbom` | `target/bom.json` + `**/target/bom.json` |
| Gradle (JVM) | `cyclonedx-gradle-plugin` (via init-script) | `.github/workflows/build-gradle-app.yml` | `gradle-build-sbom` (or `<artifact-name>-sbom` when overridden) | `build/reports/bom.json` + `**/build/reports/bom.json` |
| Gradle (Android) | same as above | `.github/workflows/build-gradle-android.yml` | `<date> - <prefix> - <repo> - <flavor> - build SBOM` (per matrix variant) | same as above |
| npm | `@cyclonedx/cyclonedx-npm` (via `npx`) | `.github/workflows/build-npm.yml` | `npm-build-sbom` | `bom.json` |
| Cargo (beta) | `cargo-cyclonedx` (`--all` for workspaces) | `.github/workflows/build-rust.yml` | `rust-build-sbom` (configurable) | `bom.json` + `**/bom.json` |

The build SBOM lives in its own upload artifact — separate from the code artifact (`maven-build-artifacts`, `npm-build-artifacts`, `gradle-build-artifacts`). This keeps the SBOM as a first-class compliance deliverable and means a broken SBOM plugin can't take down the code upload.

Tool versions are pinned and tracked by Renovate via `# renovate: datasource=...` comments in the workflow files.

### Rust workflow is direct-call only (beta)

`build-rust.yml` is currently SBOM-only (beta) — it runs `cargo-cyclonedx` without invoking `cargo build`/`cargo test`. cargo-cyclonedx resolves the full dependency graph from `Cargo.lock`, so the SBOM itself is accurate whether or not a build has been performed. The workflow is **not** wired into `release-orchestrator.yml`; consumers call it directly via `uses:` until `cargo build` + `cargo test` steps land. The filename stays stable across that expansion so consumers don't need to migrate.

### Build-SBOM failure semantics

All four ecosystems run their SBOM step with `continue-on-error: true`: an SBOM tool regression must not fail the real build. A yellow warning in the Actions UI, plus a `⚠️` line in the job step summary, is the signal to investigate.

If you specifically *do* need SBOM generation to be mandatory for your project, override the per-language workflow in your pipeline and remove `continue-on-error`.

## Verifying a build SBOM was produced

Each build workflow writes a one-line status to the GitHub step summary of its run:

```
### Build SBOM
- ✅ CycloneDX (aggregate): `server/target/bom.json`
```

To verify without clicking into the run, the same file appears in the job's Artifacts list under the artifact name from the table above. Download and inspect with:

```bash
gh run download <run-id> --name maven-build-artifacts
jq '.metadata.component.name, (.components | length)' target/bom.json
```

## Aggregation in release flows

`scripts/sbom/generate-sbom.sh` consolidates SBOMs from the build stage into a single set of release files named:

```
<project>-<version>-<short-sha>-build-sbom.cyclonedx.json
```

The aggregator looks for `bom.json` under `./release-artifacts/` (the destination of `actions/download-artifact` with `merge-multiple: true`) and picks the shallowest match — so the root aggregate BOM wins over per-module ones. It ignores vendored BOMs under `node_modules/` and compile caches under `target/`. Consumers using a non-root `working-directory` are handled automatically because the lookup uses a path pattern, not fixed locations.

## Opt-in for dev builds

`release-dev-orchestrator.yml` accepts a `generate-sbom: true` input (default `false`). Turn it on when you want to test SBOM wiring on a branch without cutting a real release:

```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-dev-orchestrator.yml@<sha>
with:
  project-type: maven
  generate-sbom: true
```

The `generate-dev-sbom` job downloads the build artifacts, runs `generate-sbom.sh` for `source,build,analyzed-artifact`, and uploads `dev-sboms-${run_id}`.

## Unsupported layers

Design, Deployed, and Runtime SBOMs (per CISA) are deliberately out of scope here — they require non-CI information (architecture diagrams, live infrastructure, runtime agents). Adding them means adding tooling outside reusable-ci, not a workflow in this repo.

## Testing

Run the SBOM test suite locally:

```bash
bats tests/sbom/
```

Coverage includes:

- Project-type auto-detection and explicit override.
- Source-layer scanning for all supported ecosystems.
- Build-layer pickup from plugin-produced `bom.json`, including:
  - `working-directory` subdirectories,
  - multi-module Maven (aggregate preferred over module BOMs),
  - npm `node_modules/` exclusion.
- Init-script wrapper contract for Gradle (`tests/sbom/generate-gradle-sbom.bats`).
- Multi-layer runs and ZIP packaging.
