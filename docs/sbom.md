# SBOM generation

reusable-ci produces Software Bills of Materials at multiple points in the pipeline. This page tells you what you get by default, how to change that, and how it works under the hood.

## Quick start

By default, SBOM-capable artifact types request all three layers: Build,
Analyzed-artifact, and Analyzed-container. Generation still depends on the
ecosystem and deliverable: notably, Gradle Android does not implement
Analyzed-artifact scanning, so Android artifacts must explicitly exclude that
layer. Snapshot builds skip SBOM generation by default for speed.

For non-Android artifacts, if those defaults are right for you, **you don't need
to configure anything**: leave `sboms` unset. Android artifacts must select a
supported subset as described below.

To change them, set the `sboms` field. That's the entire user-facing surface.

## The `sboms` field

A single string field, accepted in two places:

- **Per-artifact** in `artifacts.yml`: what kinds of SBOMs that artifact produces during build/container stages.
- **Per-orchestrator-call** (input on `release-orchestrator.yml`, `release-snapshot-orchestrator.yml`, `release-create-github.yml`): release/snapshot selection policy.

The per-artifact value controls artifact-first Build SBOM jobs and container
SBOM generation. For production releases, `release.sboms` is intersected with
the pipeline-wide union and controls the release SBOM bundle and downloads. It
also gates the separate container-first Go/Cargo Build SBOM jobs. It does not
rewrite each artifact's parsed `effective-sboms`, disable artifact-first Build
SBOM jobs, or disable registry-attached container SBOM attestations. The
snapshot `sboms` input has its own workflow-wide generation gates.

### Accepted values

| Value | Produces |
|---|---|
| `all` | `build` + `analyzed-artifact` + `analyzed-container` |
| `none` | nothing |
| `build` | CISA Build SBOM only (cyclonedx plugin during build) |
| `analyzed-artifact` | Syft scan of the built artifact only |
| `analyzed-container` | Syft scan of the pushed container only |
| `build,analyzed-artifact` | any comma-list of the three layer names |

`all` and `none` are shortcuts and cannot be combined with layer names. Whitespace around commas is tolerated. Unknown tokens are rejected at parse time.

### Defaults

| Where | Default | Why |
|---|---|---|
| Per-artifact (`artifacts.yml`) | `all` for SBOM-capable ecosystems (`maven`, `npm`, `gradle`, `gradle-android`, `cargo`, `go`); `none` for `xcode-ios` and `meta` | Match historical "SBOMs on by default for buildable types" |
| Release orchestrator (`release.sboms`) | `all` | Release fires on tag push, once per release; SBOMs expected for compliance |
| Snapshot orchestrator (`sboms`) | `none` | Snapshot fires per push/dispatch; SBOMs add 30–60 s/run that most reviews don't need |

The asymmetric defaults are deliberate. Override either side explicitly when your case differs.

Gradle Android is the exception to the general `all` default: `sbom assemble`
does not support `analyzed-artifact` for APK/AAB files. Set Android artifacts to
`sboms: build`, or `sboms: build,analyzed-container` when they feed a container.

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

# Explicit opt-out (rare; this artifact contributes no SBOMs)
artifacts:
  - name: my-toy
    project-type: maven
    sboms: none

# Release selection: include only Build SBOMs in the release SBOM bundle
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@<sha>
    with:
      branch: main
      artifacts-config: .reusable-ci/artifacts.yml
      release.sboms: build      # release-orchestrator dot-prefix convention

# Snapshot flow: turn on full SBOM generation for testing
jobs:
  release-snapshot:
    uses: diggsweden/reusable-ci/.github/workflows/release-snapshot-orchestrator.yml@<sha>
    with:
      branch: ${{ github.ref_name }}
      artifacts-config: .reusable-ci/artifacts.yml
      sboms: all                # release-snapshot-orchestrator uses flat names
