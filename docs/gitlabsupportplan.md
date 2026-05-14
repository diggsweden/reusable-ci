<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# GitLab CI Support — Architecture & Future Plan

Architecture, design rules, and the phases still ahead. For what's already
shipped, see [`gitlab-completed.md`](gitlab-completed.md). For the live
TODO list, see [`gitlab.prep.md`](gitlab.prep.md).

## Architecture

```text
                    ┌─────────────────────┐
                    │   artifacts.yml     │  Platform-agnostic product intent
                    └─────────┬───────────┘
                              │
              ┌───────────────┴───────────────┐
              │                               │
    ┌─────────▼──────────┐         ┌──────────▼─────────┐
    │  .github/workflows │         │  .gitlab/ci/       │
    │  Idiomatic GHA     │         │  Idiomatic GitLab  │
    │  (thin adapters)   │         │  (thin adapters)   │
    └─────────┬──────────┘         └──────────┬─────────┘
              │                               │
              └───────────────┬───────────────┘
                              │
                    ┌─────────▼───────────┐
                    │     scripts/        │  Shared business logic
                    │  (stays where it is)│
                    └─────────────────────┘
```

### Design Rules

1. **Scripts stay at `scripts/`** — no move to `.ci-shared/` (breaking change with zero benefit).
2. **`artifacts.yml` stays pure** — no `ci:` or `platform:` sections (product intent, not CI wiring).
3. **YAML is a thin adapter** — triggers, job graph, runners, secrets, artifact transport.
4. **Scripts own all decisions** — config parsing, policy, validation, build commands, results.
5. **Provider-specific logic uses explicit dispatch.** Substantial branches go in `scripts/*/providers/{github,gitlab}.sh` (release creation, token validation). Thin context resolution can branch inline via `case "$CI_PLATFORM"` (event-context reads in `scripts/container/compute-image-metadata.sh`). Either way, no `if [[ "$GITHUB_*" ]]` checks scattered through call sites.
6. **Inter-stage communication uses file-based manifests** — not `GITHUB_OUTPUT` directly.
7. **Capabilities, not parity** — document what each platform provides; don't fake what it doesn't.

### `artifacts.yml` — already platform-agnostic enough

Two field values carry GitHub assumptions:

- `publish-to: github-packages` — GitHub-specific registry name
- `enable-slsa: true` — only works on GitHub (SLSA L3 via `slsa-github-generator`)

These don't break the format — a GitLab adapter can map `github-packages` →
`gitlab-registry` or skip it, and ignore `enable-slsa`. No schema change
needed.

---

## Phases ahead

Phase 1 (shared-core prep) is complete. See
[`gitlab-completed.md`](gitlab-completed.md) for what landed.

### Phase 2: Define the adapter contract (design document)

Extend `docs/workflow-design-policy.md` to codify:

1. **YAML adapter owns**: triggers, job graph, runners, secrets, artifact transport, matrix syntax, platform-native integrations.
2. **Scripts own**: all business logic, all validation, all build commands, all output formatting, provider dispatch.
3. **Stage manifest JSON schema** (the `.ci-results/<stage>-result.json` shape).
4. **Provider dispatch convention** (`scripts/*/providers/{github,gitlab}.sh`).
5. **Capability matrix**:

| Capability | GitHub | GitLab | Shared logic |
|---|---|---|---|
| Build (Maven/NPM/Gradle) | Yes | Yes | `scripts/build/` |
| Container build | Yes | Yes | Docker/Buildx (portable) |
| SLSA L3 provenance | Yes (native) | No | N/A — GitHub-only |
| SBOM generation | Yes | Yes | `scripts/sbom/` (syft) |
| SBOM attestation | Yes (native) | Artifact upload | Provider dispatch |
| Release creation | Yes (`gh`) | Yes (`glab`/API) | `scripts/release/providers/` |
| Package registry | GitHub Packages | GitLab Registry | Provider dispatch |
| Container registry | GHCR | GitLab CR | Registry URL config |
| SAST upload | SARIF → Code Scanning (imperative `upload-sarif.sh`) | `reports:sast:` ingests `gitlab-sast.json` (declarative) | Producer emits both files; consumption differs in *kind*, not just detail |
| Dependency scan upload | SARIF → Code Scanning | `reports:dependency_scanning:` ingests `gl-dependency-scanning-report.json` | `scripts/security/trivy-to-gitlab-dep.sh` |
| Container scan upload | SARIF → Code Scanning | `reports:container_scanning:` ingests `gl-container-scanning-report.json` | `scripts/security/trivy-to-gitlab-container.sh` |
| Container signing | Cosign + GitHub OIDC | Cosign + GitLab OIDC | Same tool, different OIDC config |
| Step summaries | `GITHUB_STEP_SUMMARY` | MR comment or artifact | `ci_summary()` dispatch |

### Phase 3: Build the GitLab CI adapter

```text
.gitlab/
  ci/
    pullrequest.yml              # MR pipeline orchestrator
    release.yml                  # Tag pipeline orchestrator
    release-dev.yml              # Branch pipeline orchestrator
    templates/
      .build-maven.yml           # Hidden job template
      .build-npm.yml
      .build-gradle.yml
      .publish-container.yml
      .lint-devbase.yml
      .validate-prerequisites.yml
```

