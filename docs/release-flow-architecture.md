<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
SPDX-License-Identifier: CC0-1.0
-->

# reusable-ci — Release Flow Architecture

> Audience: a DevSecOps engineer or architect new to reusable-ci.
> Scope: the **build-once / promote-many** release flow — how a container moves
> `dev → staging → release(prod)` as a *promotion*, what happens at each stage, and
> how every existing capability (SBOM, scan, sign, provenance, release) is
> preserved, just reorganised. Inspired by the build-promotion pipeline pattern.
> Status of each piece is marked **[built]** / **[planned]**.

---

## 1. The one idea: build once, promote many

You do **not** rebuild the image for each environment. You build it **once**, and
the *same bytes* (identified by their content digest, `@sha256:…`) move through
`dev → staging → release` by **re-tagging**, never rebuilding. Every promotion is a
**verified tag-move**; the digest is the trust anchor.

```
              ┌──────────────────────────────────────────────┐
              │     ONE immutable image   @sha256:abc…        │
              └──────────────────────────────────────────────┘
                  ▲            ▲             ▲            ▲
  build once ─────┘            │             │            │
  (push @digest,         move :dev      move :staging  move :release
   tag :sha-<commit>)    (auto)         (approval)     (approval / tag)

  Every stage tag points at the SAME digest. Promotion = a verified tag-move.
  No rebuild → no chance the "prod" image differs from what was tested.
```

**Why this matters (security & correctness):** with rebuild-per-environment, the
prod image is a *different build* than the one you scanned in dev — you verified
something you didn't ship. Build-once guarantees prod **is** dev, bit-for-bit.

---

## 2. The moving parts

```
  ┌──────────────────────────────────────────────────────────────────────┐
  │  Workflow (YAML)         WHEN + WHO: triggers, environment approvals    │
  │     │  calls verbs                                                      │
  │     ▼                                                                    │
  │  reusable-ci (one binary)  WHAT + VERIFY: build, sbom, scan, sign,       │
  │     │                       ledger add/promote/verify/cleanup/rollback   │
  │     ▼                                                                    │
  │  domain (pure Go)          POLICY: stage graph, tag rules, digest checks │
  │     ▼                                                                    │
  │  adapters                  IO: registry endpoint (ghcr / GitLab /        │
  │                            Codeberg-Forgejo), cosign, forge API          │
  └──────────────────────────────────────────────────────────────────────┘
```

- **The ledger** — a small JSON manifest recording each image: its `digest`, its
  `candidate` (staging) tag, its `final` tag, and optional metadata. It is the
  *source of truth* for "what digest is this release", so promotion never asks a
  human to paste a SHA.
- **Registry endpoint** — an interface; `ghcr.io` today, GitLab Container Registry
  and **Codeberg/Forgejo** as peers. The same flow works on any of them.
- **Forge provider** — GitHub / GitLab / Forgejo, with a **capability** model so
  the flow degrades gracefully where a forge lacks a feature.

---

## 3. End-to-end flow

```
   source ─┐
           ▼
     ┌───────────┐   BUILD ONCE — create ATTESTATIONS OF FACT, bound to the digest
     │   BUILD   │   • build image → push @digest, :sha-<commit>, :staging-<v>
     │  (once)   │   • SBOM • SLSA provenance • cosign ORIGIN signature (keyless)
     │           │   • ledger add {digest, candidate=staging, final, sbom}
     └─────┬─────┘
           │  promote = verified tag-move (no rebuild)
           ▼
     ┌───────────┐   DEV          gate: auto
     │    DEV    │   ledger promote --stage dev   →  :dev points at @digest
     │           │   light checks (structure test). VERIFY only — no signing.
     └─────┬─────┘
           ▼
     ┌───────────┐   STAGING      gate: environment "staging" (required reviewers)
     │  STAGING  │   ledger promote --stage staging → :staging points at @digest
     │           │   VERIFY signature + provenance + namespace; optional re-scan.
     └─────┬─────┘
           ▼
     ┌───────────┐   RELEASE/PROD gate: environment "prod" (reviewers) / tag push
     │  RELEASE  │   ledger promote --stage release → :release + :<version>
     │   (prod)  │   APPROVAL signing: GPG (release/artifacts) + optional prod key
     │           │   + cut forge release (notes, checksums, SBOM assets, binaries)
     │           │   + ledger cleanup (drop staging tag)
     └─────┬─────┘
           ▼
     ┌───────────┐   POST-PROD    gate: cron
     │ POST-PROD │   security scan --image-ref <release digest> vs fresh vuln DBs
     └───────────┘

   ANY stage fails  →  ledger rollback (undo promoted tags that still serve the digest)
```

