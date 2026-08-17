# ADR 0003: The CLI verb lexicon

Status: Accepted (2026-07-09; §1 and §3 revised 2026-07-14)

Symptom 1 is done: `release verify-*` → `release validate-*` and the
`{base,release}-images` / `ledger verify-digests` renames landed. Symptoms 2–4
are structural and remain follow-up work. Enforcement is code review, not a
guard test (§3).

## Context

`reusable-ci` exposes ~17 top-level groups (`artifact`, `build`, `config`,
`container`, `doctor`, `lint`, `plan`, `platform`, `publish`, `release`,
`report`, `sbom`, `security`, `toolchain`, `validate`, `version`) and well
over a hundred subcommands. The root help already separates
"COMMANDS TO RUN DIRECTLY" from workflow-internal ones, and a generated,
sync-gated reference (`docs/cli-reference.md`) keeps the surface documented.

What is *not* written down anywhere is the verb vocabulary. Each command
author picked a plausible word, and without a contract the words drifted.
Four concrete symptoms:

1. **`validate` vs `verify` are synonyms split by group.** The `validate`
   group checks aspects (`validate tag`, `validate container-signature`,
   `validate format`); the `release` group checks the same *kind* of thing
   with a different verb (`verify-tag`, `verify-request`, `verify-changelog`,
   `verify-dist`), plus `container ledger verify-digests`. A user cannot
   predict whether "check that X is trustworthy" is `validate` or `verify`.

2. **`publish` mixes altitudes.** The `publish` group holds both actual
   uploads (`forge-packages`, `google-play`, `appstore`) and pre-flight
   credential/artifact checks — while the `validate` group *already* owns
   credential checks (`validate maven-central`, `validate has-maven-central`).
   The pre-flight turf is split across two groups under two verbs.

3. **`version` is a verb grab-bag.** Verb-noun (`tag-release`,
   `derive-release`, `generate-snapshot`, `commit-push`), noun-only
   (`file-pattern`), and an over-compound (`commit-changelog-release`) that
   overlaps `tag-release`.

4. **`container` triplicates promotion verbs.** Promotion/rollback logic
   appears under `container release-images`, `container ledger`, and
   `container base-images` with overlapping verbs, and low-level plumbing
   (`write-digest-marker`, `suffix-extracted-binaries`) sits in the same
   namespace as high-level orchestration, with nothing in the names to signal
   altitude.

None of these break usability, but they make the surface harder to predict
than the clean root suggests, and — critically — every new command re-opens
the same coin-flip. A naive round of renames would not fix that on its own:
without a written rule, the vocabulary re-diverges on the next command.

The first draft added "and a build-time check", by analogy with the guard-test
family that single-sources regexes and env names. §3 records why that analogy
was dropped: those guards pin a fact with one correct value, where verb choice
is a judgement with a long tail of legitimate answers.

## Decision

Write the verb vocabulary down as a rule, apply it in review, and rename to
match.

### 1. The reserved verb set

These nine concepts have **one canonical spelling each**. A command performing
one of them MUST use the sanctioned word rather than a synonym. Commands
performing an action *outside* this set choose their own verb freely — the set
is a reserved lexicon, not an exhaustive vocabulary:

| Verb | Meaning | Side effects |
|---|---|---|
| `validate` | Assert an input/state satisfies a rule; fail closed otherwise. Subsumes today's `verify-*`. | Read-only |
| `plan` | Compute a typed plan/JSON from inputs. | Read-only |
| `build` | Produce an artifact (binary, image, SBOM). | Writes artifacts |
| `sign` | Attach a cryptographic signature. | Writes signatures |
| `attest` | Emit/attach an attestation (provenance, SBOM lineage). | Writes attestations |
| `promote` | Move a verified digest to a new tag/stage. | Mutates registry tags |
| `rollback` | Undo a promotion. | Mutates registry tags |
| `publish` | Upload a finished artifact to an external registry/store. | Outbound upload |
| `report` | Render a human/summary view of existing state. | Read-only (writes summary sinks) |

`verify` is **retired as a top-level verb.** "Verify a signature/digest" is a
`validate` (assert-a-rule) operation; the cryptographic nature is the noun,
not the verb (`validate container-signature`, not `verify signature`).

