<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# GitLab CI Support — Architecture & Future Plan

The single GitLab planning doc: architecture, design rules, the
capability/orchestration model, the delivery model, and the work still ahead.
For the authoritative per-forge maturity, see [`providers.md`](providers.md)
(gitlab is 🟡 *partial — release creation, token validation, repo metadata;
no asset upload, SARIF, or provenance profile*).

**More of the shared core already runs on GitLab than the phases below imply.**
Readable from the code:

- **GitLab provider adapter** (`internal/adapters/gitlab/`): release creation,
  token + bot-permission validation, repo metadata, event context, capabilities.
- **The CI sink layer is GitLab-native, not just GitHub.** On `--runner gitlab`,
  scalar outputs go through `gitlaboutput` (an `artifacts:reports:dotenv` writer),
  step summaries to `$CI_SUMMARY_FILE`, and stage manifests to `.ci-results/`
  (`$CI_RESULTS_DIR`). So `reusable-ci` commands' outputs/summaries/manifests
  already work in a GitLab job today.
- **Dual-emit security tools** (SARIF + GitLab `reports:*` JSON), the
  **buildah** `container build` (no Docker-in-Docker), and the cross-platform
  runtime images.

Catalog adapter YAMLs now exist (`templates/nanolinter.yml` + `templates/megalinter.yml` +
`examples/gitlab-nanolinter/` (+ `examples/gitlab-megalinter/`) + a repo-root `.gitlab-ci.yml` self-test); the
remaining Catalog work and the narrow provider gaps are in "Phases Ahead" below.

## Architecture