---

## 4. What happens at every stage

| Stage | Trigger / gate | Actions (reusable-ci verbs) | Signing / evidence |
|---|---|---|---|
| **Build** (once) | merge / release branch | `build *` → push `@digest` + `:sha-<commit>` + `:staging-<v>`; `sbom generate container`; `release provenance`; `container sign` (cosign origin, by digest); **`container ledger add`** | **ATTEST** facts: SBOM + SLSA provenance + cosign **origin** signature — all bound to the digest, inherited free by later stages |
| **Dev** | auto | **`container ledger promote --stage dev`** (verified tag-move); structure test | **none** — dev never signs |
| **Staging** | `environment: staging` approval | **`container ledger promote --stage staging`**; `validate container-signature`; re-verify provenance; `container validate namespace`; optional `security scan` | **VERIFY** the build's attestations (signature + provenance) — no new signing |
| **Release / Prod** | `environment: prod` approval / tag | **`container ledger promote --stage release`** (sets `:release` + `:<version>`); `release create` (notes, checksums, SBOM assets, binary artifacts); **`container ledger cleanup`** | **APPROVE**: GPG sign release/artifacts; optional prod-key cosign signature; assemble release manifest |
| **Post-prod** | cron | `security scan --image-ref <release digest>` | re-scan only |
| **On failure** | — | **`container ledger rollback`** | promoted tags undone |

Only **Build** builds — and only **Build attests** (provenance/origin signature/SBOM are facts about the build). Every later stage is a **promotion of the existing digest**; dev/stage **verify**, prod **approves**.

---

## 5. Two artifact tracks (one orchestrator, one binary)

Not everything can be "promoted." A **container image** is content-addressed, so its
"version" is just a tag — promotable. A **compiled binary** (Go binary, jar, npm
package, IPA, AAB) has its version *baked into the bytes* at build (`-X
main.version`, maven `<version>`), so you can't re-tag a `1.2.3-dev` binary into
`1.2.3` without lying. So the flow has **two tracks**:

```
                      ┌──────────────── BUILD (once) ────────────────┐
       CONTAINER track│                          BINARY / PACKAGE track│
       (PROMOTE)      ▼                          (BUILD-AT-RELEASE)    ▼
   image @digest, tags move:                jars / tarballs / IPAs / AABs,
   dev → staging → release                    version baked at build →
   (no rebuild, digest constant)            attached as RELEASE ASSETS at the
                                            release stage (not promoted between stages).
```

> `reusable-ci` is, and stays, **one binary**. "Two tracks" is about the
> *consumer's release outputs*, not the tool.

---

## 6. The trust boundary (why this is safe)

Every promotion — at every stage, on every registry — runs the same check. A
promotion that would land the wrong image **fails loud** (exit 1), it never passes
silently.

```
  promote  candidate ──► dest :
    1. resolve(candidate) == ledger.digest ?   ─ no ─►  REFUSE (before any copy)
    2. copy:  same-repo   → CopyTag (registry-native retag)
              cross-repo  → cosign copy  (carries image + signature)
    3. resolve(dest) == ledger.digest ?        ─ no ─►  REFUSE (fail loud)
```

The digest is the anchor. The ledger records it once at build; every hop verifies
the registry actually serves it. **[built]** — this logic, plus `rollback`, is the
finished, unit-tested engine.

---

## 7. Cross-registry / digital-sovereignty path

Because a promotion's destination is an *interfaced endpoint*, **every stage's
registry is free-form** — each stage can land on a *different* registry. The base
is just a registry/path string, and the `<base>:<tag>` scheme is registry-agnostic,
so ghcr, GitLab CR, Codeberg/Forgejo and Harbor are peers. A fully split pipeline is
legal:

