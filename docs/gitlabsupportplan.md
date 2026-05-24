<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# GitLab CI Support — Architecture & Future Plan

Architecture, design rules, and the phases still ahead. For the
remaining work list, see [`gitlab.prep.md`](gitlab.prep.md). What's
already in place is readable from the code: the GitLab provider
adapter (`internal/adapters/gitlab/`), the dual-emit security tools
(SARIF + `gl-*-scanning-report.json`), the cross-platform runtime
images, and the `.ci-results/` manifest sink are present and used by
the existing GitHub workflows; the Catalog adapter YAML is what's not
yet written.

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
    │  GitHub adapter    │         │  GitLab adapter    │
    └─────────┬──────────┘         └──────────┬─────────┘
              │                               │
              └───────────────┬───────────────┘
                              │
                    ┌─────────▼───────────┐
                    │ reusable-ci binary  │  Shared logic + provider adapters
                    └─────────────────────┘
```

## Design Rules

1. **The Go binary owns decisions.** Config parsing, policy, validation,
   build wrappers, security transforms, SBOM generation, summaries, and release
   helper logic live behind `reusable-ci` commands.
2. **YAML is a platform adapter.** GitHub and GitLab YAML own triggers, job
   graphs, runners, secrets, cache syntax, artifact transport, matrix syntax,
   and platform-native report declarations.
3. **Provider-specific behavior goes behind Go adapters.** GitHub/GitLab/local
   differences belong in `internal/adapters/{github,gitlab,local}` behind
   interfaces in `internal/domain/provider`, not scattered through workflows.
4. **`artifacts.yml` stays pure.** It describes product/release intent, not CI
   platform wiring. Do not add `ci.github` / `ci.gitlab` blocks.
5. **Inter-stage communication uses files and compact JSON.** Scalar CI outputs
   are for same-platform plumbing; stage manifests under `.ci-results/` are the
   durable cross-provider shape.
6. **Capabilities, not fake parity.** Document what each platform provides;
   skip or degrade clearly when GitLab lacks a GitHub-only feature.
7. **No compatibility aliases unless there is persisted data.** Workflow input
   names should reflect the long-term contract, even when that means a major
   version break.

## `artifacts.yml`

Two field values carry GitHub assumptions today:

- `publish-to: github-packages` — GitHub-specific package destination.
- `enable-slsa: true` — only works on GitHub SLSA L3 infrastructure.

These do not require schema changes. The GitLab adapter can map, skip, or warn
based on platform capability while keeping the artifact contract stable.

## Capability Matrix

| Capability | GitHub | GitLab | Shared implementation |
|---|---|---|---|
| Build wrappers | Reusable workflows | CI components/jobs | `reusable-ci build ...` |
| Release planning | Reusable workflows | CI components/jobs | `reusable-ci config ...`, `reusable-ci plan ...` |
| Container build | Docker/Buildx actions | Docker/Buildx or GitLab runner Docker | Workflow/YAML adapter, shared `reusable-ci container ...` helpers |
| SLSA L3 provenance | Native GitHub generator | No direct equivalent | GitHub-only; skip on GitLab |
| SBOM generation | Artifacts + attestations | Artifacts + `reports:cyclonedx` | `reusable-ci sbom ...` |
| Release creation | `gh` / GitHub API | GitLab API / `release:` keyword | Provider adapter plus GitLab YAML upload strategy |
| Package registry | GitHub Packages | GitLab Package Registry | Platform YAML + provider-aware validation |
| Container registry | GHCR | GitLab Container Registry | Registry URL config + namespace validation policy |
| SAST upload | SARIF to Code Scanning | `artifacts:reports:sast` | Producer emits SARIF + GitLab SAST JSON |
| Dependency scan upload | SARIF to Code Scanning | `artifacts:reports:dependency_scanning` | Producer emits SARIF + GitLab dependency JSON |
| Container scan upload | SARIF to Code Scanning | `artifacts:reports:container_scanning` | Producer emits SARIF + GitLab container JSON |
| Step summaries | `GITHUB_STEP_SUMMARY` | Markdown artifact or MR comment | Summary use cases render provider-neutral Markdown |
| Build-time secret mounts | `containers[].build-secrets` + `REUSABLE_CI_BUILD_SECRETS_JSON` envelope (GHA secret) | same envelope shape supplied as a GitLab CI variable | `reusable-ci container materialize-build-secrets` consumes the envelope identically on both platforms |
| Privileged-trigger gate | `reusable-ci validate event-context` reads `GITHUB_EVENT_NAME` | needs `CI_PIPELINE_SOURCE` reader and a port allowlist (`push`, `web`, `schedule`, `pipeline`, `trigger`, `api`); refuse `merge_request_event` / `external_pull_request_event` | shared allowlist policy in `internal/domain/validate.RequireAllowedEvent`; provider-specific reader behind the `provider` interface |

## Phases Ahead

### Phase 1: GitLab Component Skeleton

Add the GitLab-facing entrypoints without trying to port every workflow at
once:

```text
.gitlab/
  ci/
    security-opengrep.yml
    templates/
      reusable-ci.yml
