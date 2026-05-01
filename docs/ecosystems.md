<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Ecosystem support

This document is the canonical "what's supported per ecosystem" reference
for reusable-ci. Each ecosystem section follows the same matrix format so
they can be compared at a glance.

## How reusable-ci classifies ecosystems

Different language ecosystems have different deliverable shapes, so
reusable-ci handles them in two complementary patterns. The pattern is
determined by **where platform commitment naturally happens** in the
language's build model.

### artefact-first: platform-agnostic deliverable

```text
source ─▶ build-<lang>.yml ─▶ artefact (JAR, tarball) ─┐
                                                       │
                                                       ▼
                                          publish-container.yml ─▶ image
                                          (downloads artefact, COPYs in)
```

`build-<lang>.yml` produces a deployable artefact (JAR, NPM tarball,
APK, IPA). The container build downloads it via `from: [<artefact>]`
and `COPY`s it into a thin runtime image.

**Implemented for:** maven, gradle, gradle-android, npm, xcode-ios.

### container-first: compiled-native, container is the build environment

```text
source ─┬─▶ sbom-<lang>.yml ─▶ Build SBOM (manifest-derived)
        │   (no compile, reads Cargo.lock / go.sum)
        │
        └─▶ publish-container.yml
              ├─ Containerfile builder stage compiles the binary
              ├─ runtime stage receives the binary
              └─ optional `extract.binary` re-uses the compile to
                 produce a CI artefact alongside the image
```

The Containerfile is the build environment. The runtime image is the
primary deliverable; the binary is a byproduct, optionally extracted as a
CI artefact via `extract.binary`. Platform commitment happens at
`--platform=` time on the container build, which is where it naturally
belongs for compiled-native code.

**Implemented for:** cargo. Reserved (placeholder, unimplemented): go.

### Why two patterns

Forcing compiled-native ecosystems into artefact-first would require
cross-compile machinery in CI (cargo-zigbuild, cross-rs, native-apt
sysroots) — significant complexity for a problem that split-runner
multi-arch (`ubuntu-24.04` for amd64 + `ubuntu-24.04-arm` for arm64)
already solves inside the Containerfile. container-first accepts that the
container IS the build environment and orchestrates accordingly. The
naming reflects this: `build-<lang>.yml` for artefact-first (produces the
artefact), `sbom-<lang>.yml` for container-first (handles platform-agnostic
side-concerns; SBOM is one of them).

When a future language has both patterns available (e.g., python with
wheel as artefact-first or pyproject manifest SBOM as container-first), reusable-ci
picks the pattern that matches how the language actually ships in
practice.

## Per-ecosystem support matrices

Each ecosystem below uses the same matrix structure. Future ecosystem
reviews drop into this template.

---

### Cargo (container-first)

