# Failed release recovery

Use this runbook after a production release job fails. The safe default is to
preserve immutable evidence, identify the last successful mutation, and rerun
the same release request at the same commit. Never make a failed run appear to
have succeeded by moving or force-replacing a tag.

## 1. Freeze and inventory

1. Stop automatic retries while the failure is classified.
2. Record the release-request tag, intended final tag, commit SHA, workflow run,
   assembly manifest, image ledger, and promotion journal.
3. Check which external mutations exist: final Git tag, forge release, uploaded
   assets, package versions, container version tags, moving container pointers,
   and transparency-log entries.
4. Preserve logs and manifests. They are recovery inputs, not disposable build
   output.

Do not expose secret values while collecting evidence. Credential presence and
provider error classes are sufficient for the inventory.

## 2. Choose the recovery path

### No external mutation

Fix the prerequisite or build failure and rerun the same release-request tag at
the same commit. Production prerelease tags are not a recovery mechanism; use
the snapshot flow for branch testing.

### Final Git tag exists

Verify that the remote final tag resolves to the intended release commit.

- If it matches, rerun the same release request. Exact-tag recovery is
  idempotent and reuses that tag.
- If it differs, stop. Do not force-move or delete the tag. Choose a new version
  after investigating how the conflicting immutable tag was created.

An existing tag is not sufficient evidence by name alone; the commit identity
must match exactly. Release prerequisites enforce this. An existing final tag is
reused only when its release bump commit sits directly on the commit the
release-request tag names, is the branch head, and verifies with the release
key.

### Forge release or assets are partial

Keep the final tag immutable. Rerun only at the same commit and version so the
release flow can reconcile same-version state. Before retrying, verify that any
existing asset with the same name has the expected digest. A conflicting asset
is evidence of a non-idempotent or compromised release and requires a new
version, not silent replacement.

### A package registry accepted the version

Treat an externally published package version as consumed. Do not overwrite it,
even if the forge release later failed. Complete any safe same-version steps
that the registry supports as idempotent; otherwise fix the cause and issue a
new patch version. Registry-specific deletion is not a normal rollback path.

### Container promotion partially completed

Use the promotion journal as the authority. `container ledger rollback` may
restore moving pointers from that journal; do not infer a previous digest from
tag names or logs. Immutable version tags remain evidence and are not deleted as
part of pointer rollback.

After a successful forward retry or journal-backed rollback, use the ledger
cleanup operation for staging tags. Cleanup is tag-scoped and must not delete a
manifest still referenced by a release tag.

## 3. Signing failures

Artifact signing fails unless every selected assembly asset exists and every
configured backend produces its advertised non-empty sidecar. A failed signing
run is not publishable. Remove only local staging output before retrying; do not
delete transparency-log entries or alter an already-published Git tag.

Artifact signing credentials and Git-object signing credentials are separate.
For example, repairing a keyless Sigstore failure does not repair a missing SSH
or GPG key needed for the release commit and tag.

## 4. Retry checklist

- The release-request tag still identifies the intended commit.
- Any final tag is either absent or identifies that exact commit.
- The version is stable `vMAJOR.MINOR.PATCH`.
- Existing assets and package versions do not conflict by digest/content.
- The assembly, checksums, signatures, SBOMs, ledger, and journal belong to this
  commit and workflow attempt.
- Required credentials are present for the specific signing and publish
  backends in the resolved plan.
- Cleanup or rollback commands are driven by validated manifests/journals, not
  reconstructed shell arguments.

## 5. Escalate instead of retrying

Stop and investigate when any immutable identity disagrees: tag-to-commit,
asset-to-digest, package version-to-content, ledger entry-to-image digest, or
signature-to-publisher identity. Repeated retries cannot safely reconcile those
states. Preserve the evidence and release a new version only after the cause is
understood.
