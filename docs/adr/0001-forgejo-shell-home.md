<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# ADR 0001: Where the Forgejo consumer shell lives

Status: Proposed (2026-07-03)

## Context

The reusable-ci engine is one binary with three consumer-facing shells:

- **GitHub**: `.github/workflows/` reusable workflows + runtime images, in
  this repo.
- **GitLab**: `templates/` CI/CD Catalog components, in this repo.
- **Forgejo**: composite actions and reusable workflows in the separate
  [itiquette/forgejo-ci](https://codeberg.org/itiquette/forgejo-ci) repo,
  SHA-pinned by consumers, with the binary provisioned via a pinned
  vendored copy (transitional) and versioned together with the action set.

Two shells live with the engine; one lives downstream. That split has real
costs: verb renames need a coordinated bump in a second repo, the shells
drift stylistically, and Forgejo adopters discover the entry point in a
different place than GitHub/GitLab adopters. It also has real benefits:
forgejo-ci carries itiquette-specific policy that does NOT belong in the
engine repo — the locked signer image, the SLSA L3 isolation gate, the
secret-scoping workflow topology, and the Codeberg-specific operational
know-how encoded in its self-tests.

## Options

**A. reusable-ci grows a `.forgejo/` shell tree** (actions published from
this repo, like `templates/` for GitLab). forgejo-ci shrinks to the
itiquette policy layer (signer image, L3 gate, org pins) or is archived.

- One repo owns the verb vocabulary and all three shells; a verb change
  and its three shells land in one commit and one release tag.
- Forgejo CI cannot run this repo's self-tests (no Forgejo runner wired
  here), so the shell would ship less-tested than forgejo-ci's, which has
  a 2200-line live self-test suite.
- The trust boundary muddies: forgejo-ci's value is exactly that its
  signing workflow is pinned, audited, and slow-moving.

**B. forgejo-ci stays the Forgejo shell permanently** (status quo, made
explicit). reusable-ci documents it as the official Forgejo entry point.

- Keeps the signing boundary where the audit and self-tests already live;
  respects that the L3 story is itiquette's, not the engine's.
- Verb/flag changes keep needing the two-repo coordination dance — but
  that dance is already mandatory for the binary pin, so it adds no NEW
  coupling: shells and pin bump together either way.

**C. Split by trust level**: reusable-ci grows `.forgejo/` actions for the
UNPRIVILEGED verbs (checkout, toolchain, build, scan, artifact transport);
forgejo-ci keeps the trust boundary (sign-and-publish, signer image,
promotion, L3 gate).

- Adopters without itiquette's signing requirements get a one-repo
  experience; the audited boundary stays put.
- Two places to look for Forgejo actions; the seam needs documenting.

## Decision

Proposed: **Option B now, revisit toward C when a second independent
Forgejo consumer org appears.** The forced coupling argument decides it:
because the binary is pinned by hash and versioned with the shell, moving
the shell into this repo would not remove the coordinated-bump step — it
would only move the audited signing boundary away from where its
self-tests and ADR history live. The costs Option A removes are smaller
than the trust-boundary cost it introduces. Option C becomes attractive
exactly when adopters who never touch itiquette's signing flow need
Forgejo actions; until then a second shell location is speculative
surface.

Consequences of B:

- `docs/forgejo.md` names forgejo-ci as the official Forgejo shell (done).
- forgejo-ci's action inputs join the consumer-contract freeze (the
  Phase-0 snapshot test) so engine-side verb changes cannot silently break
  the downstream shell.
- Runtime images for Forgejo (coherencetake Phase 4.2) are published from
  this repo like all images, and forgejo-ci consumes them by digest.
