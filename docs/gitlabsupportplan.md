<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Final Proposal: GitLab CI Support for Reusable-CI

Consolidated from proposals 1-3, with all claims verified against the codebase.

## Completed (in codebase, verified)

- **1a** — `ci_log_error/warning` in `output.sh` + all scripts migrated (zero bare `::error::` outside `output.sh`)
- **1b (GitHub half)** — `CI_PLATFORM="github"` in `env.sh`, exported
- **1c** — `ci_release_url`/`ci_packages_url`/`ci_docs_url` in `output.sh`; hardcoded URLs fixed in 3 summary/validate scripts; `> [!WARNING]` → `> **Warning:**`
- **1f** — 6 inline shell blocks extracted into `scripts/build/`, `scripts/publish/`, `scripts/release/`, `scripts/validate/`; 5 workflow YAMLs updated
- **1d** — GitHub-specific variables renamed: `SHOULD_CREATE_GITHUB_RELEASE` → `SHOULD_CREATE_RELEASE`, `USE_GITHUB_TOKEN` → `USE_CI_TOKEN` (incl. workflow_call inputs `use-github-token` → `use-ci-token`), `GITHUB_REGISTRY` → `CI_REGISTRY`, `PUBLISH_MAVEN_GITHUB_RESULT` → `PUBLISH_MAVEN_REGISTRY_RESULT`; JSON keys + output names updated across scripts, workflows, tests, and docs

**Internal simplifications** (no external impact):

- Removed unused `ci_log_notice()` from `output.sh` (zero callers)
- Merged `resolve-file-pattern.sh` into `get-file-pattern.sh` (dual-mode: positional args + env vars)
- Merged `validate-auth-configuration.sh` into `validate-auth.sh` (dual-mode: positional args + env vars)
- Extracted `scripts/ci/stage-result.sh` — shared aggregation helpers used by all 6 stage-result scripts
- Merged `create-and-sign-sbom-zip.sh` into `create-sbom-zip.sh` (signing via optional `SIGN_ARTIFACTS`/`GPG_KEY_ID` env vars)

---

## Remaining GitHub Coupling in Scripts

- `scripts/container/validate-namespace.sh:23-24` — hardcoded `ghcr.io` check (security validation, registry-specific by design)

**GitHub CLI (`gh`) usage** (Phase 1e — provider dispatch):

- `scripts/release/create-github-release.sh:22,31,141-142` — `gh release view/delete/create`
- `scripts/validate/validate-bot-permissions.sh:14,19,25` — `gh api`
- `scripts/validate/validate-github-token.sh:20,37,48` — `github.com/settings`, `api.github.com`

### `artifacts.yml` — Almost Platform-Agnostic

Two field values carry platform assumptions:
- `publish-to: github-packages` — GitHub-specific registry name
- `enable-slsa: true` — only works on GitHub (SLSA L3 via `slsa-github-generator`)

These don't break the format — a GitLab adapter can map `github-packages` → `gitlab-registry` or skip it, and ignore `enable-slsa`. No schema change needed.

---

## Architecture

All three proposals agree. Verified as sound:

```
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

1. **Scripts stay at `scripts/`** — no move to `.ci-shared/` (breaking change with zero benefit)
2. **`artifacts.yml` stays pure** — no `ci:` or `platform:` sections (product intent, not CI wiring)
3. **YAML is a thin adapter** — triggers, job graph, runners, secrets, artifact transport
4. **Scripts own all decisions** — config parsing, policy, validation, build commands, results
5. **Provider-specific logic uses explicit dispatch** — `scripts/*/providers/{github,gitlab}.sh`
6. **Inter-stage communication uses file-based manifests** — not `GITHUB_OUTPUT` directly
7. **Capabilities, not parity** — document what each platform provides; don't fake what it doesn't

---

## Phase Plan

### Phase 1: Remaining shared-core prep

#### 1b. Complete `scripts/ci/env.sh` (GitLab branch)

GitHub branch is done. Fill in the GitLab stub when starting Phase 3:

```bash
elif [[ "${GITLAB_CI:-}" == "true" ]]; then
  CI_PLATFORM="gitlab"
  CI_COMMIT="${CI_COMMIT_SHA:-}"
  CI_REPO="${CI_PROJECT_PATH:-}"
  CI_RUN_ID="${CI_PIPELINE_ID:-}"
  CI_ACTOR="${GITLAB_USER_LOGIN:-}"
  CI_BRANCH="${CI_MERGE_REQUEST_SOURCE_BRANCH_NAME:-${CI_COMMIT_BRANCH:-}}"
  CI_REF_NAME="${CI_COMMIT_REF_NAME:-}"
  CI_REF="${CI_COMMIT_REF_NAME:-}"
  CI_SERVER_URL="${CI_SERVER_URL:-}"
  CI_RUN_URL="${CI_PIPELINE_URL:-}"
else
  CI_PLATFORM="local"
fi
```

#### 1e. Introduce provider dispatch for release creation

Restructure:
```
scripts/release/
  create-github-release.sh    →  providers/github.sh  (move, don't delete)
  create-release.sh           →  new generic entrypoint
  providers/
    github.sh                 ←  current create-github-release.sh
    gitlab.sh                 ←  future (placeholder)
```

`create-release.sh`:
```bash
source "$(dirname "$0")/../ci/env.sh"
case "${CI_PLATFORM:-github}" in
  github) source "$(dirname "$0")/providers/github.sh" ;;
  gitlab) source "$(dirname "$0")/providers/gitlab.sh" ;;
  *) printf "Unsupported platform: %s\n" "$CI_PLATFORM" >&2; exit 1 ;;
esac
create_release "$@"
```

Apply same pattern to:
- `scripts/validate/validate-github-token.sh` → `providers/github.sh` (100% GitHub-specific)
- `scripts/validate/validate-bot-permissions.sh` → `providers/github.sh` (uses `gh api`)

#### 1g. Introduce file-based stage manifest contract

Define a convention for inter-stage results:

```
.ci-results/
  build-result.json
  publish-result.json
  prepare-result.json
```

Scripts write to these files via a new helper:

```bash
# scripts/ci/manifest.sh
ci_write_stage_result() {
  local stage="$1" result="$2"
  local dir="${CI_RESULTS_DIR:-.ci-results}"
  mkdir -p "$dir"
  printf '{"stage":"%s","result":"%s"}\n' "$stage" "$result" > "$dir/${stage}-result.json"
}
```

Platform YAML wires these files through its own transport:
- GitHub: `actions/upload-artifact` / `actions/download-artifact`
- GitLab: `artifacts: paths:` / `needs:`

This decouples scripts from `GITHUB_OUTPUT` for structured data.

### Phase 2: Define the adapter contract (design document)

Extend `docs/workflow-design-policy.md` to codify:

1. **YAML adapter owns**: triggers, job graph, runners, secrets, artifact transport, matrix syntax, platform-native integrations
2. **Scripts own**: all business logic, all validation, all build commands, all output formatting, provider dispatch
3. **Stage manifest JSON schema** (from Phase 1g)
4. **Provider dispatch convention** (`scripts/*/providers/{github,gitlab}.sh`)
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
| Security scan upload | SARIF → Security tab | `reports: sast:` | Same SARIF generation, different upload |
| Container signing | Cosign + GitHub OIDC | Cosign + GitLab OIDC | Same tool, different OIDC config |
| Step summaries | `GITHUB_STEP_SUMMARY` | MR comment or artifact | `ci_summary()` dispatch |
| Dependency scanning | `dependency-review-action` | GitLab native | Different tools, same intent |

### Phase 3: Build the GitLab CI adapter

```
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
      .lint-devbase-check.yml
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
```
examples/maven-app/
  .github/workflows/release-workflow.yml     # existing
  .gitlab-ci.yml                              # new
```

### Phase 4: Platform-specific depth

Features that don't have cross-platform equivalents — handle with graceful degradation:

| Feature | GitHub | GitLab | Strategy |
|---|---|---|---|
| SLSA L3 provenance | `slsa-github-generator` | Not available | Skip on GitLab, document as GitHub advantage |
| SBOM attestation | `actions/attest-sbom` | Upload as artifact | Generate SBOM via shared scripts, attach differently |
| Step summaries | `GITHUB_STEP_SUMMARY` | N/A | Write to artifact markdown file; optionally post as MR comment |
| Container signing | Cosign + GitHub OIDC | Cosign + GitLab OIDC | Same tool, adapter for OIDC config |
| Dependency review | `dependency-review-action` | GitLab Dependency Scanning | Different tools, same intent — no shared logic |

---

## What NOT To Do (verified against codebase)

1. **Don't move `scripts/` to `.ci-shared/`** — Proposal 2 suggested this. It breaks all 35+ script references in existing workflows, all BATS tests, and the justfile. Zero architectural benefit.

2. **Don't add `ci:` sections to `artifacts.yml`** — Proposal 2 suggested per-artifact `ci.github` and `ci.gitlab` blocks. This pollutes the product-intent contract with CI wiring. Platform-specific config belongs in platform YAML.

