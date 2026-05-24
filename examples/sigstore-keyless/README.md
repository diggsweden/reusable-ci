<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Sigstore-keyless signing example

A Go CLI released with Sigstore-keyless signatures via cosign. No
long-lived private key, no secrets to rotate — the signing identity
is the GitHub Actions workflow that produced the release.

## Files

- `artifacts.yml` declares `sign.method: sigstore`.
- `release-workflow.yml` is the caller workflow with the required
  `id-token: write` permission so cosign can mint the runner-issued
  OIDC token.

## What gets produced

For every release artefact (`.tgz`, `.jar`, binary, …) and every
container image, the release flow produces a Sigstore v3 bundle
file (`<artefact>.bundle`) containing:

- the signature
- the Fulcio-issued certificate binding the signing key to the
  workflow identity
- the Rekor transparency-log inclusion proof

The signature for container images is stored in the OCI registry
next to the image, not as a sidecar file.

## How to verify

Artefacts (a downloaded `.bundle` and the matching file):

```bash
reusable-ci validate artifact-signature \
    --artifact my-cli-linux-amd64 \
    --cert-identity-regexp '^https://github.com/diggsweden/myorg/\.github/workflows/release-workflow\.yml@refs/tags/v.+$' \
    --cert-oidc-issuer https://token.actions.githubusercontent.com
```

Container image:

```bash
reusable-ci validate container-signature \
    ghcr.io/diggsweden/myorg/my-cli@sha256:abc123... \
    --method sigstore \
    --cert-identity-regexp '^https://github.com/diggsweden/myorg/\.github/workflows/release-workflow\.yml@refs/tags/v.+$' \
    --cert-oidc-issuer https://token.actions.githubusercontent.com
```

What this proves: "the file was signed by a job running
release-workflow.yml in diggsweden/myorg on a release tag". The
branch-protection rules on that workflow file become the
signing-security posture — no private key can be stolen, because no
private key exists for more than ~10 minutes.

## Switching from GPG

If migrating from `sign.method: gpg`, the consumer side changes
significantly (`gpg --verify` no longer applies; consumers run
`reusable-ci validate artifact-signature` with cosign). Dual-sign
during transition by maintaining both backends in separate releases
is the polite path — see docs/verification.md.
