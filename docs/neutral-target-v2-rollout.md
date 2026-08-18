# Neutral Target Contract V2 Rollout

This document tracks the coordinated hard break from the Forge Lab neutral
target contract version 1 to version 2. The rollout covers Forge Lab,
reusable-ci, forge-tidy, forge-sync, and release-ci.

The plan is operational state, not a compatibility promise. Checkboxes are
updated and committed as milestones complete. No rollout commit is pushed by
this work.

## Status

- [x] Confirm the five-repository dependency boundary.
- [x] Select a hard break instead of dual active v1/v2 support.
- [x] Preserve unrelated worktree changes rather than resetting them.
- [x] Record the rollout plan before implementation.
- [x] Freeze the authoritative v2 schema and fixtures.
- [ ] Implement every consumer against the frozen fixture.
- [x] Replace Forge Lab v1 emission with v2 emission.
- [ ] Pass all provider-free verification gates.
- [ ] Complete guarded live verification with disposable Forge Lab state.
- [ ] Record final commits and residual risks.

## Repositories And Baselines

| Role | Repository | Starting branch | Starting commit |
| --- | --- | --- | --- |
| Producer | `codefloe/itiquette/forge-lab` | `live-harness-convergence` | `867c735` |
| OCI/Fulcio consumer | `diggsweden/r3/reusable-ci` | `test/rename-e2e-tag-to-smoke` | `a0d30c22` |
| Strict Go consumer | `codefloe/itiquette/forge-tidy` | `test/tagged-tier-compile-check` | `090ab10` |
| Strict Go consumer | `codefloe/itiquette/forge-sync` | `test/tagged-tier-compile-check` | `daa2a77` |
| Strict shell consumer | `codefloe/itiquette/release-ci` | `main` | `5f46d67` |

Existing uncommitted changes are part of the working baseline but must not be
silently folded into unrelated commits. In particular:

- Forge Lab has an in-progress generation-bound cleanup-runtime hardening.
- release-ci has in-progress release-token and live-cleanup fixes.
- reusable-ci has an unrelated live-validation test improvement.

## Hard-Break Policy

- [x] Forge Lab will emit only neutral target contract version 2.
- [x] There will be no contract-version selector and no new v1 emission.
- [x] Active producer and consumer parsers will reject v1.
- [x] Existing v1 generations remain cleanable only through their advertised,
  generation-bound frozen cleanup commands.
- [x] A v1 destination cannot be replaced in place by the v2 emitter. It must
  be revoked first.
- [x] Recovery schema version 2 remains unchanged and independent.
- [x] The `shape-v2` producer protocol remains unchanged and independent.
- [x] Historical evidence readers may retain old profile values; this does not
  make v1 an accepted live target contract.

## Authoritative V2 Wire Contract

The v2 document keeps the existing generation, CA, endpoint, credential, and
interface lifecycle fields. It changes the root version to `2` and adds an OCI
registry object to every endpoint plus a root Fulcio object.

```json
{
  "version": 2,
  "generation": {
    "id": "run-example"
  },
  "ca_file": "/absolute/forge-lab-ca.crt",
  "endpoints": [
    {
      "name": "forgejo",
      "kind": "forgejo",
      "web_base_url": "https://forgejo.compose.forgelab:8443",
      "api_base_url": "https://forgejo.compose.forgelab:8443/api/v1",
      "git_base_url": "https://forgejo.compose.forgelab:8443",
      "oci_registry": {
        "base_url": "https://forgejo.compose.forgelab:8443",
        "credential": "endpoint"
      },
      "capabilities": [
        "artifacts",
        "packages",
        "repositories"
      ],
      "credential": {}
    }
  ],
  "fulcio": {
    "base_url": "https://fulcio.compose.forgelab:8443",
    "issuers": [
      {
        "endpoint": "forgejo",
        "oidc_issuer": "https://forgejo.compose.forgelab:8443"
      }
    ]
  },
  "interfaces": {}
}
```

### Field Rules

