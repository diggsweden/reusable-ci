<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# ADR 0002: The signer trust boundary

Status: Proposed (2026-07-07)

## Context

`container ledger sign` runs inside an SLSA-L3 signing boundary (itiquette's
locked signer image on Forgejo; the equivalent privileged job on other
forges). Everything upstream of it is less trusted: build jobs run
consumer-supplied Containerfiles and build extensions, and a consumer may
own its own policy CLI (nanolinter's Rust `nanolinter-ci`, coherencetake
Phase 7.2) that *produces* the image ledger the signer consumes.

The central invariant of the whole system is therefore:

> **Consumer-supplied code or data can never cause code execution, nor scope
> escalation, inside the locked signer.**

Today this invariant is real and enforced in code, but it is documented only
in prose scattered across `docs/threat-model.md`, `docs/verification.md`, and
`docs/signing-convergence.md`. It is the single most important architectural
rule in the codebase and has no ADR. It is also the rule that decides which
verbs may move to a consumer's CLI and which must stay in this binary — so it
needs to be citable, not reconstructed.

## Decision

Record the boundary as a first-class invariant with four concrete rules, each
already enforced:

1. **The signer executes no consumer-supplied script.** It consumes only
   *data* — a JSON ledger plus a predicate file plus premade SBOMs — and runs
   only pinned tools (cosign, syft). There is no code path where a ledger
   entry or predicate field is executed.

2. **The signer re-validates every rule at the boundary.** It does not trust
   the build side's validation. `imageledger.Validate` runs signer-side so "a
   build job cannot smuggle an out-of-scope tag past signing"
   (`internal/domain/imageledger/imageledger.go` package doc).

3. **Refs and tags are confined to an expected repository.**
   `imageledger.ValidateEntryRepository` and `ValidatePromotionRecordRepository`
   (`internal/domain/imageledger/repository_constraints.go`) pin every entry
   ref/tag to an exact repository, wired from `--expected-image-repository` /
   `--expected-base-repository` on the sign verb. The rollback journal is
   re-checked as "a signer-boundary artifact."

4. **Provenance extras can only add, never override.**
   `provenance.MergeExternalParameters`
   (`internal/domain/provenance/externalparams.go`) fails with `ErrValidation`
   on any collision with a computed key, and `ParseExternalParametersJSON`
   rejects anything that is not a JSON object of parameters. A caller cannot
   overwrite an engine-computed fact (digest, source, base_input_id) in the
   attested predicate.

**Corollary — the Phase 7.2 port boundary.** A consumer CLI (nanolinter-ci)
may compute and emit the ledger, because the ledger is *data* that the signer
re-validates and confines under rules 2–4. But no signer-side verb —
`ledger sign`, promotion, the rollback journal — may move out of this pinned
binary, because moving it would move code across the boundary. This is why
`collect` (pure ledger production, no signing) was portable while `ledger
sign` (still Go, in the signer) is not.

## Consequences

- Any future "move this verb to the consumer" proposal is checked against
  rule 1: if the verb runs *inside* the signer, it stays; if it only produces
  data the signer re-validates, it may move.
- The `--expected-*-repository` flags are not optional hardening; they are the
  rule-3 enforcement point and must be set by every signer workflow.
- Changing `MergeExternalParameters` from fail-on-collision to
  last-writer-wins would silently break rule 4 and must never happen without
  superseding this ADR.
- A drift-guard (or at minimum this ADR referenced from the sign command's
  doc string) should keep the four enforcement sites from being weakened
  independently.