```text
                    ┌─────────────────────┐
                    │   artifacts.yml     │  Platform-agnostic product intent
                    └─────────┬───────────┘
                              │
              ┌───────────────┴───────────────┐
              │                               │
    ┌─────────▼──────────┐         ┌──────────▼─────────┐
    │  .github/workflows │         │  templates/ (+      │
    │  GitHub adapter    │         │  .gitlab-ci.yml)    │
    │                    │         │  GitLab adapter     │
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

- `publish-to: forge-packages` — GitHub-specific package destination.
- `enable-slsa: true` — produces **portable, signed SLSA provenance** (cosign
  attest via `reusable-ci container attest`, ~SLSA Build L2) on any forge,
  including GitLab. Only the *isolated-builder* L3 add-on (GitHub's attestation
  store / slsa-github-generator) is GitHub-only.

These do not require schema changes. The GitLab adapter can map, skip, or warn
based on platform capability while keeping the artifact contract stable.

## Capability Matrix

| Capability | GitHub | GitLab | Shared implementation |
|---|---|---|---|
| Build wrappers | Reusable workflows | CI components/jobs | `reusable-ci build ...` |
| Release planning | Reusable workflows | CI components/jobs | `reusable-ci config ...`, `reusable-ci plan ...` |
| Container build | `reusable-ci container build` (buildah; daemonless, native per-arch, no QEMU) | same `reusable-ci container build` on a plain GitLab runner — **no Docker-in-Docker / kaniko needed** | Portable — `container build` replaced docker/setup-buildx + build-push; identical on both forges |
| Run-artifact transport | `artifact upload/download` via the runner's `ACTIONS_RUNTIME_*` store | upload is **declarative** (`artifacts: paths:`, runner uploads at job end); download by **job** via the API with `CI_JOB_TOKEN` (no imperative named upload) | partial — download mappable behind the provider adapter; imperative upload has no GitLab equivalent (see "Artifact & credential model") |
| Signed SLSA provenance (L2) | `container attest` (cosign) | `container attest` (cosign) | Portable — `reusable-ci container attest` |
| Isolated-builder L3 add-on | GitHub attestation store / slsa-github-generator | No direct equivalent | GitHub-only; skip on GitLab |
| SBOM generation | Artifacts + signed cosign attestation | Artifacts + signed cosign attestation + `reports:cyclonedx` | Portable — `reusable-ci sbom ...` + `container attest --type cyclonedx` |
| Release creation | `gh` / GitHub API | GitLab API / `release:` keyword | Provider adapter plus GitLab YAML upload strategy |
| Package registry | GitHub Packages | GitLab Package Registry | Platform YAML + provider-aware validation |
| Container registry | GHCR | GitLab Container Registry | Registry URL config + namespace validation policy |
| SAST upload | SARIF to Code Scanning | `artifacts:reports:sast` | Producer emits SARIF + GitLab SAST JSON |
| Dependency scan upload | SARIF to Code Scanning | `artifacts:reports:dependency_scanning` | Producer emits SARIF + GitLab dependency JSON |
| Container scan upload | SARIF to Code Scanning | `artifacts:reports:container_scanning` | Producer emits SARIF + GitLab container JSON |
| Step summaries | `GITHUB_STEP_SUMMARY` | `$CI_SUMMARY_FILE` (write + publish as artifact / MR comment) | provider-neutral Markdown via the `stepsummary` sink — already wired for `--runner gitlab` |
| Scalar outputs | `$GITHUB_OUTPUT` | `artifacts:reports:dotenv` | provider-neutral `OutputSink` (`gitlaboutput`) — already wired |
| Build-time secret mounts | `containers[].build-secrets` + `REUSABLE_CI_BUILD_SECRETS_JSON` envelope (GHA secret) | same envelope shape supplied as a GitLab CI variable | `reusable-ci container materialize-build-secrets` consumes the envelope identically on both platforms |
| Privileged-trigger gate | `reusable-ci validate event-context` reads `GITHUB_EVENT_NAME` | needs `CI_PIPELINE_SOURCE` reader and a port allowlist (`push`, `web`, `schedule`, `pipeline`, `trigger`, `api`); refuse `merge_request_event` / `external_pull_request_event` | shared policy `domain/validate.RequireAllowedEvent` + `DefaultAllowedEvents` is **done**; only the GitLab reader (`CI_PIPELINE_SOURCE`) + its allowlist are missing |

> **Capability-flag note.** The gitlab adapter reports `Attestation: false`,
> which means **GitHub's native attestation API**, *not* "no signed provenance."
> `container attest` / `container sign` use cosign directly and don't consult that
> flag, so signed SLSA-L2 provenance and image signatures are portable to GitLab.
> Likewise `ReleaseAssets: true` reflects the `release:` capability; the
> *imperative asset upload* path is still stubbed (see Phase 4 below).

## Orchestration model

The GitLab adapter mirrors the reusable-ci GitHub structure: **components are
the reusable units** (≈ the reusable workflows under `.github/workflows/`), and
thin **orchestration flows** compose them (≈ the GitHub orchestrators). A
component stays thin — run `reusable-ci <verb>` in the runtime image, declare
GitLab-native `artifacts:reports:*`. Two composition shapes, by use case:

- **Static graph → flat `include:` + `stages:`/`needs:`/`rules:`.** For a fixed
  job set (PR quality: lint ∥ scan ∥ test) the consumer pipeline `include:`s the
  components and wires the graph. No generation; fully idiomatic. **Shipped as
  `examples/gitlab-pullrequest/`** — the GitLab PR orchestrator is the consumer
  composition (nanolinter or megalinter, scoped to MR context via `workflow:rules`), not
  a published mega-component. The gate is the pipeline status (GitLab fails the
  pipeline on any failed job natively — no GitHub-style `quality-status`
  aggregation job needed).
- **Plan-driven fan-out → parent–child pipeline fed by `reusable-ci plan` JSON.**
  The build/publish/release matrix varies per `artifacts.yml`, so it is
  computed: a prepare job runs `reusable-ci config parse-artifacts | reusable-ci
  plan release` → typed plan JSON → a generated child-pipeline YAML consumed via
  `trigger: { include: { artifact: <pipeline.yml>, job: prepare } }`.

**Why GitLab materializes a child pipeline where GitHub does not.** GitHub drives
its fan-out from the *same* plan JSON natively —
`strategy: matrix.artifact: ${{ fromJson(plan).targets.maven.items }}` — with no
YAML generation. GitLab's `parallel:matrix` cannot expand a value computed at
runtime, so the only way to turn plan JSON into a dynamic matrix is a child
pipeline whose YAML is materialized from that JSON. That materialization is the
**GitLab provider adapter** consuming the same typed contract (Design Rule 3) —
*not* a cross-platform meta-DSL. "What Not To Do" #2 still holds: one shared
pipeline generator for both forges is forbidden; a GitLab-only child-pipeline
emitter from the existing plan JSON is the platform's idiomatic shape.
`reusable-ci container platform-plan` already emits matrix-shaped JSON, so a
`plan` → GitLab-child-pipeline emitter is a natural extension of that pattern.

## Delivery model

The runtime image is the default delivery model for build / publish / security /
orchestrator jobs — components run `reusable-ci` inside it. A small set of
workflows stay on **bare runners** with a hard reason (none block GitLab):

- **`publish-container.yml`** — builds images with buildah (daemonless, no
  Docker), but needs host-level build capabilities (container storage, user
  namespaces) that a nested `container:` job complicates.
- **`self-runtime-container.yml`** — builds the runtime image itself; its merge
  job invokes the just-built `reusable-ci` because the image under test is the
  workflow's own output.
- **`security-openssf-scorecard.yml`** — uses the third-party
  `ossf/scorecard-action` Docker action (doesn't compose with a `container:`
  parent). Scorecard is GitHub-only by design; skipped on GitLab.
- **macOS workflows** (`build-xcode-ios.yml`, `publish-apple-appstore.yml`) —
  Linux runtime image not applicable; they install the binary on the macOS VM
  from `reusable-ci-binary-ref`.

## Phases Ahead

### Phase 1: GitLab Component Skeleton — shipped

The first Catalog entrypoints exist:

```text
templates/                     # consumable CI/CD Catalog components (top-level, required)
  nanolinter.yml               # single-file component (or nanolinter/template.yml for the dir form)