- [x] `version` is the JSON number `2`; every other value is rejected.
- [x] `oci_registry` is required on every endpoint and is either `null` or an
  exact object containing `base_url` and `credential`.
- [x] `oci_registry.base_url` is an HTTPS origin with no user information,
  path, query, or fragment.
- [x] `oci_registry.credential` is the literal `endpoint`, which promises that
  the endpoint credential is valid for the registry.
- [x] A selected Forge Lab endpoint without proven registry scope fails target
  emission; `null` is reserved for producers that make no registry promise.
  Capabilities do not imply registry authentication.
- [x] `fulcio` is a required root key and is either `null` or an exact object
  containing `base_url` and `issuers`.
- [x] `fulcio.base_url` is an HTTPS origin trusted through `ca_file`.
- [x] Every Fulcio issuer has one selected endpoint name and one exact HTTPS
  OIDC issuer URL.
- [x] Fulcio endpoint references are unique and resolve to entries in
  `endpoints`.
- [x] OIDC issuer URLs may have paths but have no user information, query, or
  fragment.
- [x] Unknown root and nested keys are rejected.
- [x] Duplicate semantic identities, trailing JSON, unsafe files, and documents
  over the existing size limit are rejected.
- [x] `ca_file` is transport trust for lab HTTPS endpoints. It is not a
  Sigstore signing root and does not imply Rekor availability.

### Provider Mapping

| Kind | OCI registry | Credential expectation |
| --- | --- | --- |
| Forgejo | Forge web origin | Endpoint token with package scope |
| Gitea | Forge web origin | Endpoint token with package scope |
| GitLab | Configured registry origin | Endpoint PAT with registry scope |
| GitHub | `https://ghcr.io` | Endpoint token only when Packages scope is proven |

Fulcio advertises only issuers both configured by Forge Lab and represented by
selected endpoints. Reachability is verified with the emitted CA before a
non-null Fulcio object is published.

## Phase 1: Forge Lab Contract Authority

- [x] Finish and preserve the in-progress cleanup-runtime hardening.
- [x] Correct cleanup ordering so evidence is not removed after its runtime.
- [x] Make delegated cleanup use the advertised frozen launcher.
- [x] Set `LAB_NEUTRAL_TARGETS_SCHEMA_CURRENT=2`.
- [x] Replace the active v1 validator with an exact v2 validator.
- [x] Add one canonical OCI registry topology helper shared by target emission
  and container fixture production.
- [x] Emit exact Fulcio endpoint and issuer facts.
- [x] Keep recovery-v2 evidence free of OCI, Fulcio, and credential bytes.
- [x] Refuse in-place replacement of a v1 destination with actionable cleanup
  guidance.
- [x] Keep shape context schema 1 explicit and prevent neutral-v2 fields from
  entering the `shape-v2` producer input accidentally.
- [x] Replace v1 canonical fixtures with v2 Compose and GitHub fixtures.
- [x] Add exact malformed-v2, cleanup, rollback, and frozen-runtime tests.
- [x] Update public contract, reference, development, explanation, how-to,
  README, and agent documentation.
- [x] Run `just verify-pr` and focused shell syntax checks.
- [x] Commit the Forge Lab cleanup hardening separately if it remains a
  coherent pre-v2 fix.
- [x] Commit the Forge Lab v2 hard break.

## Phase 2: reusable-ci Consumer

The current reusable-ci live harness is incompatible even with the evidenced
Forge Lab v1 shape. V2 replaces that integration directly rather than first
adding a temporary v1 compatibility layer.

- [x] Replace the reduced contract fixture with the canonical Forge Lab v2
  fixtures.
- [x] Model capabilities, object-form cleanup, OCI registry, and Fulcio.
- [x] Keep exact unknown-field rejection and add duplicate-key rejection.
- [x] Reject v1 explicitly.
- [x] Select live forges from the parsed target contract instead of
  `LAB_TARGETS`.
- [x] Permit multiple endpoint entries of one kind and require an exact
  `RC_LIVE_<FORGE>_ENDPOINT` selector when kind-only selection is ambiguous.