`--stage-repo` is a destination **prefix** (typically the registry host,
optionally + a namespace); each image's **source path is preserved** beneath it
(`<prefix>/<source-path-after-host>`), the skopeo-sync / registry-replication
idiom — so distinct images map to distinct paths *by construction*:

```
  build once → push candidate (one digest, per container)
        │
        ├─ promote --stage dev     --stage-repo ghcr.io        → ghcr.io/org/<app>:dev
        ├─ promote --stage staging --stage-repo codeberg.org   → codeberg.org/org/<app>:staging
        └─ promote --stage release --stage-repo harbor.io/m    → harbor.io/m/org/<app>:<version> + :release

  • ONE digest throughout — build-once holds across every registry
  • each destination digest is re-verified after its copy (trust boundary)
  • multi-container: ghcr.io/org/api → …/org/api, …/org/web → …/org/web
    (full source paths preserved, collision-free without any name invariant)
```

**How a copy is routed** (`copyToDest`): if the destination base equals the
candidate's base it's a cheap same-repo `CopyTag` (the digest-addressed cosign
signature is already shared). The moment the base differs it routes through
`cosign copy`, which re-uploads the image **and its signature** into the new
registry. A cross-registry copy with **no signature copier configured is refused**
(`ErrUsage`) — the flow never lands an unsigned image silently.

```
  ghcr.io/org/app@digest  ──cosign copy (image + signature)──►  codeberg.org/org/app:release@digest
       (dev / staging)                                              (sovereign prod)

  • same digest (build-once holds across registries)
  • the cosign signature travels (cross-registry needs it; same-repo shares it)
  • destination digest re-verified after the copy
```

**The release stage honors `--stage-repo` too.** Same-registry, every stage —
including release — adds one moving pointer `<base>:<stage>` on the digest; the
immutable `:<version>` was applied once at build and is never re-written by
promotion. When `--stage-repo` is set, the release additionally copies the
immutable `:<version>` tag to that registry alongside `:release`, so "prod on
Harbor" carries the **full** release (version tag + pointer) to the registry you
own — no special `final_tag` baked in at `ledger add` time.

> **Evidence caveat:** image + signature are registry-scoped and travel freely. The
> **SBOM and SLSA provenance are forge-scoped** (release assets / a forge
> capability — see §8), so splitting registries per stage does not split the
> evidence; it still anchors to the forge that cut the release.

This is the convergence of the promotion flow and the sovereignty goal: the
production artifact lives on infrastructure you own, **byte-identical** to what was
tested on ghcr. **[built]** domain + adapter (named *and* release stages honor a
target registry) **and** the live workflow path: `promote-stage.yml` does the dest
login + `cosign` install + copy, wired into `release-orchestrator` via the
`promote-release-repo` / `promote-release-registry[-username]` inputs and the
`PROMOTE_RELEASE_REGISTRY_PASSWORD` secret. Remaining: one CI dry-run to exercise it.

---

## 8. Signing: attestation vs. approval (the key distinction)

Two *different kinds* of signing happen at two *different stages*. Confusing them
is the usual mistake ("should I sign the dev image?").

**Reframe first:** build-once means there is **one digest and one signature**. You
never sign a "dev image" and a separate "prod image" — you sign the **digest once**,
and every stage points at it. Dev carries **no** signing cost and gets no signature
of its own. The real question is only *which* signatures are facts (made at build)
vs. decisions (made at prod).

```
   ATTESTATIONS OF FACT                      APPROVALS OF DECISION
   (created at BUILD, bound to the digest,   (created at RELEASE/PROD,
    inherited free by every stage)            after the gate passes)
   ─────────────────────────────────         ──────────────────────────────
   • SLSA provenance   ← cannot be made       • GPG signing of the forge
     later; it describes the build              release + artifacts (binary track)
   • cosign ORIGIN signature (keyless/OIDC)   • optional prod-KEY cosign signature
     "built by workflow X @ commit Y"           "approved for production" — only if
   • SBOM                                        you want a distinct prod identity
   ─────────────────────────────────         ──────────────────────────────
   DEV / STAGING: VERIFY, never re-sign.      Most setups have NO separate prod key →
                                              the build origin signature IS the
                                              release signature; gates = the approval.
```