**Use idiomatic GitLab, not translated GitHub**:

- `include:` + `extends:` for composition (not `workflow_call`)
- `rules:` for conditional execution
- `artifacts: reports: dotenv:` for inter-job key-value outputs
- `artifacts: paths:` for inter-job file passing (stage manifests)
- `parallel: matrix:` for fan-out builds
- `trigger:` with child pipelines for stage isolation (closest to stage workflows)
- `release:` keyword for GitLab releases
- `artifacts: reports: sast:` for security scan results

**Caller contract**:

```yaml
# .gitlab-ci.yml
include:
  - project: 'diggsweden/reusable-ci'
    ref: v3.0.0
    file: '.gitlab/ci/release.yml'

variables:
  ARTIFACTS_CONFIG: .gitlab/artifacts.yml
  CHANGELOG_CREATOR: git-cliff
```

**Add GitLab examples** alongside existing GitHub examples:

```text
examples/maven-app/
  .github/workflows/release-workflow.yml     # existing
  .gitlab-ci.yml                              # new
```

### Phase 4: Platform-specific depth

Features that don't have cross-platform equivalents — handle with graceful
degradation:

| Feature | GitHub | GitLab | Strategy |
|---|---|---|---|
| SLSA L3 provenance | `slsa-github-generator` | Not available | Skip on GitLab, document as GitHub advantage |
| SBOM attestation | `actions/attest-sbom` | Upload as artifact | Generate SBOM via shared scripts, attach differently |
| Step summaries | `GITHUB_STEP_SUMMARY` | N/A | Write to artifact markdown file; optionally post as MR comment |
| Container signing | Cosign + GitHub OIDC | Cosign + GitLab OIDC | Same tool, adapter for OIDC config |
| Dependency review | `dependency-review-action` | GitLab Dependency Scanning | Different tools, same intent — no shared logic |

---

## What NOT To Do

1. **Don't move `scripts/` to `.ci-shared/`.** Breaks every script reference in workflows, script-adjacent Go tests, and the justfile. Zero architectural benefit.
2. **Don't add `ci:` sections to `artifacts.yml`.** Per-artifact `ci.github` / `ci.gitlab` blocks pollute the product-intent contract with CI wiring. Platform-specific config belongs in platform YAML.
3. **Don't aim for 100% feature parity.** SLSA L3 doesn't exist on GitLab. Container attestation differs fundamentally. Forcing parity creates fake abstractions.
4. **Don't build a meta-DSL or YAML generator.** Breaks IDE tooling, creates a third thing to maintain, produces unidiomatic pipelines on both platforms.
5. **Don't put time estimates on phases.** Scope is clear; timeline depends on team capacity.

---

## End-state directory structure

```text
reusable-ci/
├── .github/workflows/           # GitHub Actions adapter (existing, stable public API)
├── .gitlab/ci/                  # GitLab CI adapter (Phase 3)
│   ├── pullrequest.yml
│   ├── release.yml
│   ├── release-dev.yml
│   └── templates/
├── containers/runtime/          # Shared runtime image (built and published)
├── scripts/                     # Shared logic
│   ├── ci/                      # env, output, manifest, install-* helpers
│   ├── build/                   # Build steps (portable)
│   ├── config/                  # artifacts.yml parsing
│   ├── plan/                    # Policy decisions
│   ├── publish/                 # Per-ecosystem publish helpers
│   ├── release/
│   │   ├── create-release.sh    # Generic entrypoint (provider dispatch)
│   │   ├── import-gpg-key.sh    # Replaces crazy-max/ghaction-import-gpg
│   │   ├── cleanup-gpg-key.sh   # Companion teardown for the GPG keyring
│   │   └── providers/{github,gitlab}.sh
│   ├── validate/
│   │   ├── token.sh
│   │   ├── bot-permissions.sh
│   │   └── providers/{github,gitlab}.sh
│   ├── security/                # OpenGrep, Trivy, GitLab transforms
│   ├── sbom/                    # Syft (CycloneDX + SPDX)
│   ├── summary/                 # Stage-result aggregation
│   ├── container/               # Registry validation + image-metadata helper
│   │                            # (compute-image-metadata.sh — the strategic
│   │                            #  cross-provider boundary)
│   ├── registry/                # Portable
│   └── version/                 # Portable (commit-and-push, move-tag, …)
├── examples/
│   └── maven-app/
│       ├── .github/workflows/   # GitHub examples
│       └── .gitlab-ci.yml       # GitLab example (Phase 3)
├── scripts/                     # shared shell runtime surface + Go tests beside it
├── docs/
│   ├── gitlab-completed.md      # What's already delivered
│   ├── gitlab.prep.md           # What's left
│   ├── gitlabsupportplan.md     # This file (architecture + future plan)
│   ├── workflow-design-policy.md
│   └── ...
└── artifacts.yml                # Platform-agnostic config (unchanged)
```

---

## Phase ordering

```text
Phase 2 (adapter contract doc)    ── Depends on Phase 1 (done)
         │
         ▼
Phase 3 (GitLab CI adapter)       ── Depends on Phase 2
         │
         ▼
Phase 4 (platform depth)          ── Depends on Phase 3
```
