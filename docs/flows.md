<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Artifact and image flows

How a thing you declare becomes a thing you release. This page explains the
two flows that carry build output through the pipeline: **run artifacts**,
which move files between jobs, and the **image ledger**, which moves container
images from build to signature to final tag.

It builds up in three passes: an orientation with no jargon, then the
mechanics, then the details and the invariants. Read as far as you need.

## Orientation

Think of the repo as a factory.

`.reusable-ci/artifacts.yml` is the **order form**. You write down what you
want built: a Java library, a Go binary, a container image. You do not say
how; you say what. Everything downstream is derived from that form.

The factory floor has separate **workstations**, which are CI jobs. They are
isolated, and they cannot hand things to each other directly. So to move a
finished part from one room to the next, you put it in a **labelled box on a
conveyor belt**, and someone downstream picks the box up by its label.

Two different things are called "artifact", and keeping them apart is the
single biggest aid to reading this codebase:

- The **thing you ordered**: a JAR, a binary. Declared in `artifacts.yml`.
- The **box on the conveyor** that carries it between jobs. Temporary, and
  discarded when the run ends. This is what `reusable-ci artifact upload` and
  `download` deal with.

Container images are too big for the conveyor. They go straight into a
warehouse, which is a registry, as soon as they are built. So instead of
shipping the image around, the pipeline ships a **note about** the image.

That note is the **ledger**. It says: the image sitting at digest
`sha256:abc...` is the one that should eventually be called `v1.2.3`.

The reason for that indirection is that **the job that builds is not the job
that signs**. Signing is the project's stamp of approval, and the assembly
line does not get to stamp its own work. The build job writes a note; the
signer independently re-checks that note against the registry before signing.
If the note asks to sign something and call it `v9.9.9` when the release is
`v1.2.3`, the signer refuses.

Promotion never rebuilds. It moves labels. The image built at 10:00 is
bit-for-bit the image released at noon; only the tag pointing at it changed.

## How it works

### The artifact inflow

The chain is **declare, plan, build, upload, download, consume**.

1. A human writes `.reusable-ci/artifacts.yml`. If it is absent and the repo
   has exactly one root ecosystem manifest (`pom.xml`, `go.mod`, and so on),
   one is derived in memory instead.
2. `config.Parse`, then `Validate`, then `Derive`. Parsing is strict
   (`KnownFields(true)` at every level), so a typo fails loudly rather than
   being silently dropped.
3. The result is a typed **`pipeline.ConfigPlan`**, emitted as JSON. This, not
   the YAML, is the contract every downstream job reads. It computes the
   **run-artifact names** once, centrally, so a build job and a publish job
   agree on what the box is called without having to coordinate.
4. Build jobs upload their output under those names. Downstream jobs, such as
   container build, SBOM, sign, release and publish, download them.

The forges disagree about transport, and the code hides that behind one port:

| Forge | How run artifacts move |
|---|---|
| GitHub | Zips everything into one blob (v4 results service) |
| Forgejo | PUTs each file individually (Azure-pipelines-style container API) |
| GitLab | Not at all. Uses declarative `artifacts:` and `needs:` in job YAML |

Every downloaded entry, whichever forge it came from, passes through one
shared gate before touching disk: no absolute paths, no `..`, no backslash
separators, no symlinks, and a 5 GiB per-file cap. The gate is single on
purpose. As `internal/domain/artifact/safe.go` puts it, the guarantees "live
in one place rather than being re-implemented, and re-bugged, per forge".

### The image ledger

The lifecycle is **add, merge, sign, promote, cleanup**, with **rollback**
hanging off the side.

- **add**: the build job records one entry per image it pushed. The digest is
  read back from the registry, never hand-typed. Writes take an exclusive file
  lock, because parallel build jobs write the same file, and re-adding an
  identical entry is a no-op.
- **merge**: for multi-container releases, each container's job writes its own
  small ledger and the promotion job concatenates and dedupes them into one.
- **sign**: the trust boundary. See below.
- **promote**: copies a digest to its destination tags. Never a rebuild. A
  destination already serving the same digest is a no-op; one serving a
  different digest is a hard refusal.
- **cleanup**: deletes the temporary `staging-` tag, but only after confirming
  the promoted tags really serve the digest.
- **rollback**: undoes a promotion by replaying a journal written before
  anything moved.

### The trust boundary

The same function, `entry.Validate(releaseTag)`, runs twice:

- At **record time**, inside `Append` (`internal/domain/imageledger/imageledger.go`).
- At **sign time**, inside the signer (`internal/app/container/ledgersign.go`).