| Property | Value |
|---|---|
| Pattern | container-first (compiled-native) |
| project-type identifier | `cargo` |
| Canonical tool | cargo (Rust's universal package manager + build tool) |
| Status | Production |
| Tracked since | v2.9.0 |

#### Capabilities

| Concern | Status | Workflow / Mechanism | Notes |
|---|---|---|---|
| Build (standalone deliverable) | n/a | n/a | container-first: compile happens inside Containerfile |
| Lint | ✅ | `lint-cargo.yml` | clippy + rustfmt + cargo-audit + opt-in cargo-deny |
| Test | external | caller's `test.yml` | Workspace features / testcontainers can't be per-artefact |
| SBOM — build layer | ✅ | `sbom-cargo.yml` (cargo-cyclonedx) | From Cargo.lock; no compile required |
| SBOM — analyzed-artifact | ❌ | — | Would require pre-built binary upload; not in v2.9 path |
| SBOM — analyzed-container | ✅ | `publish-container.yml` (syft) | Standard for all containers; derived from `sboms` |
| Container build | ✅ | `publish-container.yml` | Native split-runner multi-arch (no QEMU) |
| Multi-arch (linux variants) | ✅ | split-runner matrix | linux/amd64 → `ubuntu-24.04`, linux/arm64 → `ubuntu-24.04-arm`, merged into one manifest list |
| Multi-arch (macOS / Windows) | ❌ | — | Linux-only for containers |
| Binary extraction (CI artefact) | ✅ | `extract.binary` field | Opt-in; uploads as `${name}-binaries-${arch}` (per-arch, see Phase 3 of `planarch.md`) |
| Multi-binary in one container | ✅ | `extract.binary.names: [a, b]` | E.g., `hsm-worker` + `digg-hsm-keytool`; basenames suffixed with `-linux-${arch}` to avoid release-asset collision |
| Workspace (multi-crate) | ✅ | sbom-cargo `--all`; bump-version `[workspace.package]` | One bump per release |
| Single-crate | ✅ | Same workflows; bump-version `[package].version` | |
| Version-bump (Cargo.toml + Cargo.lock) | ✅ | `bump-version.sh cargo` | `cargo update --workspace` syncs lockfile |
| Release prerequisite checks | ✅ | `validate-release-prerequisites.yml` | Cargo.lock + toolchain pin |
| Publish — container to ghcr | ✅ | `publish-container.yml` | SLSA provenance + scan + analyzed-container SBOM |
| Publish — container to other OCI registries | ✅ | `publish-container.yml` | docker.io, quay.io, etc. (no SLSA outside ghcr) |
| Publish — crates.io (libraries) | ❌ | — | TODO: `publish-cratesio.yml` (v2.10+) |
| Standalone binary release (no container, GitHub Release attach) | partial | via `extract.binary` requires container scaffolding | Pure-binary release without container needs new workflow (v2.10+) |
| macOS binary distribution | ❌ | — | Would need cross-compile tooling or macOS runner |
| Windows binary distribution | ❌ | — | Same as macOS |

#### Caller responsibilities

- **Per-service multi-stage `Containerfile`** with named stages: `builder`, optional `export-binary`, and a runtime stage referenced by `target:` in `artifacts.yml`.
- **Workspace tests** in caller's own `test.yml`, called from the PR orchestrator. `cargo test --workspace` can't be expressed per-artefact.
- **Native build deps** inside the Containerfile builder stage (`apt-get install`).
- **Native lint deps** via `cargo.apt-packages` orchestrator input — clippy compiles, so the same packages must be available.
- **`Cargo.lock` checked in** — required for reproducible SBOM and release.
- **`rust-toolchain.toml` recommended** — used by both `lint-cargo.yml` and `sbom-cargo.yml` for toolchain selection. Not strictly required.

#### See also

- [Container schema fields](artifacts-reference.md#container-optional-fields)
- [SBOM patterns](sbom.md)
- [Cargo example](../examples/cargo-app/)

---

### Maven (artefact-first)

| Property | Value |
|---|---|
| Pattern | artefact-first (platform-agnostic deliverable) |
| project-type identifier | `maven` |
| Canonical tool | Maven |
| Status | Production |
| Tracked since | v2.0.0 |

This section's full capability matrix is documented at the workflow level
(see [build-maven.yml](../.github/workflows/build-maven.yml) and
[publish-maven-central.yml](../.github/workflows/publish-maven-central.yml)).
A normalized matrix matching the cargo section above is planned in a
follow-up PR.

---

### NPM (artefact-first)

| Property | Value |
|---|---|
| Pattern | artefact-first (platform-agnostic deliverable) |
| project-type identifier | `npm` |
| Canonical tool | npm (dominant package manager for the Node.js ecosystem) |
| Status | Production |

Full normalized matrix planned in a follow-up PR.

---

### Gradle (artefact-first — JVM)

| Property | Value |
|---|---|
| Pattern | artefact-first (platform-agnostic deliverable) |
| project-type identifier | `gradle` |
| Canonical tool | Gradle |
| Status | Production |

Full normalized matrix planned in a follow-up PR.

---

### Gradle Android (artefact-first)

| Property | Value |
|---|---|
| Pattern | artefact-first (Android-specific deliverable: APK / AAB) |
| project-type identifier | `gradle-android` |
| Status | Production |

Full normalized matrix planned in a follow-up PR.

---

### Xcode iOS (artefact-first)

| Property | Value |
|---|---|
| Pattern | artefact-first (iOS/macOS-specific deliverable: IPA) |
| project-type identifier | `xcode-ios` |
| Status | Production |

Full normalized matrix planned in a follow-up PR.

---

### Python (placeholder)

| Property | Value |
|---|---|
| Pattern | TBD |
| project-type identifier | `python` |
| Status | Reserved — recognized in `parse-artifacts-config.sh` but no workflows implement it yet |

Python's build tool landscape (pip, poetry, uv, hatch, flit, setuptools)
has no canonical winner, so the eventual choice between artefact-first
(build wheel, COPY into container) and container-first (manifest-based SBOM
only, container does the build) depends on the first real caller.

---

### Go (placeholder)

| Property | Value |
|---|---|
| Pattern | TBD (likely container-first by analogy with cargo) |
| project-type identifier | `go` |
| Status | Reserved — recognized in `parse-artifacts-config.sh` but no workflows implement it yet |

Go's tool and language share a name (`go build`, `go.mod`). When
implemented, it'll likely follow container-first (cargo's shape) since Go also
ships native binaries with the same multi-arch container considerations.

---

## Adding a new ecosystem

When adding support for a new ecosystem:

1. Pick the pattern that matches how the language actually ships in
   production (don't force-fit).
2. Add the project-type identifier to `VALID_PROJECT_TYPES` in
   `scripts/config/parse-artifacts-config.sh` (and `SBOM_SUPPORTED_TYPES`
   if it produces SBOMs).
3. For artefact-first: add `build-<tool>.yml`. For container-first: add
   `sbom-<tool>.yml` plus document the caller's Containerfile contract.
4. Wire into `release-build-stage.yml` matrix (if a build/sbom workflow
   produces something) and/or `release-publish-stage.yml` (if a new
   publisher is needed).
5. Add a section to this document with the same matrix structure as the
   cargo section above.
6. Add a working example under `examples/<ecosystem>-app/`.

The matrix structure is uniform on purpose: a reader comparing two
ecosystems can scan the same row labels and immediately see what differs.
