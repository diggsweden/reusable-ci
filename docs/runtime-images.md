<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Runtime Container Images

reusable-ci ships a family of OCI images that GitHub Actions workflows pull via
`container:`. Each image bundles the `reusable-ci` binary at
`/usr/local/bin/reusable-ci`, `templates/` at `/opt/reusable-ci/templates/`, the
narrow bootstrap installer surface at `/opt/reusable-ci/scripts/bootstrap/`, and
the toolchain a given workflow needs.

The goal is reproducible GitHub workflow execution today and reusable OCI image
building blocks for future GitLab CI components. A workflow that runs against a
pinned image digest gets the same binary, the same tools, and the same versions
on every runner that can pull that image.

## Image roster

Toolchain images carry the major version in their name (e.g.
`runtime-java-25`, `runtime-node-24`). When the next LTS rolls over, a
sibling image is added (`runtime-java-28`) and the old image keeps
building patch updates for its line until the upstream LTS goes EOL.
This makes consumer pinning unambiguous and avoids silent version moves.

| Image | Inherits from | Adds | Approx size |
|---|---|---|---|
| `reusable-ci-runtime-base` | `debian:13-slim` (Trixie) | `reusable-ci`, `bash`, `ca-certificates`, `curl`, `git`, `gnupg`, `gzip`, `jq`, `tar`, `unzip`, `xz`, `zip`, `yq`, `git-cliff`, `gh`, `glab`, `mise`, `publiccode-parser`, `templates/`, bootstrap installers | ~125 MB |
| `reusable-ci-runtime` (full) | `runtime-base` | `opengrep`, `syft`, `trivy` | ~575 MB |
| `reusable-ci-runtime-rust-stable` | `runtime-base` | Rust stable channel + `cargo-cyclonedx` | ~700 MB |
| `reusable-ci-runtime-go-1.26` | `runtime-base` | Go 1.26 toolchain + `cyclonedx-gomod` | ~500 MB |
| `reusable-ci-runtime-java-25` | `runtime-base` | Eclipse Temurin JDK 25 LTS + Maven 3.9.x + Gradle 8.x | ~500 MB |
| `reusable-ci-runtime-android-35` | `runtime-java-25` | Android cmdline-tools + platform-tools + build-tools 35 + android-35, no NDK | ~+900 MB |
| `reusable-ci-runtime-node-24` | `runtime-base` | Node 24 LTS + npm + corepack | ~150 MB |

## Layering

```text
debian:13-slim
└── runtime-base
    ├── runtime                  (security/SBOM tools)
    ├── runtime-rust-stable      (Rust toolchain)
    ├── runtime-go-1.26          (Go toolchain + cyclonedx-gomod)
    ├── runtime-java-25          (JDK 25 LTS + Maven + Gradle)
    │   └── runtime-android-35   (Android SDK platform 35, no NDK)
    └── runtime-node-24          (Node 24 LTS + npm + corepack)
```

`runtime-android-35` inheriting from `runtime-java-25` means there's one
JDK version, one Renovate update path, and the JDK layer is reused
across both images on the runner.

## Workflow → image mapping