That looks redundant and is not. The build job controls its own inputs, so its
own validation proves nothing to anyone else; it only stops honest mistakes
early. The signer re-runs the identical rule using **a release tag the signer
controls**, not one supplied by the build artifact. So a compromised or buggy
build job cannot get an image signed and promoted under a tag outside the
release it was authorised for. The rule is enforced by the party with
something to lose.

## The details

### Invariants

`Entry` is digest-first. `Ref` is always digest-pinned, the tag fields are
always tag-pinned, and `PinDigest` is the single constructor that builds a
`Ref` from `DigestSource()` plus a registry-resolved digest. No path lets a
digest enter the system by assertion rather than by resolution.

Three axes on `Entry` are deliberately not collapsed into one:

| Field | Meaning |
|---|---|
| `Kind` | Free-form label such as `distroless`. Also the SBOM path fallback. |
| `ImageKind` | Selects the validation scope: release-scoped rules, or content-addressed base-image rules. Empty defaults via `OrDefault()` for legacy entries. |
| `Flavor` | Tag-composition input, orthogonal to both of the above. |

The scoping rule that does the security work: `final_tag` must equal the
release tag or `<releaseTag>-<suffix>`, and `candidate_tag` must be the exact
staging counterpart `staging-<finalName>`. That exact match ties one candidate
to one final tag, so a release promotes precisely the image staged for it.

### Layered checks

Four checks, independent on purpose:

1. **Record-time validation**: first line, not a boundary.
2. **Sign-time re-validation**: the boundary. Adds sign-only checks for
   repository confinement, required `kind` and `sbom`, ref/digest consistency,
   and base-lineage pairing.
3. **Registry re-resolution** (`Verify`, exposed as `ledger validate-digests`):
   catches a recorded digest the registry does not actually serve under any
   claimed ref.
4. **Repository confinement** (`ValidateRefUnderRepository`): applied
   field-by-field to every ref-bearing field, and re-applied to rollback
   journals, because a journal is itself a signer-boundary artifact that
   drives tag deletion.

Digest checks prove what the image is. Repository confinement proves where a
mutation is allowed to land. Neither implies the other.

### The promotion journal

`PromotionRecord` captures exactly the state that cannot be reconstructed from
the ledger afterwards: whether the immutable final tag already existed, and
what digest the moving tag pointed at before. It is restricted to the release
stage with entry release tags, because that is the only case with that
property. It is JSON Lines, so a partial write is still replayable, and it is
written before any tag moves.

`SignatureCopier` being nil refuses cross-repository promotion outright rather
than completing it without signatures.

### Ports

The registry surface is decomposed to the minimum each operation needs:
`DigestResolver`, `Registry` (resolve plus copy), `TagDeleter`,
`CleanupRegistry`, and `PromotionRollbackRegistry`. Tag deletion routes
through the forge package API rather than a generic OCI delete, because
staging and final tags share one manifest and a naive OCI delete would take
the release tag with it.

## How each flow binds build to signature

Both flows face the same problem, that the job which builds is not the job
which signs, and both solve it. They solve it differently, because what they
move is different.

The **image flow** leans on the registry. A registry is content-addressed and
independent of the CI run, so the signer can simply re-resolve the digest from
it using a release tag the signer controls. The evidence lives outside the
pipeline, and the ledger is only a claim about it.

The **artifact flow** has no such registry. The forge's run-artifact store is
the transport itself, and it is not content-addressed, so a digest recorded
beside the artifact would sit in the same trust domain as the bytes it
describes and would prove nothing. The hand-off therefore carries the expected
value out of band, over the forge's control plane:

1. The producing job computes the digest with `release dist-digest` and emits
   it as a job output.
2. The caller passes that output into the signing workflow as an input.
3. The signing job re-derives the digest of what it downloaded and compares,
   with `release validate-dist --dist-dir … --expected-digest …`, which also
   rejects a `dist/` containing symlinks, non-regular entries, or control
   characters in a path.

The digest never travels inside the artifact it protects. A job that rewrites
the artifact cannot rewrite the already-emitted output of a completed job, so
the comparison is evidence rather than a checksum. forgejo-ci's consumer kit
wires exactly this, and its `check-l3-isolation` action asserts the channel is
intact as part of the SLSA Build L3 claim.

Two consequences worth knowing:

- The forge's run-artifact store is still a trusted component for availability
  and for flows that do not bind, and it is named in
  [the threat model](threat-model.md#trust-boundaries).
- reusable-ci's own GitHub release builds and signs in a single job, taking its
  provenance subjects from the checksums produced there, so it has no cross-job
  hand-off to bind in the first place.

`reusable-ci artifact digest` is a separate primitive with a different scheme:
it commits to each file's execute bit and size, which the sha256sum-compatible
`dist-digest` cannot. It is available to adopters who want a strict content
digest inside their own flow. It is not the hand-off mechanism; reach for
`release dist-digest` for that.
