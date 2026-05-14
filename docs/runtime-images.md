<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Runtime Container Images

reusable-ci ships a family of OCI images that workflows pull via `container:`
(GitHub) or `image:` (GitLab). Each image bundles `scripts/` at
`/opt/reusable-ci/scripts/`, `templates/` at `/opt/reusable-ci/templates/`,
and the toolchain a given workflow needs.

The goal is **same image on both platforms**. A workflow that runs against
a pinned image digest gets the same scripts, the same tools, and the same
versions whether the runner is GitHub or GitLab.

## Image roster

Toolchain images carry the major version in their name (e.g.
`runtime-java-25`, `runtime-node-24`). When the next LTS rolls over, a
sibling image is added (`runtime-java-28`) and the old image keeps
building patch updates for its line until the upstream LTS goes EOL.
This makes consumer pinning unambiguous and avoids silent version moves.

| Image | Inherits from | Adds | Approx size |
|---|---|---|---|
| `reusable-ci-runtime-base` | `debian:13-slim` (Trixie) | `bash`, `ca-certificates`, `curl`, `git`, `gnupg`, `gzip`, `jq`, `tar`, `unzip`, `xz`, `zip`, `yq`, `git-cliff`, `gh`, `glab`, `scripts/`, `templates/` | ~125 MB |
| `reusable-ci-runtime` (full) | `runtime-base` | `opengrep`, `syft`, `trivy` | ~575 MB |
| `reusable-ci-runtime-rust-stable` | `runtime-base` | Rust stable channel + `cargo-cyclonedx` | ~700 MB |
| `reusable-ci-runtime-java-25` | `runtime-base` | Eclipse Temurin JDK 25 LTS + Maven 3.9.x + Gradle 8.x | ~500 MB |
| `reusable-ci-runtime-android-35` | `runtime-java-25` | Android cmdline-tools + platform-tools + build-tools 35 + android-35 | ~+900 MB |
| `reusable-ci-runtime-android-35-ndk` | `runtime-android-35` | NDK r26 | ~+3 GB |
| `reusable-ci-runtime-node-24` | `runtime-base` | Node 24 LTS + npm + corepack | ~150 MB |

## Layering

```text
debian:13-slim
└── runtime-base
    ├── runtime                  (security/SBOM tools)
    ├── runtime-rust-stable      (Rust toolchain)
    ├── runtime-java-25          (JDK 25 LTS + Maven + Gradle)
    │   └── runtime-android-35   (Android SDK platform 35, no NDK)
    │       └── runtime-android-35-ndk     (future)
    └── runtime-node-24          (Node 24 LTS + npm + corepack)
```

`runtime-android-35` inheriting from `runtime-java-25` means there's one
JDK version, one Renovate update path, and the JDK layer is reused
across both images on the runner.

## Workflow → image mapping

| Workflow | Image | Notes |
|---|---|---|
| `security-opengrep`, `security-dependency-review` | `runtime` (full) | Needs opengrep/trivy |
| `release-*-stage`, `pullrequest-quality-stage`, all orchestrators | `runtime-base` | Just bash + jq + yq for summaries |
| `release-create-github`, `release-dev-publish-stage`'s `generate-dev-sboms` | `runtime` (full) | Needs syft for SBOM aggregation |
| `generate-changelog`, `validate-release-prerequisites` | `runtime-base` | git-cliff / gh + gpg are in base |
| `sbom-cargo` | `runtime-rust-stable` | Needs cargo + cargo-cyclonedx |
| `build-maven`, `build-gradle-app`, `publish-maven-central` | `runtime-java-25` | Needs JDK + Maven/Gradle |
| `build-gradle-android` | `runtime-android-35` | JDK + Android SDK |
| `publish-google-play` | `runtime-base` | Upload action is JS-only; no Android tooling needed |
| `build-npm`, `publish-dev-npm` | `runtime-node-24` | Node + npm + corepack baked |
| `publish-container`, `publish-dev-container` | host + Docker actions | DinD blocks `container:` use |
| `security-openssf-scorecard` | host + third-party Docker action | Same |
| `build-xcode-ios`, `publish-apple-appstore` | macOS host + `scripts-ref` archive fetch | macOS can't run Linux containers |

## Why some images aren't built

- **`runtime-go`**: GHA hosted runners ship Go pre-installed; almost no `scripts/` code uses Go. Not worth a dedicated image.
- **`runtime-android-ndk`**: NDK is ~3 GB and only needed for projects with native (JNI) code — the minority. The base `runtime-android` (~900 MB without NDK) covers typical Android library and app builds. Build the NDK variant when a real consumer needs it.
- **Multiple LTS lines simultaneously** (`runtime-java-21` *and* `runtime-java-25`, etc.): not built today. The current LTS gets a versioned image (e.g. `runtime-java-25`); the next LTS gets a sibling (`runtime-java-28`) when it lands. Old LTS images keep building patch updates until upstream EOL, then are dropped. Maintaining many LTS variants in parallel is the explicit non-goal — the version in the name is documentation of "the major line this image targets", not a promise of multi-version support.

## Pinning convention

| Toolchain | Default in image | Override |
|---|---|---|
| Rust | stable channel, Renovate-bumped | `runtime-image:` to a different tag, or `actions/setup-rust` |
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
| `:main` | On every push to main | No — moves frequently |
| `:weekly` | Weekly cron rebuild (refreshes pinned tools + base) | Yes for opt-in users |
| `:sha-<commit>` | On main / dispatch | Yes for testing a specific build |
| `@sha256:<digest>` | Per build | Yes — strictest pin |

External consumers should pin by **digest** (`@sha256:…`) for reproducibility,
or by `:vX.Y.Z` if they're OK with the patch-version move.

## Consumer pinning recipe

When reusable-ci cuts a release, every image gets a matching semver tag.
External consumers pin both the workflow ref and the runtime-image to
the same version:

```yaml
# .github/workflows/release.yml in a consumer repo
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v3.0.0
    with:
      artifacts-config: .github/artifacts.yml
      # Pin the runtime image to the matching version. The image name carries
      # the toolchain version (java-25, node-24, …) so future LTS bumps are
      # explicit, not silent.
      runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-base:v3.0.0
      # Renovate-friendly: bump both the @v3.0.0 and the :v3.0.0 in lockstep.
```

For workflows that need a toolchain image, pin to the version-named
variant:

```yaml
# Java workflow:
runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-java-25:v3.0.0
# Android workflow:
runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-android-35:v3.0.0
# Node workflow:
runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-node-24:v3.0.0
# Rust workflow:
runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-rust-stable:v3.0.0
```

For the strictest reproducibility, pin by digest:

```yaml
runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-java-25@sha256:abc123…
```

When the next Java LTS lands (Java 28), the workflow file at v4.0.0 will
default to `runtime-java-28:main`. Consumers on v3.x keep getting Java 25
because they pinned `runtime-java-25:v3.x`. Migration is explicit and
opt-in.

### Renovate

The `runtime-image:` strings are Renovate-friendly. Consumers using
Renovate can pick up image bumps automatically:

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

## GitLab compatibility

Every image lives on GHCR, which is reachable from GitLab CI runners. For
sovereign GitLab installations that must not pull from `ghcr.io`, mirror
the images to the consumer's own GitLab Container Registry. The
`runtime-image:` input then points at the mirror; the rest of the workflow
is unchanged.

For macOS workflows, the equivalent ref input is `scripts-ref` plus
`scripts-archive-url` — a versioned source archive of `reusable-ci`. See
[`gitlab-completed.md`](gitlab-completed.md) for that path.