```

The first component should run inside a runtime image and invoke the existing
binary command, for example `reusable-ci security scan opengrep`, then publish
GitLab-native reports via `artifacts:reports:*`.

### Phase 2: Shared Component Contracts

Define reusable include/extends shapes for common concerns:

- runtime image selection
- `reusable-ci` invocation and env mapping
- artifact paths and report declarations
- `.ci-results/` manifest upload
- failure semantics for quality/security jobs

### Phase 3: Release/Build Stage Adapter

Port the GitHub stage structure into idiomatic GitLab jobs/components:

- release setup: `reusable-ci config parse-artifacts`, `reusable-ci plan ...`
- build fanout: Maven, npm, Gradle, Android, Xcode where applicable
- publish fanout: Maven Central, npm, container, Google Play/App Store where applicable
- release summary and manifest upload

Use GitLab-native `rules:`, `parallel:matrix`, `needs:`, `artifacts:paths`,
`artifacts:reports:dotenv`, child pipelines where they improve readability, and
the `release:` keyword when it fits.

### Phase 4: Provider Depth

Finish provider behavior only when a GitLab component actually needs it:

- correct GitLab token header selection (`PRIVATE-TOKEN` vs `JOB-TOKEN`)
- release asset upload/linking via GitLab Package Registry or another real URL
- MR comments or artifact-based step summaries
- package/container registry validation semantics

## Deliberate GitHub coupling

Two reusable-ci surfaces stay GitHub-specific by design, not by
oversight:

- `reusable-ci container validate-namespace` — hardcoded `ghcr.io`
  namespace policy. Registry-specific security validation is the
  right shape; a GitLab Catalog component validates against the
  GitLab Container Registry's own rules separately.
- `reusable-ci security report upload-sarif` — pushes SARIF to GitHub
  Code Scanning. The GitLab equivalent isn't an imperative upload:
  GitLab ingests `artifacts:reports:sast` declaratively, which the
  producer (`reusable-ci security scan opengrep`) already emits.

Both surfaces have GitLab counterparts that work through different
mechanisms; no shared command can wrap them cleanly.

## What Not To Do

1. **Do not revive a shell shared-logic layer.** The long-term shared surface is
   `reusable-ci`, not `scripts/*`.
2. **Do not build a YAML generator or meta-DSL.** It would produce unidiomatic
   pipelines on both platforms and create a third thing to maintain.
3. **Do not add GitLab-specific keys to `artifacts.yml`.** Platform wiring
   belongs in platform YAML or provider adapters.
4. **Do not force parity where capabilities differ.** SLSA L3, GitHub Code
   Scanning, and GitLab report ingestion are different platform features.
5. **Do not keep compatibility aliases for renamed workflow inputs.** Major
   releases can break workflow contracts when the new name is clearer.

## End-State Directory Structure

```text
reusable-ci/
├── .github/workflows/           # GitHub Actions adapter
├── .gitlab/ci/                  # GitLab CI adapter / Catalog components
├── cmd/reusable-ci/             # CLI entrypoint
├── internal/                    # shared Go domain/app/adapters/cli
├── containers/runtime/          # shared runtime images carrying reusable-ci
├── scripts/bootstrap/           # runtime-image build-time installers only
├── examples/                    # GitHub and GitLab consumer examples
├── docs/
│   ├── gitlab.prep.md
│   ├── gitlabsupportplan.md
│   └── ...
└── artifacts.yml                # platform-agnostic config examples/schema docs
```
