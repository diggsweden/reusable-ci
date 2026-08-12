# Reusable CI/CD Workflows

<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

[![Tag](https://img.shields.io/github/v/tag/diggsweden/reusable-ci?style=for-the-badge&color=green)](https://github.com/diggsweden/reusable-ci/tags)

[![License: EUPL-1.2 OR GPL-3.0-or-later](https://img.shields.io/badge/License-EUPL--1.2%20OR%20GPL--3.0--or--later-blue?style=for-the-badge)](LICENSE)
[![REUSE](https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fapi.reuse.software%2Fstatus%2Fgithub.com%2Fdiggsweden%2Freusable-ci&query=status&style=for-the-badge&label=REUSE&color=lightblue)](https://api.reuse.software/info/github.com/diggsweden/reusable-ci)

[![OpenSSF Scorecard](https://img.shields.io/ossf-scorecard/github.com/diggsweden/reusable-ci?label=openssf+scorecard&style=for-the-badge)](https://scorecard.dev/viewer/?uri=github.com/diggsweden/reusable-ci)

Reusable GitHub Actions workflows for build, publish, release, and PR
quality, backed by the `reusable-ci` Go CLI. Targets adopters who want
SBOMs, signing, SLSA provenance, and pinned third-party actions
without rebuilding each piece per project.

The examples below target the v3.0.0 workflow contract. Before that
release tag and its matching runtime images exist, use the
branch-testing flow in [Runtime Images](docs/runtime-images.md).

## Documentation

Start with the [Documentation Index](docs/README.md) for the full Diataxis map.

**Tutorials and examples:**
- [Examples](examples/README.md) - Choose a ready-made workflow for your ecosystem

**How-to guides:**
- [Publishing Guide](docs/publishing.md) - Maven Central, NPM, registries, App Store, and Google Play
- [Runtime Images](docs/runtime-images.md) - Optional runtime image overrides, mirroring, and branch testing
- [Artifact Verification](docs/verification.md) - Verify release assets, signatures, attestations, and SBOMs

**Reference:**
- [Artifacts Reference](docs/artifacts-reference.md) - `artifacts.yml` schema, defaults, and constraints
- [Components Reference](docs/components.md) - Reusable workflow components and direct-use caveats
- [CLI Reference](docs/cli-reference.md) - Generated `reusable-ci` command reference
- [Secrets and Permissions](docs/reference.md) - Required secrets, permissions, and validation matrices

**Explanation:**
- [Workflow Architecture](docs/workflows.md) - Control-plane architecture and stage contracts
- [Ecosystems](docs/ecosystems.md) - Ecosystem patterns and support status
- [SBOM Guide](docs/sbom.md) - SBOM layer model and defaults

---

## Introduction

Three top-level chains, each invoked from the adopter's repo as a
single `workflow_call:` entry:

1. **Pull Request** (`pullrequest-orchestrator.yml`) runs on PR and
   push: nanolinter (lint, security scanning, license/REUSE), with security
   findings uploaded to Code Scanning as SARIF. The adopter's own tests are
   wired separately.

2. **Release** (`release-orchestrator.yml`) runs on signed tag push:
   parse `.reusable-ci/artifacts.yml`, validate release prerequisites,
   build the declared artifacts, publish them to their declared
   targets (Maven Central / GitHub Packages / npm / Google Play / App
   Store Connect), build and push containers, generate SBOMs at the
   three CISA layers, sign with GPG or cosign, create the GitHub
   release with changelog and checksums.

3. **Snapshot Release** (`release-snapshot-orchestrator.yml`) uses the same
   control-plane shape on branch pushes, with no signing / no SLSA / no
   permanent release; publishes a branch-suffixed npm snapshot (and
   optional SBOMs). It builds no containers. A container is built once on
   the release path and promoted to `:dev` by the promotion ladder.

---

### Getting Started

Most projects need only the workflow files:

1. `.github/workflows/pullrequest-workflow.yml` - For PR checks
2. `.github/workflows/release-workflow.yml` - For production releases
3. `.github/workflows/release-snapshot-workflow.yml` - (Optional) For snapshot (feature-branch) releases

A single-manifest repository (one root `go.mod`, `Cargo.toml`, `pom.xml`,
`package.json`, or Gradle build) needs no configuration file at all.
The plan is auto-derived from the manifest. Add `.reusable-ci/artifacts.yml`
only when you need more than the derived defaults (multiple artifacts,
containers, publish targets). Verify a setup any time with
`reusable-ci doctor`.

### How it works

- Push code → PR workflow runs checks.
- Sign and push a `release-request/vX.Y.Z` tag → release workflow validates, builds, publishes.
- A failed step prints an actionable message; the orchestrator's
  step-summary block names which leaf failed.

### Two ways to call it

#### A) Use the orchestrator as-is
```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v3.0.0
with:
  reusable-ci-binary-ref: v3.0.0
  artifacts-config: .reusable-ci/artifacts.yml
  release-publisher: github-cli
```

#### B) Compose leaves yourself
```yaml
jobs:
  build-npm:
    uses: diggsweden/reusable-ci/.github/workflows/build-npm.yml@v3.0.0
    with:
      working-directory: .

  publish-github:
    needs: build-npm
    uses: diggsweden/reusable-ci/.github/workflows/publish-maven-github.yml@v3.0.0
    permissions:
      contents: read
      packages: write
    with:
      package-type: npm
      artifact-source: npm-build-artifacts

  build-container:
    needs: build-npm
    uses: diggsweden/reusable-ci/.github/workflows/publish-container.yml@v3.0.0
    permissions:
      contents: read
      packages: write
      id-token: write
      attestations: write
      actions: read
    secrets: inherit
    with:
      reusable-ci-binary-ref: v3.0.0
      container-file: Containerfile
      artifact-types: npm
```

## Quick Start

### For New Projects

1. **Create artifacts configuration** *(optional, skip for a single-manifest repo; the plan is auto-derived from the root `go.mod`/`Cargo.toml`/`pom.xml`/`package.json`)* - Define what to build:
   ```yaml
   # .reusable-ci/artifacts.yml
   artifacts:
     - name: my-app
       project-type: maven  # or npm, gradle, gradle-android, xcode-ios, cargo, go
       working-directory: .
       config:
         java-version: 25  # or node-version for npm, xcode-version for xcode-ios
         # For project-type: go OR cargo (default: artifact-first):
         # build-mode: artifact-first   # standalone CLI binaries (default)
         # build-mode: container-first  # Containerfile owns the compile
   ```

2. **Create pull request workflow** - Run checks on PRs:
   ```yaml
   # .github/workflows/pullrequest-workflow.yml
   name: Pull Request Checks
   on:
     pull_request:
       branches: [main, master, develop]
   permissions:
     contents: read
   jobs:
     pr-checks:
       uses: diggsweden/reusable-ci/.github/workflows/pullrequest-orchestrator.yml@v3.0.0
       permissions:
         contents: read
         packages: read
       secrets: inherit  # Pass CODE_SCANNING_TOKEN if you want Security / Code Scanning upload
       with:
         reusable-ci-binary-ref: v3.0.0
         project-type: maven  # or npm, gradle, gradle-android, xcode-ios, cargo, go
         # General lint engine (mutually exclusive): nanolinter (default) | megalinter | none
         lint-engine: nanolinter
         # Optional, orthogonal Swift/iOS checks:
         # linters.swiftlint: false        # Swift linting for iOS/macOS (standalone macOS job)
   ```

3. **Create release workflow** - Trigger builds on release-request tags:
   ```yaml
   # .github/workflows/release-workflow.yml
   name: Release
   on:
     push:
       tags: ["release-request/v*"]
   permissions:
     contents: read
   jobs:
     release:
       uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v3.0.0
       permissions:
         contents: write
         packages: write
         id-token: write
         actions: read
         attestations: write
       secrets: inherit
       with:
         reusable-ci-binary-ref: v3.0.0
         artifacts-config: .reusable-ci/artifacts.yml
         release-publisher: github-cli
   ```

4. **(Optional) Create snapshot release workflow** - Fast branch builds:
   ```yaml
   # .github/workflows/release-snapshot-workflow.yml
   name: Snapshot Release
   on:
     push:
       branches: ['dev/**', 'feat/**']
   permissions:
     contents: read
   jobs:
     snapshot-release:
       uses: diggsweden/reusable-ci/.github/workflows/release-snapshot-orchestrator.yml@v3.0.0
       permissions:
         contents: read
         packages: write
       secrets: inherit
       with:
         reusable-ci-binary-ref: v3.0.0
         artifacts-config: .reusable-ci/artifacts.yml
   ```

5. **Configure release secrets**:
   Production releases validate release credentials before building. Configure
   these repository or organization secrets before pushing the first release tag:
   `RELEASE_TOKEN`, `RELEASE_GPG_PRIVATE_KEY`, `RELEASE_GPG_PASSPHRASE`, and
   `RELEASE_GPG_PUBLIC_KEY`. Add target-specific secrets such as Maven Central,
   App Store Connect, or Google Play credentials only when those publishers are
   enabled.

6. **Create your first release**:
   ```bash
   git tag -s v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

---

## Conceptual View

### Pull Request flow

`pullrequest-orchestrator.yml` composes one Setup job that plans the
PR-stage matrix, then a Quality stage running each enabled check on
its own runner. Project tests are caller-owned and run from the
adopter's own workflow (the orchestrator doesn't invoke them).

```text
┌─────────────────────────────────────────────────────────────────────┐
│                       Pull Request push                             │
└────────────────────────────────┬────────────────────────────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │      Setup (plan)       │
                    │  read PR inputs, emit   │
                    │   quality-stage matrix  │
                    └────────────┬────────────┘
                                 │
                 ┌───────────────┴───────────────┐
                 │                                │
        ┌────────▼────────┐            ┌──────────▼─────────┐
        │   nanolinter    │            │     swift-lint     │
        │ lint+license+   │            │ (macOS; iOS only,  │
        │ publiccode+SAST │            │ nanolinter has no  │
        │ +deps -> SARIF  │            │  macOS binary)     │
        └────────┬────────┘            └──────────┬─────────┘
                 └───────────────┬────────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │       PR Summary        │
                    │  aggregated step-summary│
                    │  + SARIF if configured  │
                    └─────────────────────────┘
```

### Release flow

`release-orchestrator.yml` parses `artifacts.yml`, validates every
release prerequisite up front, then dispatches a build stage (one
job per declared artifact) and a publish stage (one job per declared
target), and finally creates the GitHub release. Container builds
happen in the publish stage; SBOMs are attested per-artifact and
per-image.

```text
┌─────────────────────────────────────────────────────────────────────┐
│              Signed tag push (release-request/vX.Y.Z)              │
└────────────────────────────────┬────────────────────────────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │   Setup + Validate      │
                    │  parse artifacts.yml,   │
                    │  validate prerequisites │
                    │  (GPG, tokens, lockfiles│
                    │   tag format, signer)   │
                    └────────────┬────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │      Prepare stage      │
                    │ signed bump + final tag │
                    │       vX.Y.Z            │
                    └────────────┬────────────┘
                                 │
         ┌───────────────────────┴────────────────────────┐
         │                                                │
┌────────▼────────┐                              ┌────────▼────────┐
│   Build stage   │                              │  (parallel per  │
│  one job per    │                              │   declared      │
│  artifact:      │                              │   artifact)     │
│  build-maven,   │                              │                 │
│  build-npm,     │                              │                 │
│  build-gradle-*,│                              │                 │
│  build-go,      │                              │                 │
│  build-cargo,   │                              │                 │
│  build-xcode-ios│                              │                 │
└────────┬────────┘                              └────────┬────────┘
         │                                                │
         └────────────────────────┬───────────────────────┘
                                  │
                     ┌────────────▼────────────┐
                     │     Publish stage       │
                     │  one job per target:    │
                     │  Maven Central, GitHub  │
                     │  Packages, App Store    │
                     │  Connect, Google Play,  │
                     │  publish-container      │
                     │  (multi-arch, SLSA,     │
                     │  SBOM, Trivy gate)      │
                     └────────────┬────────────┘
                                  │
                     ┌────────────▼────────────┐
                     │   Create GitHub Release │
                     │  changelog, checksums,  │
                     │  signatures, attached   │
                     │  SBOM ZIP, attestations │
                     └─────────────────────────┘
```

### Core concepts

**Build stage**: one job per declared artifact, dispatched by
`release-build-stage.yml`. Builders today: `build-maven`, `build-npm`,
`build-gradle-app`, `build-gradle-android`, `build-go`,
`build-cargo`, `build-xcode-ios`. Each produces a typed artifact
uploaded for the publish stage to consume.

**Publish stage**: one job per declared target, dispatched by
`release-publish-stage.yml`: `publish-maven-github`,
`publish-maven-central`, `publish-google-play`,
`publish-apple-appstore`, plus `publish-container` for every entry
in `containers[]`. SBOM-only jobs (`sbom-cargo`, `sbom-go`) run
alongside for container-first ecosystems.

**Containers**: declared separately under `containers[]` in
`artifacts.yml`, referencing one or more artifacts via
`from: [artifact-name]`. Built multi-arch (linux/amd64 +
linux/arm64), signed (cosign), scanned (Trivy gate), and attached to
SLSA provenance + analyzed-container SBOM attestations.

**Snapshot releases**: branch pushes go through
`release-snapshot-orchestrator.yml`. They publish a content-addressed npm
snapshot (`0.5.9-snapshot-<branch>-<sha>`, dist-tag `snapshot`); no
containers, no signing, no SLSA, no GitHub Release. The snapshot flow is
branch/dispatch-triggered, never a tag.

---

## License

The `reusable-ci` program — the Go source — is dual-licensed
[EUPL-1.2](LICENSES/EUPL-1.2.txt) **OR**
[GPL-3.0-or-later](LICENSES/GPL-3.0-or-later.txt), at your option.

Everything you copy into your own repository is
[CC0-1.0](LICENSES/CC0-1.0.txt): the reusable workflows, the GitLab Catalog
components in `templates/`, the shell scripts, and all configuration,
documentation and examples. Lift them freely.

See [LICENSE](LICENSE) for the full statement. Every file carries an SPDX
identifier, which is authoritative for that file.

---
