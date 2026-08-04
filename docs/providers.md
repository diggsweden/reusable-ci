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
| Keyless OIDC signing (public Fulcio, no extra flags) | ✅ | ✅ | ❌ | ❌ |
| Keyless OIDC signing against your own Fulcio | ✅ | ✅ | ✅ | ❌ |
| Release asset upload | ✅ | ✅ | ✅ | ❌ |
| Run artifact upload/download | ✅ | ❌ | ✅ | ❌ |
| Container tag deletion (package/registry API) | ❌ | ✅ | ✅ | ❌ |
| Container tag listing (package/registry API) | ❌ | ✅ | ✅ | ❌ |

This matrix is not maintained by hand: `PAR-CAP-1` renders it from what the
adapters actually implement and fails when the two drift, and the live
conformance tier exercises each claim against a real forge.

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
- **Provenance** (`release provenance`) — does not degrade, because it does not
  use a forge attestation API. The statement is built from the checksums file
  plus the run context every provider resolves, and signed with cosign, so it
  is emitted identically on every forge. `--profile forgejo-actions` reproduces
  forgejo-ci's shipped builder id; every other forge uses the default `generic`
  profile.
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
directly). Provenance uses the `generic` profile — there is no GitLab-specific
one, and none is needed.

**Step outputs are renamed on GitLab, and this is the one difference a
pipeline has to know about.** GitLab has no per-step output file: values reach
later jobs through a dotenv report, whose variable names must be upper snake
case. So the CLI's lower-hyphenated output keys are translated on the way out:

| Output | Forgejo / GitHub | GitLab |
|---|---|---|
| `version` | `version` | `VERSION` |
| `version-no-v` | `version-no-v` | `VERSION_NO_V` |
| `project-name` | `project-name` | `PROJECT_NAME` |

The value is identical; only the name a later job resolves differs. The job
must nominate the dotenv path in `$CI_OUTPUT` and declare it — the CLI writes
where the pipeline says, because GitLab provides no default:

```yaml
resolve:
  variables:
    CI_OUTPUT: build.env
  script:
    - reusable-ci release resolve metadata --version "$CI_COMMIT_TAG" --repository "$CI_PROJECT_PATH"
  artifacts:
    reports:
      dotenv: build.env
```

`PAR-RUN-5` asserts this against a real runner on both forges.

### forgejo

**Publishing packages needs a token you supply — the automatic one is not
enough.** A Forgejo Actions job gets an automatic token as `$FORGEJO_TOKEN` /
`$GITHUB_TOKEN`, but on `push` events it carries
[read permission to the repository](https://forgejo.org/docs/latest/user/actions/reference/),
and the package registry refuses it: a write answers 401/403. So
`publish forge-packages deploy` fails with the automatic token, and the job must
supply one with `write:package`:

```yaml
- name: publish to the forge package registry
  env:
    FORGEJO_TOKEN: ${{ secrets.PACKAGE_TOKEN }}   # needs write:package
  run: reusable-ci publish forge-packages deploy --project-type npm
```

The container registry is different: the automatic token *does* authenticate
there for reads, so `container login` needs no extra secret. GitLab needs
nothing in either case — `$CI_JOB_TOKEN` carries package-write for the project
that issued it. `PAR-PKG-1` and `PAR-REG-5` check both against real runners.

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
but public Fulcio does not trust a Forgejo issuer. So keyless signing works —
against a Fulcio configured to trust the instance, with an explicit
`--oidc-issuer` and `--fulcio-url` — but is not the default; without those,
use key-based signing. That is the distinction the two keyless rows in the
matrix above draw. Has a provenance profile (`.forgejo/workflows/`).

### local
The dev/test fallback. No forge API — release/token/SARIF commands gate with
a typed "unsupported on platform local" error. Use it to run verbs on a
laptop without a forge.

## Implementation status

Honest maturity per forge, so you know what to rely on:

| Forge | Status |
|---|---|
| **github** | ✅ Established production path (the original target). |
| **gitlab** | 🟡 Partial — release creation, asset upload/linking (project uploads + release links), token validation, and repo metadata; **no** SARIF ingestion (GitLab consumes the JSON SAST report instead) and no GitLab-specific provenance profile (the generic one applies). |
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