An earlier draft of this section read "Every command's leading verb MUST be one
of", which was an over-reach: measured against the live tree, a literal
nine-verb allowlist rejects **166 of 201** leaf commands, among them
`container login`, `artifact upload`, `version bump`, `build go run` and
`container ledger add` — all good names. It also contradicted this ADR's own
Consequences, which predict only the `verify-*` churn. The reserved-lexicon
reading above is what was actually decided and what was actually implemented.

### 2. The altitude rule

- **Checks live under `validate`.** Any pre-flight/credential/artifact check
  is a `validate` subcommand, never a sibling of an upload under `publish`.
- **`publish` is uploads only.** After this ADR, `publish <target>` performs
  the upload; its pre-flight checks move under `validate` (or become an
  explicit `validate publish-<target>`).
- **Low-level primitives are marked.** Within a mega-group like `container`,
  plumbing subcommands that are only ever invoked by the workflows (not by an
  operator) are grouped under an explicit low-level category/subtree, so the
  tree itself signals altitude. `container ledger *` is the primitive layer;
  `container release-images *` / `base-images *` is the orchestration layer —
  the docs and categories must say so.

### 3. Enforcement: code review, not a guard test

**Decided 2026-07-14, reversing this ADR's first draft.** The verb lexicon is
enforced in code review. There is deliberately **no** `*_guard_test.go` for it.

The first draft called for a guard reflecting over the command tree, on the
reasoning that a build-time invariant beats prose nobody re-reads. That reasoning
holds for the rest of the guard family and does not hold here:

- **An allowlist is not implementable.** The reserved set covers nine concepts,
  not the ~35–40 legitimate actions the CLI performs (§1).
- **A synonym denylist bans good names.** The obvious candidates collide with
  domain-correct spellings: outlawing `deploy` as a synonym for `publish` would
  reject `publish maven-central deploy` and `publish forge-packages deploy`,
  which wrap `mvn deploy` and are named correctly.
- **A guard for `verify` alone earns little.** It never caught anything (it was
  written after the renames), and the tree now carries ~24 `validate*` commands
  as visible precedent. The pattern teaches better than the test does.
- **The remaining symptoms are structural, not lexical.** Symptoms 2–4 (publish
  altitude, the `version` grab-bag, `container` promotion triplication) are not
  reachable by any verb guard. §3's enforcement was aimed at the one symptom
  already fixed.

A guard is worth its keep when it encodes a decision that was **actually made**
and whose violation is **expensive to reverse**. Verb choice fails the second
test in practice: a reviewer catches it in one comment, before it ever reaches
the clean-break migration in §4.

Removed: `internal/cli/verblexicon_guard_test.go` (`TestNoRetiredCommandVerbs`).

### 4. Migration (clean break, in dependency order)

Command renames are outward-facing: consumers pin them in workflow YAML
(nanolinter → forgejo-ci → this engine, via the vendored binary). We do **not**
carry deprecation aliases — the old spellings are removed outright, and the
lockstep vendoring flow updates every consumer in order:

- Rename to the sanctioned verb in the engine; the old name ceases to exist.
- The same clean-break rule applied to the `trailer-mode` value rename
  (`forgejo-ci` → `coauthor-only`, old value removed).
- Land in dependency order: engine first, then forgejo-ci re-vendors the binary
  and updates its workflow calls in the same change, then nanolinter follows its
  forgejo-ci pin. Each repo is updated atomically with the re-vendor, so a
  consumer is never left calling a name the vendored binary no longer has.

## Consequences

- Symptom 1 (`verify` vs `validate`) is resolved: the renames landed and the
  reserved spelling is documented in §1. The remaining three are structural and
  are follow-up work, not renames this ADR can mechanise.
- One-time churn: `release verify-*` → `release validate-*`, `container
  {base,release}-images verify` → `validate`, and `container ledger
  verify-digests` → `validate-digests`. Each is a separate reviewable commit;
  consumers re-vendor and update their calls in the same change.
- New commands have a rule to follow, applied in review. The surface stays
  predictable by convention and precedent rather than by a build gate; the cost
  of a miss is a review comment, not a migration.
- Cost: the rename is a hard break, so it must land in the engine → forgejo-ci →
  nanolinter order with each consumer updated as it re-vendors.

This ADR records the **decision and the rule**; the renames are follow-up work
tracked separately and gated on this being accepted.