| Workflow | Image | Notes |
|---|---|---|
| `lint-nanolinter` | `runtime-base` | nanolinter + its check tools (opengrep, osv-scanner, …) are mise-installed from the consumer's .mise.toml at runtime |
| `release-*-stage`, `pullrequest-quality-stage`, all orchestrators | `runtime-base` | Just bash + jq + yq for summaries |
| `release-create-github`, `release-dev-publish-stage`'s `generate-dev-sboms` | `runtime` (full) | Needs syft for SBOM aggregation |
| `generate-changelog`, `validate-release-prerequisites` | `runtime-base` normally; `runtime-rust-stable` when Cargo artifacts are present | git-cliff / gh + gpg are in base; Cargo prerequisite validation also checks `cargo --version` |
| `build-cargo`, `sbom-cargo` | `runtime-rust-stable` | Needs cargo + cargo-cyclonedx; `build-cargo` additionally relies on `rustup target add aarch64-unknown-linux-gnu` + `gcc-aarch64-linux-gnu` baked into the image for the default `linux/amd64,linux/arm64` cross-compile matrix |
| `build-go`, `sbom-go` | `runtime-go-1.26` | Needs Go + cyclonedx-gomod |
| `build-maven`, `build-gradle-app`, `publish-maven-central` | `runtime-java-25` | Needs JDK + Maven/Gradle |
| `build-gradle-android` | `runtime-android-35` | JDK + Android SDK |
| `publish-google-play` | `runtime-base` | Upload action is JS-only; no Android tooling needed |
| `build-npm`, `publish-dev-npm` | `runtime-node-24` | Node + npm + corepack baked |
| `publish-container`, `publish-dev-container` | host + Docker actions | DinD blocks `container:` use |
| `security-openssf-scorecard` | host + third-party Docker action | Same |
| `build-xcode-ios`, `publish-apple-appstore` | macOS host + `go install` from `reusable-ci-binary-ref` | macOS can't run Linux containers |

## Why some images aren't built

- **`reusable-ci-runtime-android-35-ndk`**: Not built today. NDK is ~3 GB and only needed for projects with native (JNI) code — the minority. The current `reusable-ci-runtime-android-35` image covers typical Android library and app builds without NDK.
- **Multiple LTS lines simultaneously** (`runtime-java-21` *and* `runtime-java-25`, etc.): not built today.
  The current LTS gets a versioned image (e.g. `runtime-java-25`); the next LTS gets a sibling
  (`runtime-java-28`) when it lands. Old LTS images keep building patch updates until upstream EOL,
  then are dropped. Maintaining many LTS variants in parallel is the explicit non-goal — the version
  in the name is documentation of "the major line this image targets", not a promise of multi-version
  support.

## Pinning convention

| Toolchain | Default in image | Override |
|---|---|---|
| Rust | stable channel, Renovate-bumped | `runtime-image:` to a different tag, or `actions/setup-rust` |
| Go | 1.26.x | `runtime-image:` to a different tag, or `actions/setup-go` |
| JDK | Eclipse Temurin 25 LTS | `runtime-image:` to a different tag, or `actions/setup-java@v4` |
| Maven | 3.9.x latest | Project's `mvnw` always wins |
| Gradle | 8.x latest | Project's `gradlew` always wins |
| Node | 24 LTS | Project's `package.json` engines / `corepack` selects npm/yarn/pnpm |
| Android cmdline-tools | latest stable | AGP downloads what `compileSdkVersion` needs at build time |
| Android build-tools | 35.0.0 | AGP downloads additional versions on demand |
| Android SDK platform | android-35 | Same |

## Tag convention

All runtime images follow the same tag scheme:

| Tag | When | Stable to pin? |
|---|---|---|
| `:vX.Y.Z` | On release tag push | Yes — pin in production |
| `:vX.Y` | On release tag push (LTS-style major.minor) | Yes |
| `:vX` | On release tag push | Yes — moves on minor |
| `:<branch>` | Every published branch build (e.g. `:main`, `:feat-refactor-go`) | No — moves with the branch |
| `:v3-pre` | Pre-v3.0.0 branch/dispatch builds | No — moves to the latest pre-release |
| `:v3-pre-<YYYYMMDD-HHMMSS>` | Pre-v3.0.0 builds, from the commit timestamp | Yes — immutable build stamp |
| `:weekly` | Weekly cron rebuild (refreshes pinned tools + base) | Yes for opt-in users |
| `:sha-<short-commit>` | main / branch / dispatch builds | Yes — exact commit |
| `@sha256:<digest>` | Per build | Yes — strictest pin |

Normal consumers do not need to set `runtime-image*` inputs. Released workflows
default to the matching reusable-ci runtime image line.

