# planarch — native multi-arch container builds via split runners

Goal: replace QEMU emulation with `ubuntu-24.04` + `ubuntu-24.04-arm` runners that build natively, then merge into a multi-arch manifest.

**Status:** All three phases shipped. Native multi-arch container builds end-to-end, no QEMU.

## Design decisions (locked in during Phase 1)

- **Conservative default.** `platforms: linux/amd64`. arm64 is opt-in per caller. Mirrors `publish-container.yml`. Existing callers see no change unless they ask for multi-arch.
- **Matrix-everywhere shape, no back-compat branch.** Single-arch and multi-arch flow through the same `prep + build-arch + merge` pipeline. Single-arch is a matrix-of-1 + one-entry manifest. Cosmetic UI change for single-arch callers (3 sub-jobs instead of 1) accepted in exchange for a single code path.
- **Sunset clock for `platforms`: none.** The input is a permanent knob. We don't need to flip the default later; arm64 can stay opt-in indefinitely.
- **No QEMU anywhere.** `linux/amd64` builds on `ubuntu-24.04`; `linux/arm64` builds on `ubuntu-24.04-arm`. No `setup-qemu-action`.

## Pattern

GitHub-hosted runners include `ubuntu-24.04-arm` (free for public repos; private repos billed at the documented arm64 rate). Standard buildx multi-runner pattern, documented by Docker:

1. **Split** — matrix the build over `[ubuntu-24.04, ubuntu-24.04-arm]`. Each runner builds *its own* arch natively with `--platform linux/<arch>` and pushes by digest only (`type=image,name=…,push-by-digest=true,name-canonical=true`). No tag yet.
2. **Persist digest** — each leg writes its digest to a file and uploads it as an artifact.
3. **Merge** — a fan-in job downloads both digests and runs `docker buildx imagetools create -t <tag> <digest1> <digest2>` to assemble the multi-arch manifest. Tag is applied here.

Wall-clock: cargo arm64 native ~3–4 min, parallel with amd64. Total ~4–5 min vs. current 30+ min QEMU run. Free for public repos; private repos pay arm64 rates.

---

## Phase 1 — `publish-dev-container.yml` (DONE)

**Why first:** no SLSA / no SBOM / no scan threading. Validates the digest-split-and-merge pattern in low-risk territory before tackling production.

**Shipped shape:** `prep` → `build-arch` (matrix on platform) → `merge`. Each `build-arch` leg picks its own native runner via the matrix expression `runs-on: ${{ matrix.platform == 'linux/arm64' && 'ubuntu-24.04-arm' || 'ubuntu-24.04' }}`. Push-by-digest in build legs; `docker buildx imagetools create` in merge applies the dev tag to the manifest list. GHA cache scoped per-arch (`scope=build-amd64`, `scope=build-arm64`). Digest artifacts are scoped per `github.run_id` to avoid cross-run collisions.

**Shape**

```
on workflow_call ─► build-arch (matrix: amd64, arm64-arm-runner) ─► merge ─► outputs
                    │  buildx build, push-by-digest, no tag        │
                    │  upload digest as artifact                   │
                                                                   │ download digests
                                                                   │ buildx imagetools create -t <devtag> <digests…>
                                                                   │ output: image, digest
```

**Files touched (upstream)**

- `.github/workflows/publish-dev-container.yml` — refactor: one matrixed `build-arch` job + one `merge` job. Net ~+80 LoC.
- New `platforms` input (default `linux/amd64,linux/arm64`) so callers can pin to single-arch if they want a fast path.
- `.github/workflows/release-dev-publish-stage.yml` — no change in shape; the matrix leg now calls a workflow that internally fans-out and back-in.

**Test plan**

- Smoke on bjorn-wallet-r2ps: dispatch `release-dev-workflow.yml`, watch both arches build in parallel on native runners, then a third small job merges and pushes.
- `docker buildx imagetools inspect ghcr.io/.../wallet-bff:<devtag>` shows both `linux/amd64` and `linux/arm64` digests.
- `docker pull --platform linux/arm64` on the dev tag works from a non-emulated puller.

