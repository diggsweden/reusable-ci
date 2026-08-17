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

5. **What the signer publishes about itself is a written choice.** Signing is
   not only a local act: cosign writes a Rekor entry to the *public* Sigstore
   transparency log for every signature it makes, on every method — not just
   keyless. The entry is a `hashedrekord` carrying the artifact's SHA-256, the
   signature, the public key, and an integration timestamp. It does not carry
   the artifact's contents or its filename, so what crosses the boundary
   outward is the existence and timing of a signing event, plus a fingerprint
   anyone who later obtains the artifact can use to confirm it is the one
   signed. A Rekor entry is permanent, public, and append-only: unlike every
   other rule here, a mistake cannot be corrected afterwards.

   The choice is `sign.transparency` (`internal/domain/release/transparency.go`),
   and the default is `public`. For keyless it is not really a choice — a
   ~10-minute Fulcio certificate needs a Rekor inclusion proof (or an RFC3161
   TSA timestamp, which this engine does not configure) or the signature
   becomes unverifiable — so `SignConfig.Validate` rejects
   `method=sigstore` + `transparency=none` rather than letting it fail later
   inside cosign. For `method=kms` it is a genuine trade: the key is
   long-lived, so the signature verifies indefinitely on its own, and the log
   adds public auditability and a trusted timestamp at the cost of publishing
   release metadata. Public is the default because transparency is the reason
   to adopt Sigstore at all, and because withholding the public record should
   be a decision someone wrote down, not the result of leaving a field blank.

   `transparency=none` is correct for artifacts that are not published —
   internal-only builds — and for test suites, which must never write to a
   permanent public log. It is the wrong setting for anything whose signatures
   are meant to be publicly verifiable; in cosign's own words, "artifacts
   cannot be publicly verified when not included in a log."

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
- Rule 5 is enforced in one place by construction: `cosign.New` /
  `cosign.NewIsolated` resolve `$REUSABLE_CI_COSIGN_TRANSPARENCY`, so a
  signing path cannot opt out by forgetting to thread a flag through one of
  the ~16 adapter construction sites — the failure mode of a missed one is
  silent publication, which is exactly the mistake that cannot be undone. A
  new cosign write verb inherits the setting for free; the count assertions in
  `internal/adapters/cosign/signingconfig_test.go` trip if one is added
  without being covered.
- One setting drives both halves of rule 5 deliberately. cosign couples them:
  a signature made with no transparency log cannot be verified without
  `--insecure-ignore-tlog`. Two independent knobs that must always agree would
  be a footgun, not a safeguard — set one without the other and cosign fails
  with "not enough verified log entries", naming none of its causes. Deriving
  both from `Transparency` makes the disagreement unrepresentable.
- Because a run's transparency choice decides whether it touches
  sigstore.dev at all, `sign.requires_sigstore_egress` is precomputed into the
  config plan. A caller's egress allowlist should be read from that rather
  than re-derived in prose: an allowlist that omits Rekor while the plan
  publishes to it fails the signing step hard, at release time.
- The isolated environment (`runtimeKeep`) carries the proxy configuration as
  well as the TLS trust roots. These are the two halves of one thing — how to
  trust the endpoint, and how to reach it — and keeping only the first left
  isolated signing broken on a proxy-only network. It broke *only* for `env://`
  keys, since those alone take the isolated path, so a file key would sign
  while an `env://` key did not; forgejo-ci signs with `--key env://COSIGN_KEY`.
  A proxy URL may embed credentials, so this set is no longer strictly
  non-secret. That is a deliberate trade: rule 1 bounds what a cosign
  compromise could *do*, and cosign already holds the signing key, so
  withholding the proxy config bought no containment — cosign must reach Rekor
  by design on the public path — while breaking the feature. A run that must
  touch no network at all is `transparency=none`, which removes the reason for
  a proxy rather than hiding it.