```

The orchestrator-input naming asymmetry (`release.sboms` vs `sboms`) is a pre-existing convention: `release-orchestrator.yml` groups its inputs with a `release.` prefix (`release.signartifacts`, `release.draft`, …); the snapshot orchestrator does not. SBOMs follow the local convention of each orchestrator.

> **Mandatory when selected:** Each Build SBOM job runs without
> `continue-on-error`, so a tool failure fails the workflow. Exclude `build` in
> the artifact's `sboms`, or pass `enable-build-sbom: false` to a directly
> called builder. `release.sboms: none` is not a universal generation switch:
> artifact-first builders and container attestations still follow per-artifact
> policy. See [Failure semantics](#failure-semantics).

## CISA SBOM types: what we produce

| CISA type | Layer name | Produced by | Accuracy |
|---|---|---|---|
| **Build** | `build` | Ecosystem cyclonedx plugin running during the real build | **Highest: fully resolved dependency graph** |
| Analyzed (artifact) | `analyzed-artifact` | Syft scanning the built JAR/tgz/wheel/binary post-build; Gradle Android APK/AAB scanning is not generated today | Good, inferred from artifact contents |
| Analyzed (container) | `analyzed-container` | Syft scanning the pushed container image in `publish-container.yml` | Good, inferred from image layers |
| Design / Deployed / Runtime | — | Not generated; require non-CI information (architecture, live infra, runtime agents) | n/a |

### Why Build SBOM is special

Source-layer Syft scans are not generated by the Go port. They see declared dependencies but not the resolved version graph. Transitive pins, exclusions, and scope filtering are invisible. Build-layer generation uses ecosystem-native CycloneDX tooling where available; Go uses `reusable-ci sbom build --project-type go`, backed by `cyclonedx-gomod`, for a module-level Build SBOM without compiling every platform.

### Per-language Build tools

| Ecosystem | Tool | Workflow | SBOM artifact |
|---|---|---|---|
| Maven | `cyclonedx-maven-plugin` (`makeAggregateBom`) | `build-maven.yml` | `maven-build-sbom` |
| Gradle (JVM) | `cyclonedx-gradle-plugin` (init-script) | `build-gradle-app.yml` | `gradle-build-sbom` (or `<artifact-name>-sbom` when overridden) |
| Gradle (Android) | same as above | `build-gradle-android.yml` | per matrix-variant name |
| npm | `@cyclonedx/cyclonedx-npm` (via `npx`) | `build-npm.yml` | `npm-build-sbom` |
| Cargo | `cargo-cyclonedx` (`--all` for workspaces) | `build-cargo.yml` (artifact-first) or `sbom-cargo.yml` (container-first, publish stage; lockfile-derived) | `cargo-build-sbom` or `<artifact-name>-cargo-build-sbom` |
| Go | `reusable-ci sbom build --project-type go` (`cyclonedx-gomod`) | `build-go.yml` (artifact-first) or `sbom-go.yml` (container-first) | `go-build-sbom` or `<artifact-name>-go-build-sbom` |

The build SBOM lives in its own upload artifact, separate from the code artifact (`<artifact-name>-build-artifacts`, `<artifact-name>-build-sbom`, etc.; direct single-workflow callers keep defaults such as `maven-build-artifacts`). When Build SBOM generation is enabled, both generation and upload are mandatory: a plugin failure or a missing expected file fails the job before publication. Tool versions are pinned and tracked by Renovate via `# renovate: datasource=...` comments.

### Cargo SBOM placement follows build-mode

Cargo is dual-mode (same as Go). Where the Build SBOM is emitted depends on `config.build-mode`:

- **`build-mode: artifact-first`**: `build-cargo.yml` cross-compiles the
  binary AND emits the Build SBOM inline (one `cargo cyclonedx` step against
  the lockfile). Matches the build-go.yml shape. The SBOM and the binaries
  upload as sibling artifacts from the same build job.
- **`build-mode: container-first`**: `sbom-cargo.yml` runs at publish stage
  alongside the container build. It deliberately does **not** invoke `cargo
  build` or `cargo test`, because the actual compile happens once inside the
  Containerfile (`linux/amd64,linux/arm64` via native split-runner builds),
  with the lockfile-derived SBOM shipping next to the image.

Workspace-level `cargo test` lives in a caller-owned workflow either way, since
workspace features can't be expressed per-artifact; reusable-ci's artifact-first
build runs `cargo test --locked --all-targets` as a release-build sanity gate
(opt-out via `skip-tests`), not as a substitute for the caller's PR tests.

For default cargo projects, the lockfile-derived SBOM is identical to a
build-observed SBOM since `Cargo.lock` is the fully-resolved graph; projects
using `[target.'cfg(...)'.dependencies]` may see crates listed that aren't in a
given arch's binary.

The artifact-first / container-first split is documented end-to-end in **[docs/ecosystems.md](ecosystems.md)**, including the per-ecosystem capability matrix. Artifact-first ecosystems (maven/npm/gradle/go/cargo artifact-first) emit SBOMs as a byproduct of `build-<lang>.yml`. Container-first ecosystems (cargo/go container-first) emit SBOMs from `sbom-<lang>.yml`, while the actual compile lives in the Containerfile.

## How it works internally

### Per-artifact vs pipeline-level

The per-artifact `sboms` value drives both **build-time plugin execution** and **release-bundle inclusion**:

- Each artifact-first builder workflow accepts an `enable-build-sbom: bool`
  input. `release-build-stage.yml` derives it only from the artifact's
  `effective-sboms`; `release.sboms` does not change that build-time decision.
