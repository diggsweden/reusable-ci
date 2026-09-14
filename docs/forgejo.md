# Forgejo quick start

How `reusable-ci` runs on Forgejo (hosted or self-hosted). The authoritative
implementation, test, and live-evidence status is maintained in
[`providers.md`](providers.md).

## Detection and overrides

The CLI probes **Forgejo before GitHub**, because Forgejo runners also set
`GITHUB_ACTIONS=true`: `FORGEJO_ACTIONS`, `GITEA_ACTIONS` or `FORGEJO_OUTPUT`
identifies a Forgejo runner and selects the forgejo platform and runner
conventions. On any runner that sets `GITHUB_ACTIONS=true`, `FORGEJO_SERVER_URL`
or `FORGEJO_REPOSITORY` selects Forgejo as the forge to talk to (a GitHub-hosted
workflow publishing to Forgejo) without changing the runner conventions; on
GitLab or a laptop those two variables change nothing. Nothing to configure in the
common case; `--provider forgejo` / `--runner forgejo` (or
`REUSABLE_CI_PROVIDER` / `REUSABLE_CI_RUNNER`) force it when a job's env is
ambiguous.

## Installing the binary in a job

Runtime-container jobs already include the binary. On a plain runner that
supports remote composite actions, use the canonical installer and pin its
implementation to an immutable commit:

```yaml
- name: Install reusable-ci
  uses: diggsweden/reusable-ci/.github/actions/install-reusable-ci@<full-commit-sha>
  with:
    ref: v3.0.0
```

Verification is fail-closed: the release's `checksums.txt` must carry a valid
Sigstore bundle from the pinned publisher identity. A missing asset, checksum
mismatch, missing cosign binary, or missing/invalid signature terminates
installation. The action commit pins the installer code independently from the
binary version selected by `ref`.

The action requires the Forgejo runner to support remote composite actions. If
it does not, use a prebuilt runtime image or vendor the reusable-ci bootstrap
directory at a reviewed commit. Do not assume that
`scripts/bootstrap/install-reusable-ci.sh` exists in the consumer repository.
See the installation contract in [`providers.md`](providers.md#installing-reusable-ci).

## Runner conventions the adapter handles for you

- **Outputs** go to `FORGEJO_OUTPUT` (falling back to `GITHUB_OUTPUT`).
- **Step summaries**: Forgejo renders no job summaries, so summary text is
  routed to the job log instead.
- **Annotations**: no `::error::` dialect; plain `Error:` prefixes.
- **Run artifacts**: `artifact upload/download/digest` speak Forgejo's
  actions-artifact protocol (v1 upload quirks included). The adapter
  accepts only current-run artifacts by design, so keep producing and
  consuming jobs `needs:`-chained inside one workflow run.

## Signing caveat

Public Fulcio does not trust Forgejo as an OIDC issuer, so keyless
signing needs an explicit `--oidc-issuer` against your own Sigstore
stack; key-based cosign signing is the default path on Forgejo. Run
`reusable-ci doctor` in a job to see exactly what the detected forge
supports and what to do where it degrades.