3. **Don't aim for 100% feature parity** — Proposal 2 set this as a success metric. SLSA L3 literally does not exist on GitLab. Container attestation workflows are fundamentally different. Forcing parity creates fake abstractions.

4. **Don't build a meta-DSL or YAML generator** — All proposals agree. Breaks IDE tooling, creates a third thing to maintain, produces unidiomatic pipelines on both platforms.

5. **Don't start by writing GitLab YAML** — The shared core still has gaps: provider dispatch (1e) and the env.sh GitLab branch (1b). Fix these first.

6. **Don't put time estimates on phases** — Scope is clear; timeline depends on team capacity.

---

## End-State Directory Structure

```
reusable-ci/
├── .github/workflows/           # GitHub Actions adapter (existing, stable public API)
│   ├── pullrequest-orchestrator.yml
│   ├── release-orchestrator.yml
│   ├── release-dev-orchestrator.yml
│   ├── release-*-stage.yml      # Stage controllers
│   ├── build-*.yml              # Leaf build workflows
│   ├── publish-*.yml            # Leaf publish workflows
│   ├── lint-*.yml               # Quality workflows
│   └── security-*.yml           # Security workflows
├── .gitlab/ci/                  # GitLab CI adapter (new)
│   ├── pullrequest.yml
│   ├── release.yml
│   ├── release-dev.yml
│   └── templates/
│       ├── .build-maven.yml
│       ├── .build-npm.yml
│       ├── .publish-container.yml
│       └── ...
├── scripts/                     # Shared logic
│   ├── ci/
│   │   ├── env.sh              # Platform detection + CI_PLATFORM
│   │   ├── output.sh           # ci_log_*, ci_output, ci_summary, URL helpers
│   │   ├── stage-result.sh     # Stage result aggregation helpers
│   │   └── manifest.sh         # Stage result file I/O (Phase 1g)
│   ├── build/
│   │   ├── maven-extract-metadata.sh
│   │   └── maven-build-library.sh
│   ├── config/                 # artifacts.yml parsing (portable)
│   ├── plan/                   # Policy decisions (portable)
│   ├── publish/
│   │   ├── maven-validate-artifacts.sh
│   │   └── npm-verify-tarball.sh
│   ├── release/
│   │   ├── create-release.sh            # Generic dispatch (Phase 1e)
│   │   ├── providers/
│   │   │   ├── github.sh               # From current create-github-release.sh (Phase 1e)
│   │   │   └── gitlab.sh               # Phase 3
│   │   ├── verify-changelog.sh
│   │   ├── generate-checksums.sh
│   │   └── sign-release-artifacts.sh
│   ├── validate/
│   │   ├── validate-tag-format.sh
│   │   ├── validate-tag-signature.sh
│   │   ├── debug-workspace.sh
│   │   ├── providers/
│   │   │   ├── github.sh               # From validate-github-token.sh + validate-bot-permissions.sh (Phase 1e)
│   │   │   └── gitlab.sh               # Phase 3
│   │   └── ...
│   ├── summary/                # Platform-aware URL helpers
│   ├── sbom/                   # Portable (syft-based)
│   ├── container/              # Registry validation still has ghcr.io (security check, registry-specific)
│   ├── registry/               # Portable (variables renamed)
│   └── version/                # Portable
├── examples/
│   ├── maven-app/
│   │   ├── .github/workflows/  # GitHub examples (existing)
│   │   └── .gitlab-ci.yml      # GitLab example (new)
│   └── ...
├── tests/                      # BATS tests (expanded for new scripts)
├── docs/
│   ├── gitlab-ci.md            # GitLab setup + capability matrix (new)
│   ├── workflow-design-policy.md  # Extended with adapter contract (Phase 2)
│   └── ...
└── artifacts.yml               # Platform-agnostic config (unchanged)
```

---

## Ordering and Dependencies

```
Phase 1e (provider dispatch)      ── Depends on CI_PLATFORM (done)
Phase 1g (stage manifests)        ── Independent, can overlap with 1e
         │
         ▼
Phase 2 (adapter contract doc)    ── Depends on Phase 1 being complete
         │
         ▼
Phase 3 (GitLab CI adapter)       ── Depends on Phase 2; includes 1b GitLab branch
         │
         ▼
Phase 4 (platform depth)          ── Depends on Phase 3
```

The remaining Phase 1 items (1e, 1g) require GitLab design decisions — they are best done at the start of Phase 3 when the target platform shapes the design.