- Container-first Go/Cargo Build SBOMs are separate publish-stage jobs. They run
  only when `build` survives both the per-artifact policy and the
  `release.sboms` intersection.
- Analyzed-artifact SBOMs are generated during release assembly from downloaded
  build artifacts. Analyzed-container SBOMs are generated and attested during
  container publish from per-artifact policy; `release.sboms` only controls
  whether those documents are also downloaded into the release bundle.

Setting `sboms: none` on an artifact really means "skip everything for this artifact": both the build-time plugin and the release-bundle inclusion. Useful for toy artifacts in a monorepo that you don't want spending CI minutes on.

Gradle Android must exclude `analyzed-artifact`: requesting it reaches an
unsupported project-type branch and fails release assembly. Build SBOMs remain
supported, as do analyzed-container SBOMs when an Android artifact feeds a
container.

When called directly (not from `release-build-stage.yml`), each builder defaults `enable-build-sbom: true` for backward compatibility. Direct callers always get a Build SBOM unless they explicitly say otherwise.

`sbom-cargo.yml` and `sbom-go.yml` accept the same `enable-build-sbom` input
for direct-call parity. The orchestrator omits ineligible matrix entries;
direct callers can omit the input (defaults to true) or pass
`enable-build-sbom: false` to make the workflow a no-op.

### Container scanning is derived, not separately gated

Container SBOM scanning (`analyzed-container` layer) is **derived** from source-artifact `sboms`. `publish-container.yml` runs syft on the pushed image if and only if any of the container's source artifacts (`from: [a, b, …]`) has `analyzed-container` in its effective sboms. With the default `sboms: all` on a buildable artifact, that's always true.

To skip the container scan: set the source artifact's `sboms` to exclude `analyzed-container` (e.g. `sboms: build,analyzed-artifact`).

This replaces the v2.x `containers[].enable-sbom: bool` field. In v3 strict config parsing rejects that removed field; use artifact `sboms` instead.

> **Note: the snapshot flow builds no containers.** `release-snapshot-publish-stage.yml` produces only artifact-level SBOMs (`build`, `analyzed-artifact`) for npm/cargo/go. There is no container build or `analyzed-container` SBOM on the snapshot path. Container images (and their `analyzed-container` SBOMs) are built once on the release path by `publish-container.yml`.

Snapshot SBOM aggregation uses the snapshot orchestrator's single-project control plane. Multi-artifact release SBOM packaging is handled by the production release orchestrator.

### Source layer is not generated

CISA Source SBOMs are not part of the Go implementation or the user-facing `sboms` enum. Rationale:

- **Build SBOM is strictly richer**: the cyclonedx plugin participates in the real build and sees the resolved graph. A Source SBOM adds nothing on top.
- **Analyzed-artifact overlaps** when no richer Build SBOM is available: Syft on the built artifact reads package metadata plus what actually shipped.

### Aggregation in release flows

`reusable-ci sbom assemble` consolidates per-stack SBOMs into release files under a single CISA-aligned naming pattern. It harvests the language-native build BOM and syft-scans built artifacts and containers, so it runs *after* the build. The short commit SHA is injected for traceability when run inside a git repo:

```text
<project>-<version>-<short-sha>-build-sbom.cyclonedx.json
<artifact-basename>-<short-sha>-analyzed-jar-sbom.{cyclonedx,spdx}.json          # Maven / Gradle (JVM)
<artifact-basename>-<short-sha>-analyzed-tararchive-sbom.{cyclonedx,spdx}.json   # npm
<artifact-basename>-<short-sha>-analyzed-binary-sbom.{cyclonedx,spdx}.json       # Go / Rust
<project>-<version>-<short-sha>-analyzed-container-sbom.{cyclonedx,spdx}.json
```

`<artifact-basename>` is derived from the actual scanned file (so multi-jar projects get unique names per jar). `build` SBOMs use `<project>-<version>` instead, because there is one Build SBOM per project, not per output file. Container scans have a single artifact-type so the analyzed-container layer omits the further modifier.

The aggregator looks for `bom.json` under `./release-artifacts/` (the destination of `actions/download-artifact` with `merge-multiple: true`) and picks the aggregate match, the one with the smallest path depth. The root project BOM therefore wins over per-module ones. It ignores vendored BOMs under `node_modules/` and compile caches under `target/`. Consumers using a non-root `working-directory` are handled automatically because the lookup uses a path pattern, not fixed locations.

## Failure semantics

SBOM generation is mandatory by default. Each builder runs the cyclonedx plugin
without `continue-on-error`, so a tool failure (broken plugin, missing
lockfile, unreadable manifest) fails the workflow on the spot. This matches
the deterministic-pipeline contract: a passing pipeline implies a complete
set of the SBOM layers selected by policy.