| Signing | Asserts | Stage | Why there |
|---|---|---|---|
| **SLSA provenance** | how it was built | **Build** | a build *fact* — impossible to generate later |
| **cosign origin** (keyless/OIDC) | built by CI X @ commit Y | **Build** | digest-addressed → inherited free; lets Staging *gate on signature* |
| **SBOM** | what's inside | **Build** | a build fact |
| **GPG** (release, artifacts, checksums) | release authorised by signer | **Release/Prod** | a release / binary-track decision; GPG can't sign OCI images |
| **prod-key cosign** (optional) | approved for production | **Release/Prod** | only if "built by CI" and "approved for release" are two identities |

**Where the facts are created (always at Build):**
- Container image provenance — `actions/attest-build-provenance` against the
  manifest-list digest (the build job carries `id-token: write`).
- Release-artifact provenance — `reusable-ci release provenance` builds the in-toto
  SLSA statement; `cosign attest-blob`/`sign-blob` signs it.

### Evidence storage and portability (and forge asymmetry)

The flow deliberately **avoids OCI referrers** (which Forgejo's registry doesn't
support), so evidence is portable across forges:

| Evidence | Where it lives | Travels how |
|---|---|---|
| **Signature** | registry-attached (cosign) to the digest | shared in same repo; `cosign copy` cross-registry |
| **SBOM** | forge **release asset** (CISA layers) | rides the release, not the image |
| **Provenance / attestation** | forge **capability** (`Capabilities.Attestation`) | GitHub native; degrades on GitLab/Forgejo (provenance JSON still attachable as a release asset) |

Each promotion **copies + re-verifies** evidence; it never regenerates it.
Capability-asymmetry (GitHub Code Scanning, GitLab SAST report, Forgejo neither) is
modelled explicitly, the same way the flow handles registry asymmetry.

---

## 9. Nothing is lost — every capability still runs, just reorganised

The promotion model **moves where** steps run; it removes none. Mapping the
existing `reusable-ci` capabilities onto the flow:

| Capability (verb) | Before (two rebuild flows) | After (one promotion flow) |
|---|---|---|
| Build (go/maven/npm/gradle/cargo/xcode/swift) | per-flow build | **Build stage (once)** |
| `sbom generate` / `sbom find` | per-flow | Build stage (produced); referenced at release |
| `security scan` / `security report` | per-flow | Build stage + **re-scan at stage / post-prod** |
| `container sign` (cosign) | per-flow | Build stage (by digest); **re-verified at stage** |
| `release provenance` (SLSA) | release flow | Build stage; verified at stage |
| `container validate namespace` | publish | Build stage + re-check at stage |
| `container manifest merge` | publish | Build stage (unchanged) |
| `container ledger add/promote/verify/cleanup/rollback` | unused | **the promotion spine** |
| `release create / checksums / attachments / notes` | release flow | **Release stage** |
| `publish maven-central / npm / appstore / google-play` | publish | Release stage (artifact track) |
| `version bump`, `validate *`, `doctor`, `plan *` | unchanged | unchanged |

So: same toolbox, **reorganised from "build twice (dev + release)" into "build once,
promote, verify at each gate."**

---

## 10. Build status (honest)

| Piece | Status |
|---|---|
| Stage-aware ledger promotion (`--stage`, per-stage validation) | **[built]** |
| Cross-repo/registry promotion (`--stage-repo`, `cosign copy`), digest trust boundary, rollback | **[built]** |
| Ledger schema reconciled to the codebase (kind/SBOM optional, CycloneDX path) | **[built]** |
| Workflow `ledger add` + staging candidate tag in `publish-container` (uploads `release-images-ledger` artifact) | **[built]** |
| `promote-stage.yml` (gated `ledger validate`→`promote`→`rollback`) | **[built]** |
| Gated `promote-dev`/`-staging`/`-release` jobs + `environment:` gates in `release-orchestrator` | **[built]** |
| Sovereignty release promotion wired (`promote-release-repo` + dest login + cosign) | **[built]** |
| Dev container rebuild folded into the ladder (dev = a `:dev` promotion, not a rebuild) | **[built]** |
| One live CI dry-run to prove the wiring end-to-end | **[pending — runtime-untestable locally]** |
| GitHub Environments `staging`/`production` created with protection rules | **[pending — repo settings]** |

