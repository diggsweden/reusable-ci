<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Signing convergence

Status: **in progress** — Phase 4a (the `SigningIdentityResolver` role) has
landed; no user-visible behaviour has changed yet because nothing consumes the
role at the CLI edge so far. This document is the canonical phased plan for
converging artifact and git-object signing onto safer, forge-agnostic defaults
**without removing any current capability**.

Implemented so far (Phase 4a, the keystone):

- `internal/domain/provider/signingidentity.go` — `KeylessIdentity`,
  `SigningIdentityResolver`, and the security-critical `AnchorIdentity` helper
  (anchored, regexp-escaped, fail-closed). Unit + sibling-repo-rejection tests.
- `internal/adapters/{github,gitlab,forgejo}/signingidentity.go` — each reuses
  the existing `Describe().OIDCIssuer` and `Capabilities().KeylessOIDC` (single
  source of truth) and adds the anchored subject identity. `local` does not
  implement it → unsupported-role fallback. Compile-time conformance vars added.
- `internal/cli/deps` — `RequireSigningIdentityResolver()`.

Implemented (Phase 1, verify side):

- `internal/cli/deps` — `SigningIdentityForDetected()` (optional-role lookup;
  false for local / non-OIDC forges).
- `internal/cli/commands/validate/keylessidentity.go` — `keylessVerifyIdentity`
  fills empty `--cert-identity-regexp` / `--cert-oidc-issuer` from the resolver;
  explicit flags win; no-op when no resolver or `SupportsKeyless()==false`.
- Wired into `validate artifact-signature` and `validate container-signature`
  (skipped for KMS verifies, where `--key` is present). `validate tag signature`
  is git-tag (SSH/GPG) verification and intentionally untouched.
- The signer-side issuer default was already present via the `Describer` role
  (`DefaultOIDCIssuer(deps.DescriberForDetected())`), so no change was needed
  there — the resolver's net-new value is the anchored *verify* identity.

Implemented (Phase 1 docs + Phase 2 greenfield):

- `docs/verification.md` — `--method=sigstore` section documents the in-CI
  zero-config verify (derived, anchored identity) plus the explicit form;
  `validate container-signature` cross-referenced; the Forgejo issuer row
  corrected; "Choosing a method" now states the **greenfield recommendation**
  (sigstore for new GitHub/GitLab projects; gpg for PGP-native ecosystems; kms
  for HSM/Vault) and that the unset-fallback stays gpg.
- `examples/go-cli/artifacts.yml` — demonstrates `sign: method: sigstore` as the
  recommended default, with a comment pointing at the per-method guidance.

Phase 2 is **greenfield-only by design**: there is no scaffolding generator, and
flipping `SignConfig.EffectiveMethod()`'s unset-fallback would change behaviour
for every existing repo that never configured signing — a no-loss violation. So
the "default" moves via the recommendation + example, not via the constant.

Implemented (Phase 3, producer side — scoped to **gpg + ssh**, gitsign dropped):

- `internal/app/release/sshsigning.go` — `SSHSigningSetup` writes the OpenSSH
  key (newline-normalised) and configures git (`gpg.format=ssh`,
  `user.signingkey`, optional `user.name`/`user.email`/`commit.gpgsign`). It
  reuses the existing `gitSigningOps` seam (same `Run`+`Config` slice GPGImport
  uses), so `version tag-release` / `commit-push` then sign over SSH with no
  change — `git tag -s` / `commit -S` already delegate to git's configured
  backend.
- `internal/cli/commands/release/ssh.go` — `release ssh setup` / `release ssh
  cleanup` (idempotent, `if: always()`-safe; shared `$RUNNER_TEMP` key path).
  Registered under the release "Sign" category; `docs/cli-reference.md`
  regenerated.
- Consumer side already existed: `validate tag signature` verifies SSH tags
  against `.reusable-ci/allowed_signers` — forge-independent (`git verify-tag`).