- [x] Use parsed `ca_file` instead of `LAB_CA_FILE`.
- [x] Use declared OCI origins instead of deriving registry hostnames.
- [x] Use declared Fulcio URL and exact issuer mappings instead of
  `LAB_FULCIO_URL` and `LAB_FULCIO_ISSUERS`.
- [x] Keep runner availability as a separate, documented operator input.
- [x] Require `workflow-runs` only for scenarios that request an in-runner
  workflow, not for host-side target acceptance.
- [x] Independently constrain registry and Fulcio authorities before sending
  credentials or OIDC tokens.
- [x] Stop treating malformed target contracts as unselected targets.
- [x] Validate and freeze the contract, CA, and cleanup launcher bytes before
  builds or provider mutation, while retaining the producer's exact recovery
  path.
- [x] Descriptor-pin the frozen launcher and revalidate the original recovery
  file immediately before cleanup.
- [x] Publish a separate static loopback CONNECT proxy for keyless runner jobs
  and constrain actual OIDC token use to the mapped Fulcio authority.
- [x] Keep generated registry and keyless failure diagnostics free of
  credential values and auth-file contents.
- [x] Add shell-entrypoint tests covering preflight and cleanup traps.
- [x] Update live-testing documentation.
- [x] Run ordinary unit tests plus smoke-tag compile/tests.
- [x] Commit reusable-ci v2 support without staging unrelated worktree edits.

## Phase 3: forge-tidy Consumer

- [x] Vendor canonical v2 fixtures.
- [x] Replace `CurrentVersion=1` with a strict v2 wire decoder.
- [x] Validate OCI and Fulcio globally, then project only fields the harness
  uses.
- [x] Keep `shape-v2` context at its independent schema version 1 through an
  explicit DTO.
- [x] Ensure extended cleanup parses v2 and executes once after every armed
  failure path.
- [x] Emit `extended-neutral-v2` and `portable-neutral-v2` evidence profiles.
- [x] Keep historical evidence profiles readable.
- [x] Add v1 rejection, malformed-v2, shape, cleanup, and recovery tests.
- [x] Update live-target documentation and changelog.
- [x] Run unit tests and compile-only black-box/live tagged harnesses.
- [x] Commit forge-tidy v2 support.

## Phase 4: forge-sync Consumer

- [x] Vendor canonical v2 fixtures.
- [x] Replace the strict v1 decoder with a strict v2 decoder.
- [x] Validate OCI and Fulcio globally and deliberately omit them from the
  normalized sync topology.
- [x] Preserve evidence profiles `existing` and `prepare`.
- [x] Preserve the independent outer credential-cleanup wrapper.
- [x] Add v1 rejection, malformed-v2, projection, wrapper-cleanup, and retained
  journal tests.
- [x] Update live-target documentation and changelog.
- [x] Run offline unit/race checks and compile-only tagged harnesses.
- [x] Commit forge-sync v2 support.

## Phase 5: release-ci Consumer

- [x] Preserve and separately commit the existing token and live-cleanup fixes.
- [ ] Add a strict neutral-v2 shell parser without weakening duplicate-key or
  exact-key checks.
- [ ] Parse and validate Fulcio but do not use it for the current KMS signing
  scenarios.
- [ ] Replace road-derived signer registry assumptions with the declared OCI
  registry.
- [ ] Bind the canonical OCI authority into live identity and start evidence.
- [ ] Preserve cleanup command/file freezing and same-generation recovery.
- [ ] Add v1 rejection, malformed-v2, OCI image binding, cleanup, and retained
  claim tests.
- [ ] Update usage, development documentation, runbooks, test plan, and
  changelog.
- [ ] Do not modify production workflows solely for neutral-v2 support.
- [ ] Do not re-vendor `bin/reusable-ci` for this test-only parser change.
- [ ] Run `bash .forgejo/scripts/test-static.sh`.
- [ ] Commit release-ci v2 support without absorbing unrelated edits.

## Phase 6: Provider-Free Cross-Repository Gate

- [ ] Confirm every consumer fixture is byte-for-byte copied from the
  authoritative Forge Lab fixture or records its source hash.
