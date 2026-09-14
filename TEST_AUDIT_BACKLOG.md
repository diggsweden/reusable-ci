<!-- SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government -->
<!-- SPDX-License-Identifier: CC0-1.0 -->

# Reusable CI Test-Audit Backlog

This ledger records findings from reusable-ci test-audit Batches 1-13 against working-tree snapshots audited during 2026-09-04 through 2026-09-06 (Batches 1-10) and 2026-09-11 (Batches 11-13). The reusable-ci review is complete; every finding is resolved. The live scenarios extended in Group 74 (PAR-REL-1, PAR-REL-4, PAR-REL-6 and PAR-SIGN-1) compile under the live tag and run only in an authorized lab. The continuation checklist at the bottom records the remaining repository program.

## Status

Unchecked items are unresolved. Revalidate against current code before changing it: findings may be stale, duplicated, or based on an incorrect assumption. A `P-` prefix records where an entry came from, not how serious it is: the defect list reached zero in group 48, and rounds since have found real defects filed as proposals — argv silently lost by the shared process double (P464), a security classifier that depended on the host's locale (P114), an assertion that ran before the code it checked (P134), a credential printed by an unredacted formatter (P123). Read a proposal as a lead, and classify it from the code rather than from its prefix. Prefer small, coherent, maintainable fixes using the repository's architecture, language and test conventions. Tests must use owned temporary state, synthetic data and no external-system activity. Remove an entry only after its fix or rejection is verified, including a focused regression and relevant baseline; remove a related proposal only if its full acceptance criteria are satisfied. Do not renumber remaining IDs. Evidence modes are `executed`, `static/unexecuted`, or `mixed`. Historical audit evidence may reference IDs subsequently removed from the active lists; those references do not make them open findings again.

## Coverage/Counts

Counts cover open entries only. Raw counts are their retained source observations before cross-batch deduplication; verified fixed and refuted entries are excluded.

Findings stay on one line for counting and pruning. Markdown structural checks exclude MD013 and MD038 to preserve that format and whitespace-sensitive reproduction examples.

| Observation set | Raw | Canonical unique |
|---|---:|---:|
| Defects | 0 | 0 |
| Proposals | 0 | 0 |
| Total | 0 | 0 |

Open backlog: 0 defects and 0 proposals, counted from the lists below rather than carried forward. Verified fixed and refuted entries have been removed; remaining IDs are unchanged. Concurrent checkpoint 307ff883 captured accumulated work during group 29; later working-tree changes and existing staging were preserved through group 31, and operator commits at fa444bd9 during group 41, ac38ed88 during group 46 and 0aa131dd during group 60 captured the accumulated work. The group-60 checkpoint landed mid-round and swept up part of that round's edits; the operator has since staged the remainder of group 60, so the index is not empty -- group-60 changes are staged and group-61 changes are unstaged. This remediation run made no Git writes or pushes. Historical no-commit statements in the [history document](TEST_AUDIT_HISTORY.md) describe their individual rounds, not the current repository state.

## Defects

### High


### Medium


### Batch 11 Domain


### Batch 12 Domain


### Batch 13 Adapters


### Batch 8 Additions


### Batch 9 Live Support


## Proposals

### Critical / P0


### High / P1


### Medium / P2


### Low


### Batch 8 Additions


### Batch 9 Primitive Subset


### Batch 9 Filesystem Subset


### Batch 9 Credential Subset


### Batch 9 Hardening And Swap Subset


### Batch 9 Test Infrastructure


### Batch 9 Live Support


### Batch 10 Artifact And Project Type


### Batch 10 Build Domain


### Batch 10 Configuration


### Batch 10 Container


### Batch 11 Domain


### Batch 12 Domain


### Batch 13 Adapters


## Review History

Completed review evidence and detailed remediation records are in [TEST_AUDIT_HISTORY.md](TEST_AUDIT_HISTORY.md). Open findings and their current acceptance criteria remain here.

## release-ci Boundary

`release-ci` is the Itiquette Forgejo workflow/action layer and vendors this engine. Completed engine fixes must later use release-ci's authorized guarded vendoring procedure; never hand edit its vendored binary.

## Continuation Checklist

### Next Steps

- Batches 11-13 completed the reusable-ci review on 2026-09-11 (Batch 11: 44 domain test files; Batch 12: 36; Batch 13: 80 adapter test files not named by any earlier finding). Work on the recorded findings and revalidate each against the current worktree.
- Preserve existing work. Staging, commits, pushes, migration and vendoring require separate approval.
- Use owned offline fixtures and assertion-checked faults; native-tool, provider and live verification tiers remain separately authorized.
- See the [remediation history](TEST_AUDIT_HISTORY.md#remediation-history) for prior decisions, detailed verification and parked checkpoints.

### Remaining Reusable CI Review

- Batches 11, 12 and 13 are complete; their evidence is in the [history document](TEST_AUDIT_HISTORY.md#completed-review-evidence). Adapter test files named by earlier findings were not re-reviewed in Batch 13.
- Remaining verification: live, listener and external-tool cases marked static or compile-only still need separately authorized runtime verification.
- Temporary audit artifacts stayed outside the repository; the Batch 11-13 snapshot was taken from the 2026-09-11 working tree with Groups 69-71 applied.

### Remaining Repository Program

- Next repository: `reusable-ci-blackbox-tests`; inspect its execution boundary before running real toolchains or downloading dependencies.
- Continue the unreviewed tests in the Itiquette `gommitlint` repository, preserving its earlier uncommitted remediation and verification limitations.
- Then review Nanolinter, Forge Sync, Forge Tidy, Forge Lab, and the remaining stable and wip Itiquette repositories from the original inventory. Request precise repository locations rather than searching broad workspace parents when a path is not already known.
- Incorporate completed engine changes into release-ci only through its separately authorized guarded vendoring process.