External consumers who need stricter reproducibility can pin by **digest**
(`@sha256:…`) or override images by `:vX.Y.Z`.

## OCI labels

Every image carries `org.opencontainers.image.{title, source, url, revision,
version, created}` (set at build time). They identify an image via `docker
inspect` or the GHCR UI even when tags have been pruned, and `source` links the
GHCR package back to this repository. `licenses` is intentionally left unset —
a runtime image bundles many differently-licensed tools.

## CLI binary channels

The `reusable-ci` CLI is built once per commit and used everywhere: the
byte-identical linux binary is baked into every runtime image, and the *same*
build is published as signed tarballs (linux/darwin × amd64/arm64). Container
jobs use the baked binary; non-container jobs (e.g. macOS swift/iOS) download a
tarball, selected by `reusable-ci-binary-ref`:

| `reusable-ci-binary-ref` | Channel | Stable to pin? |
|---|---|---|
| `vX.Y.Z` | Release tarball, cosign-pinned to `release-binary.yml@vX.Y.Z` | Yes — production |
| `v3.0.0-pre` | Rolling pre-release from the latest dev-branch build, cosign-pinned to `build-cli.yml@refs/heads/…` | No — moves; not for production |

Both verify by SHA-256 + Sigstore. The pre-release channel is a separate, opt-in trust
domain — it never relaxes the release pin — and mirrors the image `:v3-pre` tag.
`install-reusable-ci.sh` falls back to `go install` if download or verification
fails.

## Advanced Pinning Recipe

When reusable-ci cuts a release, every image gets a matching semver tag. Override
runtime images only when you need digest pinning, mirrored registries, or branch
testing:

```yaml
# .github/workflows/release.yml in a consumer repo
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v3.0.0
    with:
      artifacts-config: .reusable-ci/artifacts.yml
      # Optional: override every runtime image family used by the orchestrator.
      # Normal consumers can omit these because workflow defaults already use
      # the matching release image line.
      runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-base:v3.0.0
      runtime-image-full: ghcr.io/diggsweden/reusable-ci-runtime:v3.0.0
      runtime-image-java: ghcr.io/diggsweden/reusable-ci-runtime-java-25:v3.0.0
      runtime-image-node: ghcr.io/diggsweden/reusable-ci-runtime-node-24:v3.0.0
      runtime-image-rust: ghcr.io/diggsweden/reusable-ci-runtime-rust-stable:v3.0.0
      runtime-image-go: ghcr.io/diggsweden/reusable-ci-runtime-go-1.26:v3.0.0
      runtime-image-android: ghcr.io/diggsweden/reusable-ci-runtime-android-35:v3.0.0
      reusable-ci-binary-ref: v3.0.0
      # Renovate-friendly: bump the @v3.0.0, :v3.0.0, and binary ref in lockstep.
```

For direct workflow calls that take one `runtime-image:` input, omit it for the
default or override it with the version-named variant that matches that workflow:

```yaml
# Java workflow:
runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-java-25:v3.0.0
# Android workflow:
runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-android-35:v3.0.0
# Node workflow:
runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-node-24:v3.0.0
# Rust workflow:
runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-rust-stable:v3.0.0
# Go workflow:
runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-go-1.26:v3.0.0
```

For the strictest reproducibility, pin by digest:

```yaml
runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-java-25@sha256:abc123…
```

When the next Java LTS lands (Java 28), the workflow file at v4.0.0 will default
to the Java 28 runtime image line. Consumers on v3.x keep getting Java 25 because
the v3 workflow defaults and any explicit v3 pins still use `runtime-java-25`.
Migration is explicit and opt-in.

### Renovate

If you override `runtime-image:` strings, they are Renovate-friendly. Consumers
using Renovate can pick up image bumps automatically:

```json
// renovate.json
{
  "packageRules": [
    {
      "matchDatasources": ["docker"],
      "matchPackagePatterns": ["reusable-ci-runtime"],
      "groupName": "reusable-ci runtime images"
    }
  ]
}
```

