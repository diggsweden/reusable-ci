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

reusable-ci handles two artefact lifecycle patterns, and Cargo (like Go)
supports both via `config.build-mode`:

- **artefact-first — platform-agnostic ecosystems + cross-compiled binaries**
  (maven, npm, gradle, plus go/cargo with `build-mode: artifact-first`):
  `build-<lang>.yml` produces a deployable artefact (JAR, tarball, binary);
  `publish-container.yml` downloads it and `COPY`s it into a thin runtime
  image. Standalone-CLI releases use this path.
- **container-first — compile-inside-Containerfile** (go/cargo with
  `build-mode: container-first`): the Containerfile is the build environment.
  `cargo build` runs inside the builder stage of the container build, the
  runtime image is the primary deliverable, and the binary is optionally
  extracted as a CI artefact via `extract.binary`.

This example uses **container-first** because it ships Rust *services* as
container images, not standalone CLIs. Running the compile inside the
Containerfile means multi-arch (linux/amd64 + linux/arm64) works via
buildkit on native per-architecture runners — no cross-compile linker
needed in CI, and native system deps (`apt-get install libssl-dev`) stay
with the build environment.

For a standalone Rust CLI binary released on GitHub Releases, choose
`build-mode: artifact-first` instead — `build-cargo.yml` cross-compiles to
`dist/<goos>-<goarch>/<binary>-<goos>-<goarch>` (same shape as Go),
attaches SBOMs and signatures, and the runtime image carries the
`linux/amd64 + linux/arm64` cross-linker matrix.

| Concern             | Where it runs                          | Why                                                        |
| ------------------- | -------------------------------------- | ---------------------------------------------------------- |
| `cargo build`       | inside each service's `Containerfile`  | multi-arch via native buildx runners; no cross-compile     |
| `cargo test`        | caller's `test.yml`                    | workspace features can't be expressed per-artefact         |
| `cargo clippy/fmt`  | caller's `test.yml`                    | workspace-specific quality gates stay in the consumer repo |
| `cargo audit`       | caller's `test.yml`                    | RUSTSEC policy can match the consumer's dependency model   |
| Build SBOM          | `sbom-cargo.yml` (publish stage)       | `cargo cyclonedx` reads `Cargo.lock`; no compile required (artefact-first would use the inline SBOM step in `build-cargo.yml` instead) |
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
- `pullrequest-workflow.yml` — runs reusable-ci nanolinter checks.
  Add a caller-owned `test.yml` for workspace-specific Rust checks.
- `release-workflow.yml` — standard tag-driven release; the orchestrator
  dispatches `sbom-cargo` per artefact and `publish-container` per container.
  When `extract.binary` is set, each container's compiled binary is uploaded
  as `${name}-binaries-${arch}` per platform leg and aggregated into the
  GitHub Release.

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
so they live in the caller. The reusable-ci PR orchestrator does not invoke
this workflow automatically; call it from your own PR workflow if you need it.

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

**Required:** pin the toolchain via `rust-toolchain.toml` at every Cargo
artefact's `working-directory`. `validate cargo` enforces this — a
release will fail at the prerequisites stage if the pin is missing.
`sbom-cargo.yml` auto-detects the file directly.

## Private cargo registry credentials (build-time)

If the workspace pulls from a private cargo registry, **do not** put the
token in `containers[].build-args` — BuildKit records build-arg values
verbatim in SLSA `mode=max` provenance, so the token would end up in the
public image attestation.

Use `containers[].build-secrets` instead. Declare the name once:

```yaml
containers:
  - name: hsm-worker
    from: [hsm-worker]
    container-file: hsm-worker/Containerfile
    context: .
    build-secrets:
      - PRIVATE_CARGO_REGISTRY_TOKEN
```

Consume it in the `builder` stage with a tmpfs mount:

```dockerfile
RUN --mount=type=secret,id=private_cargo_registry_token,target=/run/secrets/cargo-token \
    CARGO_REGISTRIES_INTERNAL_TOKEN="$(cat /run/secrets/cargo-token)" \
    cargo build --release
```

The caller workflow forwards a single `REUSABLE_CI_BUILD_SECRETS_JSON`
envelope; see [docs/artifacts-reference.md `build-secrets`](../../docs/artifacts-reference.md#build-secrets)
for the full recipe.

## Adding a third service

1. New `artifacts:` entry with `project-type: cargo`, its
   `working-directory`, and `config.build-mode: container-first` (this
   example wraps each crate in its own Containerfile; for a standalone
   CLI binary release, use `artifact-first` instead — see the Go-style
   dual-mode comparison in `docs/ecosystems.md#cargo-dual-mode`).
2. New `containers:` entry referencing the artefact by name, pointing at
   `<service>/Containerfile`. Include `target:` and `extract:` if you want
   the binary as a CI artefact.
3. New `<service>/Containerfile` (copy `Containerfile.example` and adjust
   the COPY paths and which binary is built).
4. The release matrix expands automatically — no orchestrator changes
   needed.