**Risk**

- arm64 runner queue depth — add `timeout-minutes: 30` per leg.
- Cache scoping bug — easy to verify by watching cache hit/miss in two runs back-to-back.

**Estimate:** half a day end-to-end including smoke tests.

---

## Phase 2 — `publish-container.yml` (DONE)

**Why later:** SLSA + analyzed-container SBOM + Trivy + SARIF upload all need per-arch threading. Doing this after Phase 1 means we already trust the merge mechanics.

**Shipped shape:** matrix-everywhere mirroring Phase 1, with per-arch attestation/SBOM/scan plumbing inside each `build-arch` leg:

- `prep` resolves image-name, tags (via `docker/metadata-action`), labels, platforms-json. Runs validate-namespace early.
- `build-arch` (matrix on platform): per-arch native runner. Builds by digest with `provenance: mode=max` (per-arch buildkit attestation auto-attaches). Then in the same job: per-arch Trivy scan (SARIF category `container-scan-${arch}`), per-arch syft SBOM (artefact name `analyzed-container-sbom-${run_id}-${arch}`), per-arch `actions/attest-sbom` against the per-arch digest, per-arch `extract.binary` extraction.
- `merge` runs `docker buildx imagetools create -t TAG1 -t TAG2 ... ${ref1} ${ref2}` to assemble the manifest list and apply all metadata-action tags. Outputs the manifest list digest.
- `provenance` (existing slsa-framework workflow_call) attests the manifest list digest. Per-platform buildkit provenance covers the platform-level case.

**Locked-in decisions:**

- **Per-arch buildkit provenance + manifest-level SLSA L3.** Two distinct attestation layers, same as today. `cosign verify-attestation` works against either layer.
- **No double-attestation of manifest with cosign attest.** The slsa-framework job already attests the manifest list; we don't replicate that with a second cosign call.
- **SARIF category suffixed per-arch.** Code Scanning dedupes per (category, file); without the suffix one arch silently overwrites the other.
- **SBOM attest subject = per-arch digest.** Each platform image carries its own SBOM attestation. `cosign verify-attestation <image>@<arch-digest>` succeeds for the matching arch.
- **`extract.binary` artefact name suffixed per-arch.** `${name}-binaries-${arch}` to avoid GHA upload-artifact v4 name collision (which forbids reuse within a run). `release-create-github.yml`'s download pattern updated from `*-binaries` to `*-binaries*` to match both the new and legacy shapes.
- **Custom `cache-from`/`cache-to` inputs are not threaded into the multi-arch matrix** (per-arch GHA cache scoping wins). Documented on the input. Multi-arch callers wanting non-default caches should file an issue.

**Known carryover into Phase 3:** per-arch extracted binaries currently share basenames (e.g., both arches' `hsm-worker` files). When attached to a GitHub Release, the second upload silently overwrites the first. Pre-existing latent issue (would have bitten today's QEMU multi-arch flow too). Phase 3 fixes this with arch-suffixed binary names.

**Shape**

```
on workflow_call ─► build-arch (matrix: amd64, arm64) ─► merge-manifest ─► attest ─► sbom-per-arch ─► scan-per-arch
                    │  buildx with provenance=mode=max  │ imagetools     │           │ syft per       │ trivy per
                    │  push-by-digest                   │ create         │           │  --platform    │  --platform
                    │  upload digest                    │ tag applied    │           │ upload 2 SBOMs │ upload 2 SARIFs
                                                                                                       (categories per arch)
```

**Files touched (upstream)**

- `.github/workflows/publish-container.yml` — split into:
  - `build-arch` (matrix, native, push-by-digest, provenance attached per-arch)
  - `merge-manifest` (single job, applies tag, becomes the public reference)
  - `sbom-analyzed-container` (matrix per arch, syft `--platform`, two SBOM uploads with arch-suffixed names)
  - `scan-container` (matrix per arch, Trivy `--platform`, two SARIF uploads with category-suffixed names — `container-scan-amd64`, `container-scan-arm64`)
  - `summarize` (collects per-arch results, writes step summary)
