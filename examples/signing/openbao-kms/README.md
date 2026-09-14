# OpenBao (KMS) signing example

A Maven library released with signatures produced by OpenBao's
Transit secrets engine. The private key lives in OpenBao and never
leaves it; cosign sends the artefact hash to OpenBao for signing.

This example sets `sign.transparency: none`, so signing does not contact
Sigstore's public Fulcio, Rekor, or TUF endpoints. It is suitable for a
private-only signing network, but the release is not offline: the runner must
reach OpenBao plus the forge, package registry, and container registry used by
the release.

The generic reusable release workflow does not perform a provider-specific
OpenBao JWT exchange. An organization-owned wrapper or fork must exchange the
GitHub OIDC JWT at OpenBao's auth endpoint and expose the resulting short-lived
`VAULT_TOKEN`, together with `VAULT_ADDR`, to the cosign steps. The OIDC JWT
therefore leaves the runner for OpenBao; the private signing key does not leave
OpenBao.

## One-time OpenBao setup

```bash
export BAO_ADDR=https://bao.examplescope.internal
bao login

# 1. Enable Transit
bao secrets enable transit

# 2. Create the signing key (ECDSA-P256 — broadest cosign compat)
bao write -f transit/keys/release-signing \
    type=ecdsa-p256 \
    exportable=false \
    allow_plaintext_backup=false

# 3. Export the public half and commit it
bao read -format=json transit/keys/release-signing \
    | jq -r '.data.keys."1".public_key' > .reusable-ci/release-pubkey.pem
git add .reusable-ci/release-pubkey.pem
git commit -m "chore: add OpenBao release signing pubkey"

# 4. Enable JWT auth so CI can authenticate via the GHA OIDC token
bao auth enable jwt
bao write auth/jwt/config \
    oidc_discovery_url=https://token.actions.githubusercontent.com \
    bound_issuer=https://token.actions.githubusercontent.com

# 5. Create a policy + role bound to this repo + release tags
bao policy write release-signing - <<EOF
path "transit/sign/release-signing" { capabilities = ["update"] }
EOF

bao write auth/jwt/role/release-signing - <<EOF
{
  "role_type": "jwt",
  "user_claim": "sub",
  "bound_audiences": ["https://bao.examplescope.internal"],
  "bound_claims": {
    "repository": "examplescope/myorg",
    "ref_type": "tag"
  },
  "policies": ["release-signing"],
  "ttl": "10m"
}
EOF
```

## Files

- `artifacts.yml` declares `sign.method: kms`, the
  `hashivault://transit/keys/release-signing` URI, and
  `sign.transparency: none`.
- `release-workflow.yml` shows the caller permissions and documents the wrapper
  step the organization must add for OpenBao authentication.

## What gets produced

A Sigstore v3 bundle file (`<artefact>.bundle`) next to each release artefact.
The bundle contains the KMS signature and no Fulcio certificate or Rekor proof.
For container images, the signature lives in the OCI registry next to the
image. Verification must explicitly allow the intentionally absent transparency
log proof; see [Verification](../../../docs/verification.md#signtransparency-what-the-signing-run-publishes-about-itself).

## How to verify

Artefacts:

```bash
reusable-ci validate artifact-signature \
    --artifact my-lib-1.0.0.jar \
    --key .reusable-ci/release-pubkey.pem
```

Container image:

```bash
reusable-ci validate container-signature \
    ghcr.io/examplescope/myorg/my-lib@sha256:abc123... \
    --method kms \
    --key .reusable-ci/release-pubkey.pem
```

The trust anchor is the committed `release-pubkey.pem` plus the
branch-protection rules on the repo (the file would be modified
through a PR that requires reviews). Anyone who tries to forge a
signature would need to compromise both: the public key (visible)
AND the path to land code that signs through OpenBao (gated by
branch protection + the JWT auth role's `bound_claims`).
