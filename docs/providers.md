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

They are detected separately because **Forgejo Actions is a GitHub-Actions
runner clone with a Gitea server behind it** — it sets `GITHUB_ACTIONS=true`
and exports `$GITHUB_OUTPUT`/`$GITHUB_STEP_SUMMARY`, but its API is the
Gitea `/api/v1` surface, not GitHub's. Keeping the axes separate is what
lets one binary serve both without misrouting API calls.

| Forge | RunnerKind | Forge API (`Platform`) |
|---|---|---|
| GitHub Actions | `gha-compatible` | `github` |
| Forgejo Actions | `gha-compatible` | `forgejo` |
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
| Runner | `--runner` | `REUSABLE_CI_RUNNER` | `auto`, `gha-compatible`, `gitlab`, `local` |

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

How commands degrade when a capability is absent:

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
SDK). Auth precedence `$FORGEJO_TOKEN` → `$GITEA_TOKEN` → `$GITHUB_TOKEN`;
server from `$FORGEJO_SERVER_URL` → `$GITHUB_SERVER_URL` → `codeberg.org`.
No SARIF; no published OIDC issuer yet (pass `--oidc-issuer` for keyless, or
use key-based signing). Has a provenance profile (`.forgejo/workflows/`).

### local
The dev/test fallback. No forge API — release/token/SARIF commands gate with
a typed "unsupported on platform local" error. Use it to run verbs on a
laptop without a forge.

## Implementation status

Honest maturity per forge, so you know what to rely on:

| Forge | Status |
|---|---|
| **github** | ✅ Established production path (the original target). |
| **gitlab** | 🟡 Partial — release creation, token validation, and repo metadata; **no** asset upload, SARIF, or provenance profile. |
| **forgejo** | 🟡 Adapter fully implemented (release create + asset upload, token + bot-permission probes, repo metadata, capabilities, provenance profile) and unit-tested against an httptest Gitea server, **but not yet validated against a live instance or wired into forgejo-ci** — that is the next integration step. |
| **local** | ✅ Dev/test fallback; forge-API commands gate with a typed "unsupported" error. |

Cross-forge verbs status:

- `release provenance` (generate) and `container ledger {add,validate}` — pure,
  golden/table-tested, ready.
- `release provenance --sign-key` and `container ledger {verify,promote}` —
  fake-tested for logic. verify/promote talk to the registry in-process via
  go-containerregistry (daemonless — no docker); cross-registry promotion and
  `release provenance --sign-key` additionally shell out to `cosign`.
  `container ledger cleanup` deletes staging tags through the
  **forge's package API** (a `TagDeleter` provider role — Forgejo via the Gitea
  SDK), *not* skopeo: staging and final tags share one manifest, so an OCI
  manifest-delete would destroy the promoted image. Cleanup is therefore
  forge-gated (Forgejo today; other forges add their own package-API deleter).

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