- Net change ~+250–400 LoC, mostly job restructuring + matrix scaffolding.
- `.github/workflows/release-publish-stage.yml` — drop the amd64-only fallback we just added (`267f825`); restore `linux/amd64,linux/arm64` default once Phase 2 lands.

**Decisions to make during Phase 2**

- **Provenance discoverability:** the per-arch attestations attach to the digests; the manifest itself needs `cosign attest --type slsaprovenance --predicate ... <manifest>` if we want a provenance reference at the manifest level. Decision: rely on per-digest provenance only, document the lookup path in `docs/`. Avoids double-attestation complexity.
- **SBOM attachment:** today `attestations: write` attaches one SBOM per image. We attach one per-arch under the same manifest reference. GitHub UI shows both correctly.
- **SARIF category suffixing:** Code Scanning uses `(category, file)` to dedupe. Suffixing per-arch keeps both visible without overwriting.

**Test plan**

- Cut a `v0.x.x-rc.1` on a smoke-test repo, observe full release flow.
- Verify on the published manifest:
  - `docker buildx imagetools inspect <tag>` shows two platforms
  - `cosign verify-attestation --type slsaprovenance` succeeds against each digest
  - GitHub Code Scanning shows two container-scan results (amd64 + arm64)
  - GitHub Releases page shows SBOM artifacts for both arches
- Negative case: deliberately introduce a CVE-laden base image on one arch only, confirm it surfaces as expected.

**Risk**

- Attestation plumbing is finicky — if `provenance: mode=max` isn't applied identically on both legs, manifest verification gets confused. Mitigated by Phase 1 having already shaken out the merge mechanics.
- arm64 runner availability for private repos — fine for our public scenario; flag in docs.

**Estimate:** 2–3 days including a real RC smoke test.

---

## Phase 3 — container-first binary extraction (upstream, follow-on)

**Why separate:** today `extract.binary` runs once on the build runner against an emulated arm64 binary. With Phase 1+2 done, each arch leg can extract its own native binary cleanly.

**Files touched**

- `publish-container.yml` `build-arch` matrix — when `extract.binary` is set, each leg also outputs `type=local,dest=./extracted-binaries-${{ matrix.arch }}/`.
- New helper to merge per-arch extractions and produce arch-suffixed release assets (`hsm-worker-linux-amd64`, `hsm-worker-linux-arm64`).
- `release-publish-stage.yml` `attach-artifacts` — accept arch-suffixed names.

**Estimate:** half a day on top of Phase 2.

---

## Downstream — `bjorn-wallet-r2ps`

**Phase 1 lands → bjorn-wallet-r2ps:**
- Bump `reusable-ci` SHA pin in 4 workflow files
- Restore explicit `platforms: linux/amd64,linux/arm64` in `.github/artifacts.yml` (or leave at default if the upstream default goes back to multi-arch in Phase 2)
- One smoke run via `release-dev-workflow.yml` to verify

**Phase 2 lands → bjorn-wallet-r2ps:**
- Bump `reusable-ci` SHA pin
- Cut `v0.1.4-rc.1` and verify multi-arch manifest on release

**Phase 3 lands → bjorn-wallet-r2ps:**
- No caller change needed unless we want arch-suffixed binary names in releases. If yes, update `extract.binary.names` semantics (likely a new `arch-suffix: true` flag, default false for compat).

---

## Tracking

Add three sections to `TODO.md` upstream:

1. `Native multi-arch dev container builds (Phase 1)` — small, validates pattern
2. `Native multi-arch production container builds + attestation re-thread (Phase 2)` — bigger
3. `Per-arch native binary extraction for container-first (Phase 3)` — container-first follow-through

Cross-reference each from the existing `Drop arm64 from default platforms` commit so the regression has a clear path back.

---

**Recommend doing Phase 1 now-ish (small, validates pattern, removes the dev-iteration pain we just hit), Phase 2 when there's a 2-3 day window for the production refactor, Phase 3 only if/when binary attachment becomes a real ask.**