**Why gitsign was dropped:** Sigstore git signing has uneven verification
tooling off GitHub and adds a dependency; SSH signing is git-native, fully
forge-portable, and verifiable via `allowed_signers`. gpg + ssh covers the goal
(no long-lived OpenPGP key on the runner) without it. GPG remains the default
for git objects.

Implemented (Phase 3, config + plan exposure):

- `internal/domain/config/gitsigning.go` — `git-signing:` block with
  `GitSignMethod` (`gpg` default | `ssh`), `EffectiveMethod`, `Validate`. Wired
  into `Config` and `Config.Validate`; added to `.reusable-ci/artifacts.schema.json`
  (top-level `additionalProperties:false`, so the `$defs/git-signing` entry was
  required).
- `internal/domain/pipeline/configplan.go` — `PlannedGitSigning` exposes
  `git_signing.method` in the config plan (always concrete, gpg default), so the
  orchestrator can branch on it without nil checks. Golden refreshed.

Implemented (Phase 3, workflow — leaf reusable workflow):

- `version-bump.yml` selects the signing path from a new `git-signing-method`
  input (default `gpg`, so existing callers are byte-identical): the GPG-import
  step is gated `!= 'ssh'`, a `release ssh setup` step is gated `== 'ssh'`, the
  commit author falls back to `committer-name/email` inputs when there is no GPG
  key identity, and an `if: always()` `release ssh cleanup` mirrors the GPG
  cleanup. New optional `RELEASE_SSH_SIGNING_KEY` secret. actionlint-clean
  (`just lint-actions`).

Implemented (Phase 3, caller chain — additive, actionlint-clean):

- `release-orchestrator.yml` → `release-prepare-stage.yml` → `version-bump.yml`
  now thread `git-signing-method` (the orchestrator reads
  `fromJson(config-plan-json).git_signing.method`, default gpg → existing repos
  unchanged), `committer-name`/`committer-email`, and the
  `RELEASE_SSH_SIGNING_KEY` secret.

**SSH committer-identity decision (resolved):** the committer email is supplied
explicitly because, for SSH signing, it is the verification *principal* —
`ssh-keygen -Y verify` matches the committer/tagger email against
`.reusable-ci/allowed_signers` (exactly what `validate tag signature` does). So
it cannot be derived from the runner actor or synthesised; it must be an identity
the org listed in `allowed_signers`. The input descriptions now spell this out,
and `doctor` cross-checks it (below). gpg verifies by key fingerprint, so its
committer comes from the key UID and needs no input.

Still CI-validated only: an end-to-end ssh release has not been exercised (no
real `RELEASE_SSH_SIGNING_KEY` in CI yet); the wiring is additive and defaults
to the unchanged gpg path.

Implemented (Phase 5, advisory):

