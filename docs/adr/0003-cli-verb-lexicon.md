<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# ADR 0003: The CLI verb lexicon

Status: Proposed (2026-07-09)

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
the same coin-flip. A naive round of renames would not fix that: without a
written rule and a build-time check, the vocabulary re-diverges on the next
command. This is the same problem the guard-test family already solved for
single-sourced regexes and env names.

## Decision

Fix the verb vocabulary as a contract, then enforce it, then rename to match.

### 1. The sanctioned verb set

Every command's leading verb (the first path segment that denotes an action,
after its noun group) MUST be one of:

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

### 3. Enforcement

Add a guard test (in the existing `internal/cli/*_guard_test.go` family) that
reflects over the live command tree and fails the build if any command's
leading action verb is outside the sanctioned set, with a small, justified
allowlist for legacy names still carrying a deprecation alias. This converts
the ADR from prose nobody re-reads into a build-time invariant, exactly like
`cienv_guard_test.go` does for env names and `provider_switch_guard_test.go`
does for platform branching.

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

- The four symptoms become mechanical renames once the guard is in place — and
  cannot regress.
- One-time churn: `release verify-*` → `release validate-*`, `container
  {base,release}-images verify` → `validate`, and `container ledger
  verify-digests` → `validate-digests`. Each is a separate reviewable commit;
  consumers re-vendor and update their calls in the same change.
- New commands have a rule to follow and a guard that enforces it, so the
  surface stays predictable as it grows.
- Cost: the rename is a hard break, so it must land in the engine → forgejo-ci →
  nanolinter order with each consumer updated as it re-vendors.

This ADR records the **decision and the rule**; the renames are follow-up work
tracked separately and gated on this being accepted.