Renovate-driven bumps in the consumer repo + the workflow ref bump
(`uses: …@v3.0.1`) should land in the same PR for atomic version moves.

## Branch Testing

Testing unreleased reusable-ci workflow changes from a consumer repository
requires two pins:

1. The workflow ref (`uses: diggsweden/reusable-ci/.github/workflows/...@<branch-or-sha>`).
2. The runtime/binary ref used inside jobs.

For Linux containerized jobs, first run `Self Runtime Container` manually on
the reusable-ci branch with `publish=true`. That publishes every runtime
variant as `:sha-<short-sha>` for that reusable-ci commit. Then pass those
image tags through the orchestrator inputs:

```yaml
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@abcdef0123456789abcdef0123456789abcdef01
    with:
      artifacts-config: .reusable-ci/artifacts.yml
      reusable-ci-binary-ref: abcdef0123456789abcdef0123456789abcdef01
      runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-base:sha-abcdef0
      runtime-image-full: ghcr.io/diggsweden/reusable-ci-runtime:sha-abcdef0
      runtime-image-java: ghcr.io/diggsweden/reusable-ci-runtime-java-25:sha-abcdef0
      runtime-image-node: ghcr.io/diggsweden/reusable-ci-runtime-node-24:sha-abcdef0
      runtime-image-rust: ghcr.io/diggsweden/reusable-ci-runtime-rust-stable:sha-abcdef0
      runtime-image-go: ghcr.io/diggsweden/reusable-ci-runtime-go-1.26:sha-abcdef0
      runtime-image-android: ghcr.io/diggsweden/reusable-ci-runtime-android-35:sha-abcdef0
```

The plain-runner jobs (container publish and macOS workflows) do not use the
runtime image. They install the Go binary from `reusable-ci-binary-ref`, so
that value must point at the same reusable-ci commit as the workflow and runtime
images being tested. Branch names work for quick iteration, but commit SHAs
avoid drift after publishing `:sha-<short-sha>` images.

## Releasing new image versions

The image release pipeline runs automatically on every reusable-ci semver
tag push:

1. Cut a normal reusable-ci release (`chore(release): vX.Y.Z`).
2. The release-orchestrator pushes the `vX.Y.Z` git tag.
3. `self-runtime-container.yml` fires on the tag push and rebuilds **all**
   runtime image variants from that commit, publishing them with `:vX.Y.Z`,
   `:vX.Y`, and `:vX` tags.
4. Multi-arch (`linux/amd64`, `linux/arm64`) is built natively per leg.

No separate "release the images" step exists — the image release is a
side effect of the reusable-ci release. Drift between workflow file
version and image version is structurally impossible because both come
from the same commit.

The path filter on the `push` trigger is intentionally absent for tag
pushes, so a release tag rebuilds the images even if the tagged commit
didn't touch the runtime files.

## Build pipeline

`.github/workflows/self-runtime-container.yml` builds all variants from a
single multi-stage `Containerfile` via a matrix over targets. Each target
publishes to its own image name (`reusable-ci-runtime-X`) under
`ghcr.io/diggsweden/`. The build order respects layering: `base`, `rust`,
`node` build from base in parallel; `java` builds from base; `android`
builds from `java`.

Tag pushes always rebuild every variant so release tags reliably get a
matching image.

## GitLab compatibility building blocks

Every image lives on GHCR, which is reachable from GitLab CI runners. These are
building blocks for future GitLab CI components; this repository does not ship a
GitLab Catalog end-user contract yet. For sovereign GitLab installations that
must not pull from `ghcr.io`, mirror the images to the consumer's own GitLab
Container Registry and point the future GitLab component's image setting at the
mirror.

For macOS workflows, the equivalent ref input is `reusable-ci-binary-ref`.
Those workflows run on a macOS VM and install the Go binary with
`go install github.com/diggsweden/reusable-ci/cmd/reusable-ci@<ref>` instead
of using the Linux runtime image.