The supported controls have different scopes:

- **Per-artifact:** exclude a layer in `artifacts.yml`. This is the production
  workflow's generation policy for artifact-first Build SBOMs and container
  SBOM attestations.
- **Per-builder:** pass `enable-build-sbom: false` when calling a builder
  workflow directly. The step is skipped (`steps.sbom.outcome == 'skipped'`),
  the upload is skipped, and the status report records "skipped" in the step
  summary. No silent failure.
- **At the production orchestrator:** `release.sboms` selects release-bundle
  layers and gates container-first Go/Cargo Build SBOM jobs. It does not pass
  `enable-build-sbom: false` to artifact-first builders and does not disable
  container-image SBOM generation or attestation.

All controls are visible in committed config or workflow inputs and traceable in
the run UI.

## Verifying a Build SBOM was produced

Each build workflow writes a one-line status to its GitHub step summary:

```text
### Build SBOM
- ✅ CycloneDX (aggregate): `server/target/bom.json`
```

To verify without clicking into the run, the SBOM appears as its own artifact (`maven-build-sbom`, etc.). Download and inspect:

```bash
gh run download <run-id> --name maven-build-sbom
jq '.metadata.component.name, (.components | length)' bom.json
```

## Delivery and verification

Release SBOMs have two delivery paths:

- The build and analyzed SBOMs selected by effective `release.sboms` policy are packaged in a signed
  `*-sboms.zip` GitHub Release asset. The release checksum manifest covers the
  ZIP and the other release assets.
- Each per-architecture container digest receives its analyzed-container
  CycloneDX document as a signed cosign attestation. This path is registry- and
  forge-neutral; it does not depend on GitHub's attestation API.

Verify release files in trust order: authenticate the checksum manifest, check
downloaded files against it, then authenticate the optional detached SBOM ZIP
signature:

```bash
gh release download "$TAG" -p 'checksums.sha256*' -p '*-sboms.zip*'
gpg --verify checksums.sha256.asc checksums.sha256
sha256sum -c checksums.sha256 --ignore-missing
gpg --verify PROJECT-1.0.0-sboms.zip.asc PROJECT-1.0.0-sboms.zip
```

For a container, verify and extract the signed predicate from the exact platform
digest:

```bash
cosign verify-attestation --type cyclonedx \
  --certificate-identity-regexp '^https://github.com/<owner>/<repo>/\.github/workflows/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/<owner>/<repo>@sha256:PLATFORM_DIGEST

cosign download attestation --predicate-type=https://cyclonedx.org/bom \
  ghcr.io/<owner>/<repo>@sha256:PLATFORM_DIGEST \
  | jq -r '.payload | @base64d | fromjson | .predicate'
```

See [Verification](verification.md#full-verification-guide) for the surrounding
artifact and provenance checks.

## Consuming SBOMs

Build SBOMs use CycloneDX 1.6. Analyzed-artifact and analyzed-container layers
are available as CycloneDX 1.6 and SPDX 2.3 so security and legal tooling can use
the format it already supports.

```bash
# Vulnerabilities, with a release-relevant severity floor
trivy sbom --severity HIGH,CRITICAL artifact.cyclonedx.json

# License findings
trivy sbom --scanners license artifact.cyclonedx.json

# Inspect SPDX package/license data directly
jq '.packages[] | {name, version: .versionInfo, license: .licenseConcluded}' \
  artifact.spdx.json

# Convert an existing SBOM to Syft's table view
syft convert artifact.spdx.json -o table
```

The generated documents cover the applicable NTIA minimum elements and the
Build/Analyzed portions of the CISA SBOM taxonomy. Source, Design, Deployed, and
Runtime SBOMs require information outside this CI pipeline and are not emitted.
An SBOM supports CRA transparency work, but CRA update and incident-reporting
obligations remain outside CI.

Further references:

- [CISA SBOM guidance](https://www.cisa.gov/sbom)
- [CISA SBOM types](https://www.cisa.gov/resources-tools/resources/types-software-bill-materials-sbom)
- [CycloneDX 1.6](https://cyclonedx.org/specification/overview/)
- [SPDX 2.3](https://spdx.github.io/spdx-spec/v2.3/)
- [Syft](https://github.com/anchore/syft)
- [Trivy](https://github.com/aquasecurity/trivy)

## Testing

```bash
go test ./internal/domain/sbom ./internal/app/sbom ./internal/cli/commands/sbom ./internal/app/build
```

Coverage includes project-type auto-detection, build-layer pickup (working-directory subdirs, multi-module Maven aggregate preference, npm `node_modules/` exclusion), gradle init-script contract, multi-layer runs, and ZIP packaging.
