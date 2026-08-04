<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Providers and runners

The `reusable-ci` CLI is forge-agnostic: the same verbs run on GitHub
Actions, GitLab CI, Forgejo Actions, and on a developer laptop. This page
explains how the CLI decides which forge it is talking to, how to override
that, and which features are available where.

## Two axes: runner conventions vs forge API

"Which CI platform" is really two independent things:

- **Runner conventions** (`RunnerKind`) — how the CLI emits output: the
  workflow-command dialect (`::error::`), the `$*_OUTPUT` key/value writes,
  and the step-summary file.
- **Forge API** (`Platform`) — the server REST surface used for releases,
  asset upload, token/permission checks, repo metadata, SARIF, and run
  artifacts (upload/download via each forge's artifact service — GitHub v4
  results, Forgejo v3 runtime).

They are detected separately because the two axes genuinely disagree —
most visibly for **Forgejo**, which is its own value on *both*. Forgejo
Actions sets `GITHUB_ACTIONS=true`, but Forgejo itself states it is
["not designed to be compatible"](https://forgejo.org/docs/latest/user/actions/github-actions/)
with GitHub Actions, only familiar. The differences land squarely on this
binary's output surface: Forgejo does **not** render `::error::`
annotations, `::group::` log folds, or job summaries
([go-gitea/gitea#27898](https://github.com/go-gitea/gitea/issues/27898);
[nektos/act#1187](https://github.com/nektos/act/issues/1187)), its native
step-output var is `$FORGEJO_OUTPUT` (with `$GITHUB_OUTPUT` as a
compatibility alias since Forgejo Runner 7.0.0), and it
[documents no job-summary variable at all](https://forgejo.org/docs/latest/user/actions/reference/)
— only `OUTPUT`/`ENV`/`PATH` step files. So `forgejo` is a distinct runner
dialect — it emits plain `Error: …` log lines instead of unrenderable
workflow commands, and routes step summaries to the job log. Keeping the
axes separate is what lets one binary serve every forge without misrouting
API calls *or* emitting output a runner silently drops.

| Forge | RunnerKind | Forge API (`Platform`) |
|---|---|---|
| GitHub Actions | `github` | `github` |
| Forgejo Actions | `forgejo` | `forgejo` |
| GitLab CI | `gitlab` | `gitlab` |
| Local / dev | `local` | `local` |

## Detection and overrides

Auto-detection reads the runner's environment, in this order (the Forgejo
check runs **before** GitHub because Forgejo masquerades as GitHub):

1. `$FORGEJO_ACTIONS` / `$GITEA_ACTIONS` / `$FORGEJO_SERVER_URL` /
   `$FORGEJO_REPOSITORY` / `$FORGEJO_OUTPUT` present → **forgejo**
2. `$GITHUB_ACTIONS=true` → **github**
3. `$GITLAB_CI=true` → **gitlab**
4. otherwise → **local**

Auto-detection is only a default. Both axes have explicit overrides — useful
when a runner masquerades as another forge or is misconfigured:

| Override | Flag | Env var | Values |
|---|---|---|---|
| Forge API | `--provider` | `REUSABLE_CI_PROVIDER` | `auto`, `github`, `gitlab`, `forgejo`, `local` |
| Runner | `--runner` | `REUSABLE_CI_RUNNER` | `auto`, `github`, `forgejo`, `gitlab`, `local` |

```bash
# Force the Forgejo forge API even though the runner sets GITHUB_ACTIONS=true
reusable-ci --provider forgejo release create ...
```

Run `reusable-ci doctor` to see the resolved provider, runner, and the
capability matrix for the current environment.

## Capability matrix

Not every forge implements every feature. The CLI reports each forge's
capabilities (and `doctor` prints them), and commands **degrade** rather
than fail when a capability is missing.

| Capability | github | gitlab | forgejo | local |
|---|:---:|:---:|:---:|:---:|
| SARIF / Code Scanning upload | ✅ | ❌ | ❌ | ❌ |
| SLSA build-provenance attestation API | ✅ | ❌ | ❌ | ❌ |
| Keyless OIDC signing | ✅ | ✅ | ❌ | ❌ |
| Release asset upload | ✅ | ✅ | ✅ | ❌ |
| Run artifact upload/download | ✅ | ❌ | ✅ | ❌ |
| Container tag deletion (package/registry API) | ❌ | ✅ | ✅ | ❌ |
| Container tag listing (package/registry API) | ❌ | ✅ | ✅ | ❌ |

How commands degrade when a capability is absent:

A refusal for a missing capability exits **78** (`EX_CONFIG`), never 69
(`EX_UNAVAILABLE`). 69 means an external system is unavailable and is worth
retrying; a capability the forge does not have never arrives, so a pipeline
that retries on 69 would loop forever. Treat 78 here as "this pipeline is
configured for a forge that cannot do this" and change the configuration or the
forge.

- **SARIF upload** (`security report upload-sarif`) — only github implements
  the code-scanning role. On gitlab/forgejo/local the command emits a
  `Notice` and exits 0; route findings to the step-summary or an uploaded
  artifact instead. No failed API call.
- **Keyless signing** (`release sign --method=sigstore`) — on a forge without
  keyless OIDC and no explicit `--oidc-issuer`, the CLI emits a `Warning`
  naming the forge and suggesting `--method=gpg`/`--method=kms` or an explicit
  issuer (it does not hard-fail).
- **Provenance** (`release provenance`) — github and forgejo supply a build
  profile; gitlab and local refuse with a clear "unsupported on <forge>"
  rather than emitting a dishonest predicate.
- **Run artifacts** — github and forgejo expose an intra-run artifact store
  through their runner's own service; gitlab does not, so a step that would
  hand a file to a later job must use GitLab's native `artifacts:` keyword in
  the pipeline definition instead. The capability is reported only when both
  upload and download are implemented, so a half-supported forge reports false
  rather than promising a store that cannot round-trip.

## Per-forge notes

### github
Full role set. OIDC issuer `https://token.actions.githubusercontent.com`.
Token advice on `validate auth token` (classic-PAT refusal). Releases,
assets, and SARIF go through the GitHub REST API (`go-github`).

### gitlab
Releases via `/api/v4`. OIDC issuer `$CI_SERVER_URL` (self-hosted) or
`https://gitlab.com`. No SARIF (GitLab consumes the JSON SAST report
directly) and no provenance profile.

### forgejo
Releases + asset upload via the Gitea `/api/v1` surface (official Gitea Go
SDK). Auth precedence `$CI_TOKEN` → `$FORGEJO_TOKEN` → `$GITEA_TOKEN` →
`$GITHUB_TOKEN`; server from `$FORGEJO_SERVER_URL` → `$GITHUB_SERVER_URL` →
`codeberg.org`. Precedence is only half the rule: a token is handed over only
if it may be sent to the server being called. A token the current runner
injected is valid only at that runner's own server — so on a GitHub runner
publishing to a Forgejo instance, `$GITHUB_TOKEN` is GitHub's job token and is
withheld rather than transmitted. A token the runner does not inject (a
workflow that sets `$FORGEJO_TOKEN` on GitHub, or a dedicated `$RELEASE_TOKEN`)
was set deliberately and is used as asked.
No SARIF ([forgejo#3669](https://codeberg.org/forgejo/forgejo/issues/3669)).
Forgejo **v15.0+** issues OIDC id-tokens (job-level
[`enable-openid-connect`](https://forgejo.org/docs/v15.0/user/actions/security-openid-connect/)),
but public Fulcio does not trust a Forgejo issuer, so keyless signing still
needs an explicit `--oidc-issuer` (and a Fulcio that trusts it) — otherwise
use key-based signing. Has a provenance profile (`.forgejo/workflows/`).

### local
The dev/test fallback. No forge API — release/token/SARIF commands gate with
a typed "unsupported on platform local" error. Use it to run verbs on a
laptop without a forge.

## Implementation status

Honest maturity per forge, so you know what to rely on:

| Forge | Status |
|---|---|
| **github** | ✅ Established production path (the original target). |
| **gitlab** | 🟡 Partial — release creation, asset upload/linking (project uploads + release links), token validation, and repo metadata; **no** SARIF ingestion (GitLab consumes the JSON SAST report instead) or provenance profile. |
| **forgejo** | 🟢 Adapter fully implemented (release create + asset upload, token + bot-permission probes, repo metadata, capabilities), unit-tested against an httptest Gitea server, and **live in production via the forgejo-ci middle layer** — its vendored binary drives real Codeberg releases (nanolinter). Consumers adopt via forgejo-ci's reusable workflows + consumer kit (requires Forgejo v15+ for workflow_call job expansion), not via engine-shipped orchestrators. |
| **local** | ✅ Dev/test fallback; forge-API commands gate with a typed "unsupported" error. |

Cross-forge verbs status:

- `release provenance` (generate) and `container ledger {add,validate}` — pure,
  golden/table-tested, ready.
- `release provenance --key` and `container ledger {verify,promote}` —
  fake-tested for logic. verify/promote talk to the registry in-process via
  go-containerregistry (daemonless — no docker); cross-registry promotion and
  `release provenance --key` additionally shell out to `cosign`.
  `container ledger {cleanup,rollback}` delete staging and pointer tags through
  the **forge's own package/registry API** (a `TagDeleter` provider role —
  Forgejo via the Gitea SDK, GitLab via the project registry API), *not* skopeo:
  staging and final tags share one manifest, so an OCI manifest-delete would
  destroy the promoted image. Both forge APIs are tag-scoped and keep the
  manifest. Cleanup is therefore forge-gated; github and local have no deleter
  yet and refuse with a typed `unsupported` error rather than reaching for an
  unsafe generic delete.
- **Untagged manifests are not retained everywhere.** GitLab keeps a manifest
  reachable by digest after its last tag moves away; Forgejo drops it
  immediately. This decides whether a release is recoverable: `container ledger
  rollback --journal` restores a moving tag (`:stable`) to the digest it held
  before, and on Forgejo that previous image survives only while some tag still
  references it — in practice its own immutable `:<version>` tag. Keep version
  tags for as long as you may want to roll back to them. When the previous image
  is genuinely gone, rollback says so and fails permanently; it does not report a
  transient error that would have CI retry.

## Forge-agnostic commands worth knowing

These work the same on every forge (the forge-specific plumbing is hidden):

- `release provenance` — generate an in-toto / SLSA-v1.0 provenance
  statement from a checksums file (github + forgejo), optionally signed with
  `cosign sign-blob`.
- `container ledger {add,validate,verify,promote,cleanup}` — the digest-first
  release-image ledger: record image entries, re-validate them at the trust
  boundary, and verify/promote/clean up candidate→final tags against the
  registry. Pure OCI refs/digests/tags — identical on `ghcr.io` and
  `codeberg.org`.