.gitlab-ci.yml                 # reusable-ci's own pipeline: self-test components + release: job to publish
examples/gitlab-nanolinter/    # consumer example (include: component@<version>)
```

GitLab CI/CD Catalog requires components under a **top-level `templates/`**
directory (not `.gitlab/ci/`, which is only for a project's own internal
includes). Each component is a thin GitLab adapter. The first, `nanolinter`, is
the default PR lint gate (the analog of GitHub's `lint-engine: nanolinter`): it runs
`nanolinter verify` in the nanolinter flavour image — which bundles the whole
check toolchain (opengrep SAST, osv-scanner, secrets, trivy-fs, ecosystem
linters), so we never run opengrep standalone — and fails the job on blocking
findings. (Most other components run `reusable-ci <verb>` in the runtime image
and declare GitLab-native `artifacts:reports:*`; nanolinter wraps the external
lint tool, mirroring the GitHub `lint-nanolinter` job.)

**Remaining before it can be published and trusted (the immediate next work):**

- **Publication prerequisites** (repo/settings, not code): mark the project a
  *CI/CD Catalog project*, give it a project description, and ensure the **root
  `README.md` documents the components**. Don't duplicate input docs there —
  GitLab renders inputs from each component's `spec:`.
- **Validate on a live GitLab runner**: run the self-test pipeline, confirm the
  SAST report lands on the MR Security tab (untested on a real instance so far),
  then cut the first semver tag to publish via the `release:` job.

### Phase 2: Shared Component Contracts

Define reusable include/extends shapes for common concerns:

- runtime image selection
- `reusable-ci` invocation and env mapping
- artifact paths and report declarations
- `.ci-results/` manifest upload
- failure semantics for quality/security jobs

### Phase 3: Release/Build Stage Adapter — *thin jobs via binary-owned sequences*

**Road decision (2026-06-20).** Don't lead with a big child-pipeline generator.
Each ecosystem build/publish job is a *bespoke multi-step* `reusable-ci`
sequence (Go build = metadata→download→test→sbom→compile→report), and that
sequence is **forge-independent** — GitHub's `build-go.yml` runs the same
`reusable-ci` calls a GitLab job would. Today that sequence is hand-wired in
GitHub YAML and *would be re-wired* in GitLab YAML — duplication that drifts, and
it's what makes a faithful emitter expensive.

So extend **Design Rule 1** from "the binary owns each step" to "the binary owns
the step *sequence*": consolidate each ecosystem behind one entrypoint
`reusable-ci build <eco> run`. Then:

- every forge's job collapses to one line; platform-transport (checkout, cache,
  artifact upload) stays thin in YAML (Rule 2);
- the sequence lives once in Go, shared by GitHub/GitLab/Forgejo (general);
- security-bearing steps (SBOM, sign, scan-gate, provenance) are enforced and
  ordered by the binary, not re-encoded per forge where one could be dropped;
- the GitLab **fan-out generator becomes small and late** — one-line jobs per
  plan item (golden-testable), or static includes for single-artifact projects.
  It was the wrong thing to lead with; it's a consequence of thin jobs.

Done incrementally, one ecosystem at a time, **validatable on GitHub before
GitLab** (each consolidation improves GitHub too).

- **✅ Go shipped (2026-06-20):** `reusable-ci build go run` orchestrates the full
  sequence in-process (no `$CI_OUTPUT` round-trip); `build-go.yml` collapsed its
  5 build steps + summary into one step; thin GitLab component
  `templates/build-go.yml`. Granular `build go <step>` subcommands remain as the
  composable units the orchestrator calls.
- **✅ npm shipped (2026-06-20):** `reusable-ci build npm run` (metadata → ci →
  test → build → SBOM → pack → summary). Per-step variations are now
  **configuration**: the renovate-pinned `cyclonedx-npm` version is a flag
  (`--sbom-tool-version` / `$CYCLONEDX_VERSION`), `--skip-tests`, `--build-sbom`,
  `--script` likewise. `build-npm.yml` collapsed 8 steps → one; thin component
  `templates/build-npm.yml`. The soft `npm test` (warn-not-fail) is preserved.
- **✅ cargo shipped (2026-06-21):** `reusable-ci build cargo run` (metadata →
  fetch → test → SBOM → compile → status). `cargo-cyclonedx` is baked (no
  version-pin wrinkle); no build-summary step (matches the workflow).
  `build-cargo.yml` collapsed 6 steps → one; thin component
  `templates/build-cargo.yml`.
- **Cohesion:** the common build knobs (Dir, ArtifactName, SkipTests,
  EnableBuildSBOM) are gathered in a shared `app/build.ReleaseBuildOptions`
  embedded by every `<eco>ReleaseBuildInput`, so adding an ecosystem reuses the
  contract instead of re-spreading fields. Shared CLI consts (`subCmdRun`,
  `flagBuildSBOM`, …) likewise.
- **✅ maven shipped (2026-06-21):** `reusable-ci build maven run` (install →
  metadata → app/lib build → SBOM → summaries). Most complex so far — the
  build-type branch (app/lib), CLI-opts, library profile, and the renovate-pinned
  cyclonedx-maven-plugin version are all **configuration** now; `mvn` runs in the
  step's working-directory (the GitLab component `cd`s in). `build-maven.yml`
  collapsed 7 steps → one; `templates/build-maven.yml`.
- **✅ gradle shipped (2026-06-21):** `reusable-ci build gradle run` (gradlew
  chmod → metadata → tasks → SBOM → summaries). The `chmod +x ./gradlew` step is
  now `os.Chmod` in the binary; gradle runs in cwd (the GitLab component `cd`s
  in); pinned cyclonedx-gradle-plugin version is config. `build-gradle-app.yml`
  collapsed 6 steps → one; `templates/build-gradle.yml`.
- **All five language ecosystems done:** go, npm, cargo, maven, gradle — each a
  thin one-line forge job over a binary-owned `build <eco> run` sequence,
  GitHub-validatable, with a thin GitLab component. The shared `shared.go` const
  set (`flagTasks`, `flagSBOMToolVersion`, `usageGradleTasks`, …) keeps growing
  as the cohesion anchor.
- **✅ build fan-out generator shipped (2026-06-21):** `reusable-ci plan
  gitlab-build-pipeline` reads the typed build-stage plan and emits a GitLab
  child pipeline — one `include:` of a `build-<eco>` component per running
  artifact (`internal/app/gitlabpipeline`). The "small, last piece" the thin jobs
  enabled: it synthesises no job bodies, just reuses the components. Consumed via
  `trigger:{include:{artifact:…}}` — see `examples/gitlab-release-build/`. This
  is the GitLab rendering of the same plan GitHub expands with `strategy:matrix`
  (not a meta-DSL; What-Not-To-Do #2 holds).
- **✅ container build-logic core consolidated (2026-06-21):** `reusable-ci
  container build-and-scan` owns build → (push-by-digest) scan, threading the
  digest in-process (build → scan `name@digest`) instead of round-tripping it
  through the output sink; the digest is still emitted for `manifest merge`, and
  the scan writes its SARIF + GitLab container-scanning report for the forge job
  to upload. Additive — `container build` + `security scan container` remain.
  Adoption (collapsing publish-container.yml's build+scan steps, a GitLab
  container component) waits for a buildah+registry runner to validate against —
  not rewriting that 20-step security-critical workflow blind.
- **✅ gradle-android shipped (2026-06-21):** `reusable-ci build gradle-android run`
  (artifact-names → keystore → metadata → tasks → secrets.properties → build →
  SBOM → status). The keystore is decoded outside the project dir and its path
  threaded to gradle in-process via the env (signing passwords stay env the build
  reads); artifact-names + version emitted as job outputs for downstream
  upload/publish. `build-gradle-android.yml` collapsed ~9 steps → one;
  `templates/build-gradle-android.yml`. **The entire Linux build set is now
  consolidated** (go, npm, cargo, maven, gradle, gradle-android).
- **✅ xcode-ios shipped (2026-06-21):** `reusable-ci build xcode-ios run`
  consolidates the reusable-ci build-logic core (artifact-name → signing →
  metadata → xcconfig → archive → export → list); the xcconfig path is threaded
  to the archive in-process; the macOS host setup (xcode-select/brew/xcodegen)
  stays in the forge YAML (not reusable-ci's to own). `build-xcode-ios.yml`
  collapsed its 7 reusable-ci steps → one. No GitLab component (macOS GitLab
  runners are niche; the consolidation still benefits the existing macOS job and
  a future runner). **Every build ecosystem is now consolidated** (go, npm,
  cargo, maven, gradle, gradle-android, xcode-ios).
- **✅ `build-and-scan` adopted in publish-container.yml (2026-06-21):** the
  per-platform "Build by digest" + "Scan" steps collapsed into one
  `container build-and-scan` (scan self-gates on the digest = push mode); the
  `install trivy` step moved ahead of it (carrying the `&if_scan_enabled`
  anchor); the digest still flows to export/upload and the SARIF/GitLab reports
  to their uploads — actionlint green.
- **Deliberate boundary:** SBOM, attest, and extract stay as the per-platform
  job's *distinct* steps — they're separate tool pipelines (syft / cosign-signing
  / a second `container build` in export mode), not "more of build-and-scan".
  Merging all five would be a ~30-param orchestrator wiring
  buildah+ggcr+trivy+syft+mvn+git+cosign and handling **signing keys** — an
  unmaintainable, unverifiable anti-pattern. `build-and-scan` (the digest-coupled
  build+scan) is the correct unit.
- **✅ publish-side fan-out generator shipped (2026-06-21):** `reusable-ci plan
  gitlab-publish-pipeline` — the publish sibling of `gitlab-build-pipeline`, one
  `include:` of the matching `publish-<target>` component per running item
  (Maven Central, GitHub Packages, Google Play, App Store, container). The
  fan-out helper was generalised (`addTargetIncludes[T]`) so build
  (PlannedArtifact) and publish (PlannedArtifact + PlannedContainer) share one
  mechanism. The `publish-<target>` components don't exist yet (publishing is
  credential-bound) — this is the ready wiring; the includes are structurally
  valid.
- TODO: author the `publish-<target>` Catalog components (credential-bound to
  external registries — best done against a live runner); live GitLab end-to-end
  validation of everything shipped.

### Phase 4: Provider Depth

Finish provider behavior only when a GitLab component actually needs it. The
model behind these is the *Artifact & credential model* section above:

- **Token-header selection** (`JOB-TOKEN` vs `PRIVATE-TOKEN`).
- **Run-artifact role** — implement `RunArtifactDownloader` for GitLab
  (`CI_JOB_TOKEN` / `CI_API_V4_URL`; map `--name` → producing job name);
  imperative upload stays `ErrUnsupported`-with-guidance.
- **Release-asset upload/link** (Generic Package Registry + asset links) — the
  adapter's `CreateRelease` works, but asset linking is still stubbed.
- **`CI_PIPELINE_SOURCE` event reader** for `validate event-context` (the shared
  `RequireAllowedEvent` policy already exists; only the GitLab reader is missing).
  Coherent shape: resolve `EventName` from the provider Context (all three
  adapters already populate it) rather than the current hardcoded
  `GITHUB_EVENT_NAME` env source — a shared fix, not a GitLab branch.
- **Stage-result aggregation — ✅ shipped (forge-neutral core + two input adapters).**
  `report stage-result` aggregates job outcomes against the stage plan through
  one shared pure function (`summary.ResolveTargetResults`) with one fail-closed
  rule (`summary.NormalizeJobStatus`: unknown/missing → failure, never skipped).
  Two capability-shaped input adapters feed it, because how a job learns sibling
  outcomes is platform-forced, not accidental:
  - GitHub/Forgejo children are `uses:` jobs — the summary job passes
    `--job-results` (a neutral `{job:{result}}` map, fed `toJson(needs)`).
  - GitLab jobs can't read siblings — each records its own outcome with the new
    `report job-result` verb (jobs/<job>.json under `$CI_RESULTS_DIR`), and the
    summary collects them.

  This is the coherent realisation of "manifests as the durable cross-provider
  shape": not "manifests everywhere" (anti-idiomatic on GitHub, whose `uses:`
  children can't self-record without ~15 workflow edits), but one aggregator
  behind two forge-native feeds. The earlier framing — *remove* `toJson(needs)` —
  was over-reach; the real fix was to stop it driving a *separate* path and route
  it through the shared aggregator. The GitLab feed is demonstrated end-to-end in
  `examples/gitlab-stage-summary/`: each job records its outcome in
  `after_script` (`$CI_JOB_STATUS`), the summary job `needs:` them and
  aggregates; scalar outputs go to a `reports:dotenv` (`$CI_OUTPUT`). Verified
  locally including the fail-closed path (a job that left no record → failure).
- **Registry/package validation** verified against real GitLab runners
  (`container validate namespace` is already registry-configurable via
  `--enforce-namespace-on`).
- **SARIF → GitLab-SAST converter** (`security report to-gitlab-sast`) — ✅ shipped.
  A generic SARIF v2.1.0 document (e.g. nanolinter's unified security SARIF) is
  converted to the GitLab SAST report schema so the `nanolinter` component can
  populate the MR Security tab via `artifacts:reports:sast`. The scanner identity
  is read from the SARIF `tool.driver`; severity comes from
  `properties.security-severity` (CVSS band) when present, otherwise from the
  SARIF `level`; each finding gets a deterministic UUID. This is the SAST sibling
  of the Trivy converters (`to-gitlab-dep`/`to-gitlab-container`). The
  `nanolinter` component now calls it and publishes `artifacts:reports:sast` —
  it installs reusable-ci at runtime via the cosign-verified bootstrap (the
  flavour image bakes curl + cosign), converts, and the SAST step is best-effort
  (never reds the lint gate). Display of the Security tab needs GitLab Ultimate;
  producing the report is tier-agnostic. Not yet validated on a live instance.

> **Container builds are a viable early target.** `reusable-ci container build`
> is now buildah (daemonless, native per-arch, no QEMU), so a GitLab
> container-build component runs on a plain runner with **no Docker-in-Docker /
> kaniko** — it doesn't have to wait for Phase 3.

## Deliberate GitHub coupling

One reusable-ci surface stays GitHub-specific by design, not by oversight:

- `reusable-ci security report upload-sarif` — pushes SARIF to GitHub
  Code Scanning. The GitLab equivalent isn't an imperative upload: GitLab
  ingests `artifacts:reports:sast` declaratively. For the *opengrep* scanner the
  producer already emits that format directly (`--gitlab-sast-file`); for
  *nanolinter*'s unified SARIF the `security report to-gitlab-sast` converter
  (Phase 4 above, now shipped) produces it.

It has a GitLab counterpart through a different mechanism; no shared command
can wrap it cleanly.

> **Obsolete (now configurable):** `container validate namespace` was once
> hardcoded to `ghcr.io`. It now takes `--enforce-namespace-on` (default
> `ghcr.io`): a GitLab component points it at the GitLab Container Registry
> host and the same policy check runs. No longer a GitHub coupling.

## Artifact & credential model

Two cross-provider realities the original plan predates:

1. **Run-artifact transport differs in *shape*, not just names.** GitHub/Forgejo
   Actions are name-keyed and imperative (`artifact upload --name X` /
   `download --name X`) over the runner store (`ACTIONS_RUNTIME_TOKEN/URL`).
   GitLab is **job-keyed and declarative**: a job emits one bundle via
   `artifacts: paths:` (the runner uploads at job end — there is no imperative
   "push a named artifact" API), and you download a *job's* bundle through the
   API with `CI_JOB_TOKEN`/`CI_API_V4_URL`. So a GitLab provider adapter can
   implement `RunArtifactDownloader` (map `--name` → producing job name) but
   `artifact upload` should stay `ErrUnsupported`-with-guidance ("declare
   `artifacts:` in `.gitlab-ci.yml`"), not a shim.

2. **Two credential classes, two error helpers** (`internal/domain/errs`):
   - **Runner-injected, no flag** → `errs.RuntimeRequired(op, provider, vars)`.
     The var *names* are a parameter, so the GitLab adapter passes
     `CI_JOB_TOKEN` / `CI_API_V4_URL` where GitHub/Forgejo pass `ACTIONS_*`.
     The message prose stays provider-neutral.
   - **User-suppliable** (`PRIVATE-TOKEN`/PAT, registry password, GPG key) →
     `errs.CredentialRequired(Credential{What,Flag,Env})` → "pass `--x-file` or
     set `$X`". The prep doc's "JOB-TOKEN vs PRIVATE-TOKEN" item maps onto this
     split: JOB-TOKEN is the runner-injected class, PRIVATE-TOKEN the
     user-suppliable one.

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
├── templates/                   # GitLab CI/CD Catalog components (consumable)
├── .gitlab-ci.yml               # reusable-ci's own GitLab pipeline (self-test + release)
├── cmd/reusable-ci/             # CLI entrypoint
├── internal/                    # shared Go domain/app/adapters/cli
├── containers/runtime/          # shared runtime images carrying reusable-ci
├── scripts/bootstrap/           # runtime-image build-time installers only
├── examples/                    # GitHub and GitLab consumer examples
├── docs/
│   ├── gitlabsupportplan.md      # the single GitLab planning doc
│   └── ...
└── artifacts.yml                # platform-agnostic config examples/schema docs
```
