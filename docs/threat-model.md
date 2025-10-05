# Threat model

This document names what reusable-ci defends against, what it deliberately
doesn't, and where the adopter's own controls have to layer on top.
Adopters evaluating reusable-ci for production use should read this to
understand where the trust boundaries sit. Maintainers should consult it
when assessing proposed changes. A "security improvement" that doesn't
close a real attack vector is over-engineering; one that does should be
reflected here.

## Trust boundaries

| Layer | Owned by | What can go wrong here |
|---|---|---|
| Caller workflow YAML | Adopter | Over-permissioned `permissions:`, over-scoped `secrets: inherit` |
| `.reusable-ci/artifacts.yml` | Adopter | Names with path traversal (validated), unsafe `working-directory` (validated), env-var refs (validated) |
| `pom.xml` / `build.gradle` / `Cargo.toml` / `package.json` / `Containerfile` | Adopter | Arbitrary code at build/publish time, **by design** (that's what `mvn package`, `gradle build`, `cargo build`, `npm pack`, `docker build` do) |
| `.reusable-ci/allowed_signers` / `allowed_gpg_keys.asc` | Adopter | Allowlist additions; only meaningful when combined with branch protection |
| Reusable workflow YAML | reusable-ci | Documented inputs/outputs; SHA-pinned third-party actions; every internal `workflow_call` site declares the secret names it needs explicitly (no blanket `secrets: inherit` between reusable workflows) |
| `reusable-ci` Go binary | reusable-ci | Argv-pinned tools (tests assert exact argv); env-only secret reads; reproducible builds |
| Runtime images | reusable-ci | Tag-pinned (`@vN.N.N`) by default; **see Known trust boundaries below** |
| Third-party GitHub Actions | upstream | SHA-pinned by reusable-ci's Renovate base config |
| Toolchain base images (`debian:13-slim` etc.) | upstream | Digest-pinned (`@sha256:…`); a local Go contract test enforces the pin and Renovate updates it |
| Forge run-artifact store (GitHub Actions artifacts / Forgejo Actions artifacts) | upstream (forge) | Carries build output between jobs. Every downloaded entry is gated for path traversal, symlinks, and size (`domainartifact.SafeJoin`). A cross-job build-to-sign hand-off must be bound out of band: the producer emits `release dist-digest` as a job output and the signer re-checks with `release validate-dist --expected-digest`, so the expected value travels the forge control plane. `validate isolation` checks selected static wiring and lexical hints, not runtime verification. The owner of the pinned signing workflow must enforce the trusted digest input, verifier failure gate and ordering before signing; Itiquette's release-ci workflow owner is one integration-specific example. The store is trusted for availability and for any flow that does not bind. See [Artifact and image flows](flows.md#how-each-flow-binds-build-to-signature). |

## What reusable-ci defends against

| Concern | Mechanism |
|---|---|
| Argv leakage of secrets | Env-only contract; tested via mockbinary argv-pinning + the swap-refusal test |
| Tampered third-party action versions | Renovate `pinDigests: true` on the `github-actions` manager (base config) |
| Tampered Containerfile base images | Renovate `pinDigests: true` on the `dockerfile` manager (local config) |
| Path traversal via `working-directory` | `validateWorkingDirectory` at config-parse time |
| Path traversal via artifact `name` / `binary-name` | `validateArtifactFileNames` at config-parse time |
| Env-var refs in artifacts.yml string fields | Rejected by `validateWorkingDirectory` (no silent literal-string fallback) |
| Silent CVE-laden container release | `publish-container.yml` Trivy scan + severity gate (fail on CRITICAL/HIGH by default) |
| Silent SBOM-missing release | All SBOM steps mandatory; explicit `enable-build-sbom: false` opt-out |
| Non-reproducible JVM / Cargo releases | `validate jvm-reproducibility` + `validate cargo` hard-fail at prerequisites stage |
| Stale-runner drift | `runs-on: ubuntu-24.04` pinned (not `ubuntu-latest`) |
| Workflow contract drift | `TestWorkflowInputContract` fails CI if a `with:` key doesn't match a declared `inputs:` |
| Plan/code drift | `TestTargetKeys_MatchStructTags` + `TestPlanContracts_Golden` |
| Docs/code drift | `TestDocsCLIReferenceInSync` covers the generated CLI surface. `cmd/reusable-ci/smoke_cli_contract_test.go` maps the binary-smoke scenarios, including `ART-01`–`ART-05`, to real CLI behaviour. `ART-06`–`ART-09` in `docs/cli-black-box.md` are manual traceability references to supporting domain/adapter/live evidence, not docs/code guards; those direct tests do not prove built-binary flag/env wiring, provider selection, or process exit-code wiring. |
| Timestamp non-determinism in artifacts | `SOURCE_DATE_EPOCH` baked from `git log -1 --format=%ct HEAD` for Go + Cargo binaries and security reports |
| Per-step secret over-scoping | Each publish step's `env:` block lists only the secrets that specific step needs; no step receives the union of its job's secrets |
| **Untrusted-trigger publish/release**: signing, package, and API secrets reaching a workflow run whose commit context is contributor-controlled | Two layers: (a) `pullrequest-orchestrator.yml` declares only `CODE_SCANNING_TOKEN` in its `workflow_call.secrets` block, so the publish-secret family is unreachable through the documented PR path; (b) every privileged publish / release workflow runs `reusable-ci validate event-context` as its first runtime step, which refuses any trigger outside `{push, workflow_dispatch, release, schedule, workflow_run, merge_group}`, catching adopters who wire a direct caller under `pull_request*` by mistake. The opt-out (`--allowed-events` on the guard step) is per-call-site, never an env-var bypass. |

## What reusable-ci does NOT defend against

| Concern | Why not | Adopter mitigation |
|---|---|---|
| **Malicious build extensions** (pom.xml plugins, build.gradle tasks, Cargo `build.rs`, package.json scripts, Containerfile `RUN`) | These run with whatever env is set on their step, by design. `mvn deploy` invokes maven-deploy-plugin; if a malicious extension is on the classpath, it runs with the credentials in env. | Per-team / per-repo secrets (not org-wide); branch protection so unreviewed code doesn't reach the release branch; release-authorisation allowlist gate |
| **Multi-tenant secret reuse** | A shared org secret used by N teams' reusable-ci callers can be exfiltrated by any team's malicious build extension during a publish step. reusable-ci has no way to know "this caller is team A, that one is team B." | Scope secrets per-team / per-repo at the GitHub org-secrets level |
| **Compromised commit access** | A bad actor with push rights to the release branch can publish releases via the normal flow. The signature on the tag is also under their control. | Branch protection + required reviews + `require-allowlisted-signer: true` + `.reusable-ci/allowed_signers` with the maintainer fingerprints |
| **Runtime-image registry compromise** | `ghcr.io/diggsweden/reusable-ci-runtime-*:v3.0.0` is tag-pinned. A tag rewrite at ghcr.io would ship a different image. | Adopters override `runtime-image-*` inputs with `@sha256:…` digests when they need stricter posture |
| **Caller-selected job container with write authority** | A `runtime-image` override becomes the job container, which starts before the first step. No step-level check can validate it, because the image is already what runs the steps. Most container jobs hold no durable write permission, but three do: `version-bump.yml/bump-version` and `release-create-github.yml/create-release` (`contents: write`, the latter also `id-token: write`) and `publish-snapshot-npm.yml/build-and-publish` (`packages: write`). An unreviewed image there can write to the repository, mint an OIDC token, or publish a package. This is not an escalation across a trust boundary — the caller already holds those permissions — but it is worth knowing before overriding the input. | Override `runtime-image` only with a reviewed reference, pinned by `@sha256:` digest. `TestCallerSelectedContainersWithWriteAuthorityAreDeclared` in `internal/workflowguard` fails if a fourth such job appears without being recorded here |
| **Checkout destination swapped after it was checked** | `reusable-ci platform checkout` guards its destination and then hands the path to Git, which takes a pathname rather than a directory descriptor. Anything able to write the parent directory can swap the path between the check and the write. Prevention is not available without Git accepting a descriptor. | The guarded directory's inode is recorded and re-checked before publication, so a run whose path was swapped refuses instead of publishing an unverified directory as the caller's checkout. This is detection, not prevention: work may already have been written into the substituted directory. It sees a rename or symlink swap — what a hostile replacement is — and not a delete-and-recreate that the filesystem happens to give the freed inode. A destination this run creates is also built in a sibling staging directory and renamed into place only on success |
| **Self-hosted runner abuse** | Adopters using their own runners inherit those runners' risks (host filesystem persistence, network position, container escape). reusable-ci can't see this layer. | Use GitHub-hosted runners where possible; isolate self-hosted runners per-repo |
| **Egress exfiltration during publish steps** | `harden-runner` is in `audit` mode by default on publish workflows that have variable network surface (mvn/gradle/xcode plugins reach unpredictable endpoints). | Set `egress-policy: block` + `egress-allowed-endpoints` per caller on the workflows that declare these inputs; grep `egress-policy` across `.github/workflows/` for the current set. |
| **GitHub Actions log secret masking bypass** | GHA masks secrets in logs by substring match. Base64-encoding or split-string echoing defeats this. | Same as malicious build extensions: limit who has code-write access; rotate any secret that was theoretically observable; per-team secret scoping |
| **`build-args` values leaked via the published image** | `publish-container.yml` passes `containers[].build-args` from artifacts.yml to the build (buildah `--build-arg`) verbatim. They surface in the image config / build records (e.g. `docker history`, any registry inspect). buildah emits no build provenance, so they are not written into a SLSA provenance attestation, but build-args remain unsuited for secrets. | **Never pass secrets via `containers[].build-args`.** reusable-ci ships `containers[].build-secrets` for this: declare the secret name in artifacts.yml, pack the value into the `REUSABLE_CI_BUILD_SECRETS_JSON` envelope secret at the top-level caller, and consume it in the Containerfile with `RUN --mount=type=secret,id=<lowercased-name> …`. buildah mounts the value on tmpfs and never records it. Full recipe in [docs/artifacts-reference.md](artifacts-reference.md#build-secrets). |
| **Error-message inference of allowlist contents** | `validate release-authorization` returns different exit codes / annotations depending on whether the tag signer is missing from the allowlist vs the allowlist file itself is missing vs the signature is malformed. A probing caller can narrow which keys are listed. | Limit who can trigger releases (branch protection). The allowlist file is repo-readable anyway, so it is informational, not a credential. |

## Adopter responsibilities

These are the controls that have to live on the adopter's side. reusable-ci can document them but cannot enforce them across organizational boundaries.

1. **Branch protection** on the release branch (require PRs, require reviews, require signed commits if your org policy demands it)
2. **Per-team / per-repo secrets** instead of org-wide where possible; limits multi-tenant blast radius
3. **Review `.reusable-ci/allowed_signers`** and `allowed_gpg_keys.asc` like any other security policy file (the test for it is "would I be comfortable if this person could ship a release with my org's name on it")
4. **Audit the publish-workflow caller** for `secrets: inherit` scope. Internally, reusable-ci does not use `secrets: inherit` between reusable workflows; every call site names the secrets it forwards. The **top-level caller in the adopter's repo is still adopter-controlled**, though: a caller workflow that inherits everything will pass everything down.
5. **Override runtime-image to a digest** when stricter posture is required: `runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-go-1.26@sha256:…`
6. **Promote egress to `block`** on the workflows that declare `egress-policy` / `egress-allowed-endpoints` (grep `.github/workflows/`), where the network surface is bounded and you can enumerate the endpoints
7. **Rotate publish credentials periodically** (Maven Central tokens, npm tokens, and so on) independent of any specific incident; reduces the value of a compromised credential

## Frequently confused: what's an attack vs. a foot-gun

reusable-ci's validators reject some patterns that aren't really security threats but DO cause confusing failures deep in the pipeline:

- `working-directory: /srv/build`: rejected as absolute, but the failure mode without rejection is "build fails on a fresh runner because the path doesn't exist", not "attacker escapes the workspace"
- `binary-name: "../bad"`: rejected as path-traversal, but the failure mode without rejection is "compiled binary lands one directory up where upload-artifact doesn't find it", not "attacker writes /etc/passwd"
- `working-directory: ${WORKSPACE}/svc`: rejected as env-ref, but artifacts.yml doesn't perform shell expansion so the value would land as the literal string `${WORKSPACE}/svc` and a stat would fail. Not exfiltration; just a confusing error.

These rejections are **input hygiene**, not security boundaries. The runner is fresh per job; the workspace contains only the consumer's own files. A clear parse-time error pointing at the offending field is the value, not "we prevented an exploit."

## Attested parameter ownership

Caller-declared provenance extras cannot use engine-owned names. `source`,
`ref`, `workflow`, `image`, `flavor`, and `base_input_id` remain reserved even
when the selected profile omits one of them, so changing profiles cannot turn an
engine fact into caller-controlled data. Existing predicate fields are reserved
as well.

## OCI release identity

OCI release identity matching requires non-empty revision, version, ref name,
and source expectations before comparing image labels. An unlabelled image
therefore cannot match an empty or incomplete expected identity, even when the
caller bypasses CLI-required flags and invokes the application API directly.

## Reporting a security issue

See [SECURITY.md](../SECURITY.md) for the disclosure process. The threat
model above sets expectations for what's in scope: a report claiming
"a malicious pom.xml plugin can read MAVEN_CENTRAL_PASSWORD during
mvn deploy" is correctly classified as out-of-scope (documented in the
table above) and would be closed with a pointer here.

A report claiming "argv leakage during release sign" or "a non-control
path puts a secret in step summary output" would be in-scope and
treated with the usual disclosure timeline.