- [ ] Confirm every active parser accepts v2 and rejects v1.
- [ ] Confirm unknown and duplicate keys fail at every nested boundary.
- [ ] Confirm no consumer derives OCI or Fulcio endpoints from forge hostnames.
- [ ] Confirm no consumer reads retired `LAB_TARGETS`, `LAB_CA_FILE`,
  `LAB_FULCIO_URL`, or `LAB_FULCIO_ISSUERS` inputs.
- [ ] Confirm no normal neutral-v1 wording remains in active documentation.
- [ ] Confirm recovery-v2, shape-v2, evidence versions, claim versions, and
  journal versions were not mechanically changed.
- [ ] Run REUSE checks in all changed repositories.
- [ ] Record exact commands and results below.

## Phase 7: Guarded Live Cutover

This phase requires explicit disposable-lab authorization and is not implied by
permission to edit or run provider-free tests.

- [ ] Freeze new v1 target generation.
- [ ] Inventory current and retired v1 target generations and consumer claims.
- [ ] Invoke each v1 generation's advertised frozen cleanup command.
- [ ] Verify no v1 credentials, claims, recovery evidence, or cleanup runtimes
  remain.
- [ ] Emit one fresh v2 generation.
- [ ] Run forge-tidy extended and portable scenarios.
- [ ] Run forge-sync existing, prepare, cleanup, and recovery scenarios.
- [ ] Run all release-ci guarded Compose and k3s scenarios.
- [ ] Run reusable-ci OCI and keyless Fulcio scenarios.
- [ ] Verify provider resources, OCI artifacts, credentials, recovery evidence,
  and cleanup runtimes are absent after each run.

## Rollback

There is no active v2-to-v1 parser fallback.

1. Stop new live runs and target emission.
2. Invoke every v2 generation's advertised frozen cleanup command.
3. Verify all v2 credentials, resources, evidence, and runtimes are gone.
4. Roll back Forge Lab and all consumers as one coordinated set.
5. Use the old Forge Lab release to mint fresh v1 generations if required.

Source rollback must never precede v2 credential cleanup. The frozen v2 cleanup
runtime is the supported bridge when current source can no longer parse a live
generation.

## Verification Record

| Repository | Command | Result |
| --- | --- | --- |
| Forge Lab | `just verify-pr`; docs lint; REUSE; focused contract/runtime tests | Pass |
| reusable-ci | Unit/race tests; tagged vet compiles; smoke tests; Bash/ShellCheck; REUSE; diff check | Pass |
| forge-tidy | `just test-race`; Go lint; REUSE; tagged compile-only checks | Pass |
| forge-sync | Offline unit/race; integration; fuzz seeds; REUSE; tagged compile-only checks | Pass |
| release-ci | Pending | Pending |

## Commit Record

| Repository | Milestone | Commit |
| --- | --- | --- |
| reusable-ci | Rollout plan | `4457bd7d` |
| Forge Lab | Cleanup-runtime hardening | `5ab1238` |
| Forge Lab | Neutral-v2 producer | `17c95fe` |
| reusable-ci | Neutral-v2 consumer | `5aaab5df` |
| forge-tidy | Neutral-v2 consumer | `4e293e3` |
| forge-sync | Neutral-v2 consumer | `0ac79bb` |
| release-ci | Token and live-cleanup fixes | `b4474e3`, `c97f1d6` |
| release-ci | Neutral-v2 consumer | Pending |
| reusable-ci | Final rollout status | Pending |

## Residual Risks

- A hard break requires a coordinated maintenance window because consumers and
  producer cannot be independently deployed without a temporary live-test
  outage.
- Registry credentials and Fulcio OIDC tokens are sent to contract-declared
  authorities. Every operational consumer must retain an independent disposable
  lab host boundary.
- Provider-free tests cannot prove real token scopes, OCI cleanup, Fulcio
  issuance, or source-rollback cleanup. Those claims require the guarded live
  phase.
- Existing uncommitted work must remain reviewable and must not be hidden inside
  broad v2 commits.