The **engine** (Go) and the **workflow wiring** are both done and validated
(actionlint + Go unit tests). What remains is operational: one CI dry-run to
exercise the live path, and creating the gated Environments. Dev NPM/package
snapshots are kept as a separate artifact track — only the wasteful dev container
rebuild was removed.

### Constraints & deliberate trade-offs

- **Two reference styles, one rule against `:latest`.** `:dev`/`:staging`/`:release`
  are *gated, per-environment* moving pointers: an environment's deploy controller
  may legitimately watch its own pointer (that is what the gate enforces — the
  pointer moves only after approval). For *reproducible / immutable* references —
  a release manifest, a pinned digest in IaC, a provenance subject — use the
  digest (or `:<version>`), never a moving pointer. What's banned is `:latest`:
  unscoped, ungated, and cross-environment. The stage pointers differ on all
  three counts.
- **Promotion needs a valid OCI tag; an unusual one degrades, never breaks.**
  The staging tag and ledger are built from the raw release tag, so they only
  push when that tag is usable verbatim as an OCI tag (the `prep` job's
  `ref-clean` output, computed in Go by `IsCleanRefTag`). semver (`vX.Y.Z`) and
  dates (`2024.01`) qualify; a tag with registry-illegal characters (e.g.
  `releases/1.2.3`) skips promotion — the image still publishes with its
  sanitized version tags, signature, and attestation, and the missing ledger
  surfaces as a forge-aware warning at promote time. This is defense-in-depth:
  the release orchestrator already rejects non-semver tags upstream at
  `validate-prerequisites`, so promotion code never *assumes* a clean tag.
- **Staging candidate cleanup is forge-aware.** On Forgejo the `staging-<version>`
  tag is deleted after release; on ghcr it is *retained by necessity* — ghcr can
  only delete a package *version*, which the staging and release tags share, so
  deleting it would delete the release. The leftover tag is a harmless immutable
  record. Both cleanup and rollback are best-effort and never fail a release.
- **The gates control environment rollout, not version publication.** Two
  concerns are deliberately separated: the **GitHub release** (version `1.2.3` is
  published — signed artifacts, SBOMs, binaries, attached to the immutable
  `:<version>`/digest) is cut as soon as build+publish succeed, on the tag; the
  **stage gates** (`staging`/`production` Environments) gate when the image's
  `:staging`/`:release` *pointers* move — i.e. environment rollout. So
  `create-release` runs in parallel with the gates by design (it also keeps
  non-container releases ungated). A *rejected* prod gate therefore means "didn't
  roll to that environment", not "un-publish the release": the version still
  exists at its immutable digest, the `:release` pointer simply never moved, and
  rollback (best-effort) undoes any partial pointer move. If you instead want the
  prod gate to hold back the *version publication* itself, make `create-release`
  depend on `promote-release` (with a conditional-needs guard so non-container
  releases aren't skipped) — a one-line policy change, not a structural one.

---

## 11. Glossary (for newcomers)

- **Digest** (`@sha256:…`) — the content hash of the image. Immutable; the trust
  anchor. Build-once means this never changes across stages.
- **Tag** — a human-friendly pointer (`:dev`, `:v1.2.3`) to a digest. Mutable;
  promotion *moves* a tag to point at the build's digest.
- **Promotion** — moving the next stage's tag onto the recorded digest, verified
  before and after. Never a rebuild.
- **Ledger** — JSON manifest recording `{digest, candidate, final, …}`; the source
  of truth that drives promotion (no human-typed SHAs).
- **Candidate / staging tag** — `:staging-<version>`, the tag the build pushes; the
  promotion source.
- **Stage** — a step in the `dev → staging → release` graph; each has a gate.
- **Gate** — who/what allows a promotion: `auto` (dev), `environment` approval
  (staging/prod), or a tag push.
- **Registry endpoint** — the OCI registry a stage targets (ghcr, GitLab,
  Codeberg/Forgejo, Harbor); an interface, not hard-wired. Free-form per stage —
  each stage may land on a different registry (`--stage-repo`).
- **Capability** — a forge/registry feature flag (e.g. attestation API, referrers);
  the flow degrades gracefully where one is absent.
