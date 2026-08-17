# Forgejo quick start

How `reusable-ci` runs on Forgejo (hosted or self-hosted). For the
per-forge maturity table see [`providers.md`](providers.md) — the forgejo
adapter is fully implemented and unit-tested, with live-instance
validation tracked as the remaining integration step.

## Detection and overrides

The CLI probes **Forgejo before GitHub**, because Forgejo runners also set
`GITHUB_ACTIONS=true`: any of `FORGEJO_ACTIONS`, `GITEA_ACTIONS`,
`FORGEJO_SERVER_URL`, `FORGEJO_REPOSITORY`, or `FORGEJO_OUTPUT` selects the
forgejo platform and runner conventions. Nothing to configure in the
common case; `--provider forgejo` / `--runner forgejo` (or
`REUSABLE_CI_PROVIDER` / `REUSABLE_CI_RUNNER`) force it when a job's env is
ambiguous.

## Installing the binary in a job

Use the bootstrap installer — the same one macOS jobs use:

```yaml
- name: Install reusable-ci
  run: |
    source scripts/bootstrap/install-reusable-ci.sh
    REUSABLE_CI_BINARY_SHA256="<pinned sha256>" \
      install_reusable_ci v3.0.0
```

Verification is fail-closed: the release's `checksums.txt` must carry a valid
Sigstore bundle from the pinned publisher identity. A missing asset, checksum
mismatch, or missing/invalid signature terminates installation;
`REUSABLE_CI_BINARY_SHA256` additionally pins the exact binary hash (the same
variable the forgejo-ci signer toolchain asserts).

In practice most Forgejo consumers do not call the binary directly: the
[release-ci](https://codefloe.com/itiquette/release-ci) actions library
wraps the verbs in composite actions pinned by commit SHA, and versions
the binary pin together with the action set.

## Runner conventions the adapter handles for you

- **Outputs** go to `FORGEJO_OUTPUT` (falling back to `GITHUB_OUTPUT`).
- **Step summaries**: Forgejo renders no job summaries, so summary text is
  routed to the job log instead.
- **Annotations**: no `::error::` dialect; plain `Error:` prefixes.
- **Run artifacts**: `artifact upload/download/digest` speak Forgejo's
  actions-artifact protocol (v1 upload quirks included). The adapter
  accepts only current-run artifacts by design — keep producing and
  consuming jobs `needs:`-chained inside one workflow run.

## Signing caveat

Public Fulcio does not trust Forgejo as an OIDC issuer, so keyless
signing needs an explicit `--oidc-issuer` against your own Sigstore
stack; key-based cosign signing is the default path on Forgejo. Run
`reusable-ci doctor` in a job to see exactly what the detected forge
supports and what to do where it degrades.
