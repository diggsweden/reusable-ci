# Sigstore-keyless signing example

A Go CLI whose release artifacts use Sigstore-keyless signatures via cosign.
There is no long-lived **artifact-signing** key: the artifact identity is the
GitHub Actions workflow that produced the release.

Release commits and tags are signed independently. This example sets
`git-signing.method: ssh`, so it still needs `RELEASE_SSH_SIGNING_KEY` and a
matching principal and public key in `.reusable-ci/allowed_signers`. Start from
the included `.reusable-ci/allowed_signers.example`, replace its demonstration
key with the public half of `RELEASE_SSH_SIGNING_KEY`, and commit it as
`.reusable-ci/allowed_signers`. The SSH key protects Git objects; it is not used
for artifact signatures. reusable-ci does not currently offer keyless
Git-object signing.

## Files

- `artifacts.yml` declares `sign.method: sigstore` and
  `git-signing.method: ssh`.
- `release-workflow.yml` is the caller workflow with the required
  `id-token: write` permission so cosign can mint the runner-issued
  OIDC token.
- `.reusable-ci/allowed_signers.example` shows the OpenSSH allowed-signers
  format required to verify release commits and tags. Its bundled key is only a
  demonstration key and must be replaced.

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
    --cert-identity-regexp '^https://github.com/examplescope/myorg/\.github/workflows/release-workflow\.yml@refs/tags/release-request/v.+$' \
    --cert-oidc-issuer https://token.actions.githubusercontent.com
```

Container image:

```bash
reusable-ci validate container-signature \
    ghcr.io/examplescope/myorg/my-cli@sha256:abc123... \
    --method sigstore \
    --cert-identity-regexp '^https://github.com/examplescope/myorg/\.github/workflows/release-workflow\.yml@refs/tags/release-request/v.+$' \
    --cert-oidc-issuer https://token.actions.githubusercontent.com
```

What this proves: "the file was signed by a job running
release-workflow.yml in examplescope/myorg on a release-request tag". The
branch-protection rules on that workflow file become the
artifact-signing security posture: no reusable artifact-signing private key
exists. Protect and rotate the separate SSH Git-signing key normally.

## Switching from GPG

If migrating from `sign.method: gpg`, the consumer side changes
significantly (`gpg --verify` no longer applies; consumers run
`reusable-ci validate artifact-signature` with cosign). Dual-sign
during transition by maintaining both backends in separate releases
is the polite path; see docs/verification.md.
