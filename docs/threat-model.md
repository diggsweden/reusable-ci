# Threat model

This document names what reusable-ci defends against, what it deliberately
doesn't, and where the adopter's own controls have to layer on top.
Adopters evaluating reusable-ci for production use should read this to
understand where the trust boundaries sit. Maintainers should consult it
when assessing proposed changes — a "security improvement" that doesn't
close a real attack vector is over-engineering; one that does should be
reflected here.

## Trust boundaries

| Layer | Owned by | What can go wrong here |
|---|---|---|
| Caller workflow YAML | Adopter | Over-permissioned `permissions:`, over-scoped `secrets: inherit` |
| `.reusable-ci/artifacts.yml` | Adopter | Names with path traversal (validated), unsafe `working-directory` (validated), env-var refs (validated) |
| `pom.xml` / `build.gradle` / `Cargo.toml` / `package.json` / `Containerfile` | Adopter | Arbitrary code at build/publish time — **by design** (that's what `mvn package`, `gradle build`, `cargo build`, `npm pack`, `docker build` do) |
| `.reusable-ci/allowed_signers` / `allowed_gpg_keys.asc` | Adopter | Allowlist additions; only meaningful when combined with branch protection |
| Reusable workflow YAML | reusable-ci | Documented inputs/outputs; SHA-pinned third-party actions; every internal `workflow_call` site declares the secret names it needs explicitly (no blanket `secrets: inherit` between reusable workflows) |
| `reusable-ci` Go binary | reusable-ci | Argv-pinned tools (tests assert exact argv); env-only secret reads; reproducible builds |
| Runtime images | reusable-ci | Tag-pinned (`@vN.N.N`) by default — **see Known trust boundaries below** |
| Third-party GitHub Actions | upstream | SHA-pinned by reusable-ci's Renovate base config |
| Toolchain base images (`debian:13-slim` etc.) | upstream | Digest-pinned (`@sha256:…`); a local Go contract test enforces the pin and Renovate updates it |
| Forge run-artifact store (GitHub Actions artifacts / Forgejo Actions artifacts) | upstream (forge) | Carries build output between jobs. Every downloaded entry is gated for path traversal, symlinks, and size (`domainartifact.SafeJoin`). Where a hand-off crosses the build → sign boundary it is bound out of band, not left to the store: the producer emits `release dist-digest` as a job output and the signer re-checks with `release validate-dist --expected-digest`, so the expected value travels the forge control plane (forgejo-ci's `check-l3-isolation` asserts that channel; reusable-ci's own GitHub release signs in the build job, so it has no hand-off to bind). The store is trusted for availability and for any flow that does not bind. See [Artifact and image flows](flows.md#how-each-flow-binds-build-to-signature). |

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
| Docs/code drift | `TestDocsCLIReferenceInSync` (CLI surface); `cmd/reusable-ci/smoke_cli_contract_test.go` maps the black-box scenarios in `docs/cli-black-box.md` to real CLI behaviour |
| Timestamp non-determinism in artifacts | `SOURCE_DATE_EPOCH` baked from `git log -1 --format=%ct HEAD` for Go + Cargo binaries and security reports |
| Per-step secret over-scoping | Each publish step's `env:` block lists only the secrets that specific step needs; no step receives the union of its job's secrets |
| **Untrusted-trigger publish/release** — signing, package, and API secrets reaching a workflow run whose commit context is contributor-controlled | Two layers: (a) `pullrequest-orchestrator.yml` declares only `CODE_SCANNING_TOKEN` in its `workflow_call.secrets` block, so the publish-secret family is unreachable through the documented PR path; (b) every privileged publish / release workflow runs `reusable-ci validate event-context` as its first runtime step — refuses any trigger outside `{push, workflow_dispatch, release, schedule, workflow_run, merge_group}`, catching adopters who wire a direct caller under `pull_request*` by mistake. The opt-out (`--allowed-events` on the guard step) is per-call-site, never an env-var bypass. |

## What reusable-ci does NOT defend against

| Concern | Why not | Adopter mitigation |
|---|---|---|
| **Malicious build extensions** (pom.xml plugins, build.gradle tasks, Cargo `build.rs`, package.json scripts, Containerfile `RUN`) | These run with whatever env is set on their step — by design. `mvn deploy` invokes maven-deploy-plugin; if a malicious extension is on the classpath, it runs with the credentials in env. | Per-team / per-repo secrets (not org-wide); branch protection so unreviewed code doesn't reach the release branch; release-authorisation allowlist gate |
| **Multi-tenant secret reuse** | A shared org secret used by N teams' reusable-ci callers can be exfiltrated by any team's malicious build extension during a publish step. reusable-ci has no way to know "this caller is team A, that one is team B." | Scope secrets per-team / per-repo at the GitHub org-secrets level |
| **Compromised commit access** | A bad actor with push rights to the release branch can publish releases via the normal flow. The signature on the tag is also under their control. | Branch protection + required reviews + `require-allowlisted-signer: true` + `.reusable-ci/allowed_signers` with the maintainer fingerprints |
| **Runtime-image registry compromise** | `ghcr.io/diggsweden/reusable-ci-runtime-*:v3.0.0` is tag-pinned. A tag rewrite at ghcr.io would ship a different image. | Adopters override `runtime-image-*` inputs with `@sha256:…` digests when they need stricter posture |
| **Self-hosted runner abuse** | Adopters using their own runners inherit those runners' risks (host filesystem persistence, network position, container escape). reusable-ci can't see this layer. | Use GitHub-hosted runners where possible; isolate self-hosted runners per-repo |
| **Egress exfiltration during publish steps** | `harden-runner` is in `audit` mode by default on publish workflows that have variable network surface (mvn/gradle/xcode plugins reach unpredictable endpoints). | Set `egress-policy: block` + `egress-allowed-endpoints` per caller on the workflows that declare these inputs — grep `egress-policy` across `.github/workflows/` for the current set. |
| **GitHub Actions log secret masking bypass** | GHA masks secrets in logs by substring match. Base64-encoding or split-string echoing defeats this. | Same as malicious build extensions — limit who has code-write access; rotate any secret that was theoretically observable; per-team secret scoping |
| **`build-args` values leaked via the published image** | `publish-container.yml` passes `containers[].build-args` from artifacts.yml to the build (buildah `--build-arg`) verbatim. They surface in the image config / build records (e.g. `docker history`, any registry inspect). buildah emits no build provenance, so they are not written into a SLSA provenance attestation — but build-args remain unsuited for secrets. | **Never pass secrets via `containers[].build-args`.** reusable-ci ships `containers[].build-secrets` for this: declare the secret name in artifacts.yml, pack the value into the `REUSABLE_CI_BUILD_SECRETS_JSON` envelope secret at the top-level caller, and consume it in the Containerfile with `RUN --mount=type=secret,id=<lowercased-name> …`. buildah mounts the value on tmpfs and never records it. Full recipe in [docs/artifacts-reference.md](artifacts-reference.md#build-secrets). |
| **Error-message inference of allowlist contents** | `validate release-authorization` returns different exit codes / annotations depending on whether the tag signer is missing from the allowlist vs the allowlist file itself is missing vs the signature is malformed. A probing caller can narrow which keys are listed. | Limit who can trigger releases (branch protection). The allowlist file is repo-readable anyway — informational, not a credential. |

## Adopter responsibilities

These are the controls that have to live on the adopter's side. reusable-ci can document them but cannot enforce them across organizational boundaries.

1. **Branch protection** on the release branch (require PRs, require reviews, require signed commits if your org policy demands it)
2. **Per-team / per-repo secrets** instead of org-wide where possible — limits multi-tenant blast radius
3. **Review `.reusable-ci/allowed_signers`** and `allowed_gpg_keys.asc` like any other security policy file (the test for it is "would I be comfortable if this person could ship a release with my org's name on it")
4. **Audit the publish-workflow caller** for `secrets: inherit` scope. Internally, reusable-ci does not use `secrets: inherit` between reusable workflows — every call site names the secrets it forwards. The **top-level caller in the adopter's repo is still adopter-controlled**, though: a caller workflow that inherits everything will pass everything down.
5. **Override runtime-image to a digest** when stricter posture is required: `runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-go-1.26@sha256:…`
6. **Promote egress to `block`** on the workflows that declare `egress-policy` / `egress-allowed-endpoints` (grep `.github/workflows/`), where the network surface is bounded and you can enumerate the endpoints
7. **Rotate publish credentials periodically** — Maven Central tokens, npm tokens, etc. — independent of any specific incident; reduces the value of a compromised credential

## Frequently confused: what's an attack vs. a foot-gun

reusable-ci's validators reject some patterns that aren't really security threats but DO cause confusing failures deep in the pipeline:

- `working-directory: /srv/build` — rejected as absolute, but the failure mode without rejection is "build fails on a fresh runner because the path doesn't exist", not "attacker escapes the workspace"
- `binary-name: "../bad"` — rejected as path-traversal, but the failure mode without rejection is "compiled binary lands one directory up where upload-artifact doesn't find it", not "attacker writes /etc/passwd"
- `working-directory: ${WORKSPACE}/svc` — rejected as env-ref, but artifacts.yml doesn't perform shell expansion so the value would land as the literal string `${WORKSPACE}/svc` and a stat would fail. Not exfiltration; just a confusing error.

These rejections are **input hygiene**, not security boundaries. The runner is fresh per job; the workspace contains only the consumer's own files. A clear parse-time error pointing at the offending field is the value, not "we prevented an exploit."

## Open question: externalParameters reserved keys

`MergeExternalParameters` refuses a caller-declared key that collides with one
the engine computed, so a declared document cannot rewrite an attested fact.
The reserved set is positional: it is whatever keys are already in the map when
the merge runs, which depends on the profile.

- The default profile computes `source` and `ref`, so both are reserved.
- The forgejo-actions profile computes `workflow` instead, so under it `source`
  and `ref` are **not** reserved and a caller may declare them. The mirror case
  holds too: nothing computes `workflow` under the default profile.

Confirmed by probe, not inference: with the forgejo profile selected, extras of
`{"source": "git+https://example.invalid/attacker"}` are accepted and appear in
`buildDefinition.externalParameters.source` of the statement.

The blast radius is bounded. The authoritative source identity is
`resolvedDependencies[0]` — `git+<repo>@<ref>` with the `gitCommit` digest —
which is engine-computed under both profiles, and extras merge only into
`externalParameters`. A verifier following SLSA (check `builder.id`, then
`resolvedDependencies`) is unaffected. The exposure is a verifier or a human
reading `externalParameters.source` and taking it for an engine fact, which it
is under one profile and not the other.

**What settles the classification:** whether calling-repo configuration can
reach `--external-parameters-json`. If only the engine's own workflow writes
that flag, this is input hygiene of the kind described above. If repo-declared
config flows into it — the `base_input_set` and `build_group` examples suggest
it might — the guard is a real trust boundary and is applied inconsistently.

If it is a boundary, the cheap fix is to reserve a fixed set of names
independent of profile (`source`, `ref`, `workflow`, plus the container-side
`image`, `flavor`, `base_input_id`) while still emitting exactly what each
profile emits today. Output is unchanged, so existing verifiers see identical
documents; only a caller declaring one of those names under a profile that does
not compute it starts getting an error, which is the case worth rejecting.

Note that emitting `source` under the forgejo profile is not an option: its
absence is deliberate compatibility shaping for existing forgejo-ci verifiers,
pinned by `TestGenerateProvenance_ForgejoProfileNamesTheWorkflowNotAGenericSource`.

## Open question: `assemble-dist --path` is the least validated destructive flag

`release assemble-dist --prune-dirs` deletes every subdirectory of `--path`
with `os.RemoveAll`. That path is taken straight from the flag and validated
only by `validateSingleLineValue`, which rejects an empty value and embedded
newlines. It is not checked for traversal or absoluteness. The last-ditch guard
in `pruneAssembleDistDirs` refuses only the literal `.` and `/`.

Confirmed by probe, inside a temp directory: with the working directory at
`<tmp>/work` and `--path ../victim --prune-dirs`, `<tmp>/victim/precious` was
deleted. The command then failed on the empty tree, well after the removal.

The inconsistency is the argument. `--release-images-path`, which only *writes*
a file, is checked with `validateSafeRelativePath`. Transfer item paths, which
only decide where a download lands, are checked the same way in this very
function. `--path`, the one flag that deletes directories, is not.

Classifying it honestly: on a fresh runner where the consumer already executes
their own build code, this is not an escalation — they can remove files anyway.
It is a foot-gun of the destructive kind, in the sense used above: a typo'd
`--path ..` does not produce a confusing error deep in the pipeline, it removes
directories and then reports something unrelated about an empty hand-off. It
becomes more than that anywhere the flag could be set from less-trusted
configuration, or in a privileged context of the kind ADR 0002 describes.

The fix is one line — run `--path` through `validateSafeRelativePath` like its
two neighbours — and it would reject nothing any current caller passes, since
the engine's own workflows pass `dist/`.

## Open question: artifact transfer plan validation

`DownloadArtifacts` re-parses and re-validates the artifact transfer plan on the
far side of a process boundary, since the plan arrives as JSON on
`--artifact-transfer-plan-json`. That re-validation covers the item kind, the
name/name_template exclusivity and a non-empty path. Two things it does not do,
both confirmed by probe:

- **`path` is not checked for safety.** `"path": "../../../etc/"` and
  `"path": "/etc/"` are both accepted and handed to the downloader as the
  directory to extract into. This is not a judgement call the codebase has
  made once and applied: `AssembleDist` consumes the *same* transfer plan and
  does check, via `validateSafeRelativePath(item.Path, "artifact transfer
  path", true)`. Two consumers of one plan, one of which validates the field
  that decides where files land. The publish path and release notes are
  guarded by `safeRelativePath` too, and artifacts.yml rejects absolute and
  traversal paths as described above, so `DownloadArtifacts` is the outlier.
- **Validation is interleaved with fetching.** Items are validated inside the
  download loop, so a plan whose first item is valid and whose second is invalid
  downloads the first before failing. A refused plan can leave the workspace
  partly populated.

Severity is low as things stand: plans are engine-generated in
`internal/domain/pipeline/releaseplan.go` and `snapshotreleaseplan.go`, where
every path is a hardcoded constant (`./release-artifacts/`, `./sbom-artifacts/`,
`./release-artifacts/binaries/`). Nothing consumer-authored reaches these
fields today. This is defence in depth at a boundary that already re-validates
everything else, not a live hole.

Deciding either way is cheap. Validating the whole plan before the first fetch
makes a refusal mean nothing happened, and running `path` through the same
relative-path check as the rest of the package closes the gap with the
convention the codebase already follows. Both change behaviour only for plans
that no current producer emits.

## Reporting a security issue

See [SECURITY.md](../SECURITY.md) for the disclosure process. The threat
model above sets expectations for what's in scope: a report claiming
"a malicious pom.xml plugin can read MAVEN_CENTRAL_PASSWORD during
mvn deploy" is correctly classified as out-of-scope (documented in the
table above) and would be closed with a pointer here.

A report claiming "argv leakage during release sign" or "a non-control
path puts a secret in step summary output" would be in-scope and
treated with the usual disclosure timeline.