- `internal/app/doctor` — `ssh git-signing allowlist` check: when
  `git-signing.method=ssh`, warns if `.reusable-ci/allowed_signers` is missing
  (SSH-signed tags can't be verified without it) and otherwise reminds that the
  configured committer-email must be one of its principals. Silent for the gpg
  default.
- `internal/app/doctor` — a non-failing `signing method recommendation` check
  (severity OK) that fires only when the repo signs artifacts with gpg, the
  detected forge supports keyless, and no artifact publishes to a PGP-native
  registry (Maven Central). The CLI passes `KeylessAvailable` from the resolved
  forge capabilities. Suppressed when already on sigstore/kms, when keyless is
  unavailable, or when a Maven Central publish genuinely needs a PGP `.asc`.

Implemented (Phase 5, branch-point retirement):

- `PlannedSign` gained two precomputed semantic flags (matching the existing
  `RequiresIDToken` precedent): `imports_gpg_key` (method==gpg) and
  `signs_containers` (sigstore|kms — cosign can sign OCI, gpg cannot). golden
  refreshed; a table test locks the flags per method.
- `release-create-github.yml` gates the GPG-import step on
  `sign.imports_gpg_key` instead of `sign.method == 'gpg'`;
  `release-publish-stage.yml` computes `sign-image` from `sign.signs_containers`
  instead of `sign.method != 'gpg'`. The workflows no longer hard-code method
  names — a new method's behaviour is decided once, in the plan. actionlint-clean.

Deliberately left: `publish-container.yml`'s internal `sign-method != 'gpg'`
guards read its own `sign-method` *input* (a method-typed parameter passed
down), not a config-plan string-branch, so they are a legitimate leaf-level
check rather than the magic-string branching Phase 5 targets.

Implemented (Phase 4b, registry auth federation):

- `internal/domain/provider/registryauth.go` — `RegistryAuth` +
  `RegistryAuthResolver` role, with `MatchesRegistry` (host-only,
  scheme-stripping, case-insensitive) so a runner token is only ever sent to the
  forge's own registry.
- Adapters: github (`ghcr.io` + `$GITHUB_ACTOR`/`$GITHUB_TOKEN`), gitlab
  (`$CI_REGISTRY` + `$CI_REGISTRY_USER`|gitlab-ci-token + `$CI_REGISTRY_PASSWORD`),
  forgejo (instance host + actor + token cascade). local omits it → fallback.
  Conformance vars added.
- `internal/cli/deps` — `RequireRegistryAuthResolver` + `RegistryAuthForDetected`.
- `container login` falls back to the forge's runner-injected credentials when
  no `--password`/`$REGISTRY_PASSWORD` is supplied **and** the login target is
  the forge's own registry; explicit `--registry-username`/`--registry-password-file` always win. This
  lets a GitHub/GitLab/Forgejo job push to its own registry with no separately
  managed secret.

This realises the "shrink standing secrets" goal for registry auth. The cloud-
KMS-over-OIDC half of 4b (federated KMS signing auth) remains future work; the
`slsa-attestor.yml` already takes operator KMS auth via `kms-auth-env` and nudges
toward OIDC there.

## Why

Today `release sign --method gpg|sigstore|kms` already works, and
`validate artifact-signature` already auto-detects `.asc` (gpg) vs `.bundle`
(cosign) and verifies accordingly. What is missing is not the engine but the
*defaults and ergonomics*:

- The default method is still `gpg` (`DefaultSignMethod = SignMethodGPG`), i.e. a
  long-lived private key imported onto the runner. Safer-by-default (keyless) is
  not the default.
- Verify identity (`--certificate-identity-regexp`, `--certificate-oidc-issuer`)
  is hand-written per call. A too-loose regexp still *passes* — a silent
  weakening.
- Git-object signing (`git tag -s`, `commit.gpgsign`) is GPG-only, so
  "no private keys on runners" is not fully reachable.

The `internal/domain/provider` **role pattern** (forge adapters implement
capability interfaces; the composition root resolves them via
`deps.requireRole[T]`; absent roles return an unsupported-role error) is the
seam this plan builds on. One new role does most of the work.

## What this gives us

- **Safe-by-default signing** — keyless (no key on the runner) wherever the forge
  exposes OIDC; gpg/kms remain available.
- **Verify that cannot silently be weak** — verification identity is derived from
  the same resolver that signed, so "verify as this repo on this forge" is
  zero-config and the identity regexp is anchored, not hand-typed.
- **True forge-agnosticism** — one `SigningIdentityResolver` role; GitHub /
  GitLab / Forgejo implement it, `local` falls back. Adding a forge stays one
  edit.
- **Fewer standing secrets** — KMS / registry auth can federate via OIDC instead
  of static secrets.
- **More coherent CLI / YAML** — method-agnostic workflows, no
  `if: sign.method == 'gpg'` branching, verify defaults that just work.

## What it does NOT change

- GPG `.asc` artifact signing stays (PGP-native ecosystems: Maven Central,
  apt/rpm).
- GPG-signed git tags/commits stay the **default for git objects** until an
  ssh/gitsign verify story is proven on every target forge.
- No existing `sign.method` config changes meaning. Every method keeps working on
  every supported forge. The `DefaultSignMethod` fallback constant is unchanged.

## Spine: decisions & invariants

1. **Two orthogonal axes stay separate** — `ForgeAPI` (forge API) and
   `RunnerKind` (runner conventions). Signing identity is a `ForgeAPI` concern.
2. **No central `switch forge`.** All forge variation is a provider role; absence
   of a role degrades (sigstore → gpg), never crashes.
3. **Dependency direction:** `domain/provider` must not import `domain/release`.
   Roles expose *capabilities* (`SupportsKeyless() bool`, identity strings); the
   release/config layer maps capability → `SignMethod`.
4. **Identity is derived, not typed.** Verify identity comes from the same
   resolver the signer used.
5. **Additive only.** New flags/inputs/roles; the `DefaultSignMethod` constant and
   its guard test remain — the *resolved* default moves, the fallback does not.
6. **A method requests only its own secret.** Keyless requests `id-token: write`
   and nothing else.

## New domain surface (`internal/domain/provider`)

```go
// signingidentity.go
type KeylessIdentity struct {
    OIDCIssuer    string // https://token.actions.githubusercontent.com
    TokenAudience string // requested id-token aud (default "sigstore")
    SubjectID     string // exact cert SAN: the workflow-ref URL
    SubjectRegexp string // anchored ^…$ form for verify
}

// SigningIdentityResolver — forges that can mint an OIDC token for Sigstore
// keyless. local / registry-less forges don't implement it, so
// deps.RequireSigningIdentityResolver returns the unsupported-role error and
// the caller falls back to gpg/kms.
type SigningIdentityResolver interface {
    SupportsKeyless() bool
    ResolveKeylessIdentity(ctx context.Context) (KeylessIdentity, error)
}

// RegistryAuthResolver — login / KMS-auth creds, OIDC-federated where
// supported. Sibling of the existing RunArtifactCreds policy.
type RegistryAuthResolver interface {
    ResolveRegistryAuth(ctx context.Context, registry string) (RegistryAuth, error)
}
```

`SubjectRegexp` is built by an exported, domain-pure `AnchorIdentity(repoURL)`
that emits `^<escaped repoURL>/` (anchored, metacharacters escaped). A golden
test pins its output per forge so a refactor cannot silently loosen verify.

## Adapters (`internal/adapters/{github,gitlab,forgejo,local}`)

| Forge   | Issuer                                   | `SupportsKeyless` |
| ------- | ---------------------------------------- | ----------------- |
| github  | `token.actions.githubusercontent.com`    | true              |
| gitlab  | `$CI_SERVER_URL`                          | true              |
| forgejo | instance OIDC (if advertised)            | true / false      |
| local   | — (role not implemented)                 | n/a → fallback    |

Each adapter carries a compile-time conformance var:
`var _ provider.SigningIdentityResolver = (*Adapter)(nil)`.

## Composition root (`internal/cli/deps`)

```go
func (d *Deps) RequireSigningIdentityResolver() (provider.SigningIdentityResolver, error) {
    return requireRole[provider.SigningIdentityResolver](d, "sigstore keyless signing identity")
}
func (d *Deps) RequireRegistryAuthResolver() (provider.RegistryAuthResolver, error) {
    return requireRole[provider.RegistryAuthResolver](d, "registry authentication")
}
```

## CLI consumption

- **`buildSigner`** (`cli/commands/release/artifacts.go`): for `--method sigstore`
  with no explicit `--oidc-issuer`, resolve it from the role. Falls back to
  today's cosign auto-detect when the flag is set or the role is absent. The
  `Signer` interface is unchanged — only construction inputs.
- **Verify** (`validate artifact-signature`, `container-signature`,
  `tag signature`): default `--cert-identity-regexp` / `--cert-oidc-issuer` from
  the same resolver when unset. Explicit flags always win (cross-repo verify
  escape hatch).
- **Default-method resolution** (config/plan layer, not the constant):
  `explicit sign.method → scaffolding default → (resolver.SupportsKeyless ? sigstore : DefaultSignMethod)`.

## Workflow & config

- Workflows opt into `id-token: write`; drop hand-wired `--oidc-issuer`/identity;
  collapse `if: sign.method == 'gpg'` to a minimal pre-step.
- `slsa-attestor.yml`: `egress-policy: audit` → `block` + allowlist when promoted
  from experimental.
- `examples/` greenfield templates default to `sigstore`; document `sign.method`
  precedence and the new `git.signing` dimension.
- `doctor`: report the resolved keyless identity so operators see what verifiers
  must match.

## Phases

| Phase                     | Scope                                                                 | Exit criterion                                                  |
| ------------------------- | --------------------------------------------------------------------- | -------------------------------------------------------------- |
| 0 Observability           | method+trust banner via `domain/summary/jobresult.go`; `doctor` shows identity | every run surfaces method + identity                  |
| 4a Identity role *(early)* ✅ | `SigningIdentityResolver` + adapters + `AnchorIdentity`            | resolver returns correct identity on github + gitlab — **done** |
| 1 Verify parity ✅          | verify defaults from resolver; per-method recipes in `verification.md` | verify defaults wired (artifact + container); recipes documented — **done** |
| 2 Greenfield default ✅     | recommendation + example emit sigstore (no fallback flip)             | new projects guided to keyless; existing configs untouched — **done** |
| 3 Git-object signing ✅(code) | opt-in `git-signing: gpg\|ssh` behind `gitSigningOps` (gitsign dropped) | producer + consumer + config + plan + full caller chain wired; **ssh release round-trip not yet exercised in CI** |
| 4b Registry/KMS OIDC ◐    | `RegistryAuthResolver`; OIDC > static                                  | registry login w/ zero static secret done (3 forges); **cloud-KMS-over-OIDC still future** |
| 5 Governance ✅(code)      | advisory lint (doctor); status labels; retire YAML branch points      | advisory + docs + plan-driven branch-point retirement done |

Dependency edges: **4a precedes 1**; 3 and 4b are independent and
parallelizable; 5 last.

## Test strategy

- Domain-pure unit tests for `AnchorIdentity` / `KeylessIdentity` validation
  (no I/O), like `domain/gpg` heuristics.
- **Golden files** for rendered identity regexps so any loosening shows as a
  review diff (as in `signer_cosign_test` and the provider render tests).
- App-layer **fakes** (`fakesigningidentity`, mirroring `fakejobresultstore` /
  `fakeoutputsink`) injected through the narrow role interface.
- Compile-time **conformance vars** per adapter.
- **smoke** (`just test-smoke`) sign→verify round-trip; add scenario IDs to
  `docs/cli-black-box.md`.

## No-loss contract

1. GPG `.asc` artifact signing stays.
2. GPG git-object signing stays the default until ssh/gitsign verify is proven on
   all target forges.
3. No existing `sign.method` config changes meaning; only the unset/greenfield
   default moves, and only where the resolver reports keyless support.
4. Every method works on every supported forge; identity always flows through the
   role.
5. A method requests only the secret it uses; no-OIDC forges silently fall back.

## Risks & mitigations

- **Loosened verify identity via refactor** → golden test on `SubjectRegexp`;
  anchoring is a domain function, not inline string-building.
- **Forge OIDC quirks (Forgejo maturity)** → `SupportsKeyless()` gate + graceful
  gpg fallback.
- **Dependency cycle (`provider` → `release`)** → capability-based roles +
  mapping in the release layer; enforce with an import-lint rule.
- **Phase 2 downstream breakage** → resolver-driven default keeps the guarded
  constant intact; dual invariant test.
