<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Cargo application example

A Cargo workspace with two services (`hsm-worker`, `wallet-bff`) shipped as
separate container images. The example also shows how to extract the compiled
binaries as CI artefacts so they can be attached to a GitHub Release alongside
each container manifest.

## Why this shape (container-first)

reusable-ci handles two artefact lifecycle patterns:

- **artefact-first — platform-agnostic ecosystems** (maven, npm, gradle):
  `build-<lang>.yml` produces a deployable artefact (JAR, tarball);
  `publish-container.yml` downloads it and `COPY`s it into a thin runtime
  image.
- **container-first — compiled-native ecosystems** (cargo, go): the Containerfile
  is the build environment. `cargo build` runs inside the builder stage of
  the container build, the runtime image is the primary deliverable, and
  the binary is optionally extracted as a CI artefact via `extract.binary`.

Cargo follows container-first because Rust binaries are platform-specific; running
the compile inside the Containerfile means multi-arch (linux/amd64 +
linux/arm64) Just Works via buildkit + QEMU without per-target cross-compile
machinery in CI.

| Concern             | Where it runs                          | Why                                                        |
| ------------------- | -------------------------------------- | ---------------------------------------------------------- |
| `cargo build`       | inside each service's `Containerfile`  | multi-arch via buildx + QEMU; no cross-compile in CI       |
| `cargo test`        | caller's `test.yml`                    | workspace features can't be expressed per-artefact         |
| `cargo clippy/fmt`  | `lint-cargo.yml` (PR-time)             | one job for the whole workspace, fast feedback             |
| `cargo audit`       | `lint-cargo.yml` (PR-time)             | RUSTSEC advisories block PRs, not just releases            |
| Build SBOM          | `sbom-cargo.yml` (release-time)        | `cargo cyclonedx` reads `Cargo.lock`; no compile required  |
| Container scan SBOM | `publish-container.yml`                | derived from each service's effective-sboms                |
| Binary extraction   | `publish-container.yml` (`extract:`)   | reuses the container builder's compile; no double-build    |

`sbom-cargo.yml` does **only** SBOM generation. The actual compile happens
once in the Containerfile and is shared between the runtime image and the
binary extraction.

## Files

- `artifacts.yml` — declares two `cargo` artefacts and two containers, one
  per service. `target: runtime` selects the runtime stage; `extract.binary`
  opts into binary upload.
- `Containerfile.example` — reference multi-stage Containerfile with the
  three named stages (`builder`, `export-binary`, `runtime`). Copy to
  `<service>/Containerfile` and adjust per-service.
- `pullrequest-workflow.yml` — turns on clippy / rustfmt / cargo-audit and
  passes `cargo.apt-packages` so clippy can compile crates with native deps.
- `release-workflow.yml` — standard tag-driven release; the orchestrator
  dispatches `sbom-cargo` per artefact and `publish-container` per container.
  When `extract.binary` is set, each container's compiled binary is uploaded
  as `${name}-binaries` and aggregated into the GitHub Release.

## Containerfile pattern

The reference `Containerfile.example` uses three named stages:

1. **`builder`** — installs native deps via apt, `COPY`s workspace sources,
   runs `cargo build --release` with `--mount=type=cache` for
   `/usr/local/cargo/registry` and `target/`. Cache mounts persist across
   runs via the GHA cache backend (same mechanism every other ecosystem in
   reusable-ci uses for build caches).
2. **`export-binary`** — `FROM scratch`, just `COPY --from=builder` the
   binaries to root. Used by `extract.binary.target: export-binary` to
   write the binary to disk during container publish.
3. **`runtime`** — Debian-slim base, runtime deps installed via apt,
   non-root user, `COPY --from=builder` the binary, `ENTRYPOINT`. This is
   what gets pushed to the registry.

Build context is the workspace root (`context: .` in `artifacts.yml`),
so `COPY Cargo.toml Cargo.lock` and per-crate `COPY` statements resolve
against workspace-level paths.

The Containerfile works with both `docker build` and `podman build` (>=
4.0). For local dev with podman:

```bash
# Build the runtime image
podman build --target runtime --build-arg SERVICE=hsm-worker -t hsm-worker:dev .

# Extract just the binary
podman build --target export-binary -o ./out .
```

CI uses docker via `docker/build-push-action`. Local podman builds work
without docker installed.

## Workspace tests live in the caller's `test.yml`

Workspace-level features and testcontainers can't be expressed per-artefact,
so they live in the caller:

```yaml
# .github/workflows/test.yml
name: Cargo Test
on: [workflow_call]
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - run: rustup show active-toolchain >/dev/null 2>&1 || rustup toolchain install
      - uses: Swatinem/rust-cache@v2
      - run: |
          cargo test --workspace --features hsm-worker/testcontainers,wallet-bff/testcontainers \
            -- --test-threads=1
```

Pin the toolchain via `rust-toolchain.toml` at the repo root so CI and
local dev share one source of truth. `sbom-cargo.yml` and `lint-cargo.yml`
auto-detect this file via `scripts/cargo/install-toolchain.sh`.

## Adding a third service

1. New `artifacts:` entry with `project-type: cargo` and its
   `working-directory`.
2. New `containers:` entry referencing the artefact by name, pointing at
   `<service>/Containerfile`. Include `target:` and `extract:` if you want
   the binary as a CI artefact.
3. New `<service>/Containerfile` (copy `Containerfile.example` and adjust
   the COPY paths and which binary is built).
4. The release matrix expands automatically — no orchestrator changes
   needed.
