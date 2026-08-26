# Verification Guide

How to verify the artifacts these workflows produce — checksums,
signatures, SBOM attestations, and SLSA provenance — and how the
matching guarantees are generated on the producing side.

## Code Quality Verification

### Linting

PR linting runs through `lint-nanolinter.yml` (called automatically by the PR
orchestrator when `lint-engine: nanolinter`, the default). It runs `nanolinter
verify` inside the nanolinter flavour image — which bakes nanolinter and its
check toolchain — against your project's `nanolinter.toml` verify plan. Security
findings (SAST, dependencies, secrets) are uploaded to GitHub Code Scanning as
SARIF. Swift/iOS projects additionally run `lint-swift.yml` on macOS.

A **mandated lint floor** is enforced on every run: the job passes `nanolinter
verify --require "$REQUIRED_LINTS"` (default `secrets,sast`), forcing those
checks to run *and* block regardless of the project's `nanolinter.toml`. A
project cannot drop SAST or secret detection — it can only add to the floor
(by `extends`-ing a base with `[policy].require`), never remove from it. The
set is the `required-lints` orchestrator input.

Your project needs a `nanolinter.toml` verify plan; the flavour image provides
the tools, so no `just`/`justfile` is required. (A local `.mise.toml` + `just`
recipe is optional, for running the same checks on a developer's machine.) The
alternative engine, `lint-engine: megalinter`, reads a `.mega-linter.yml`
instead. See the [examples](../examples/).

## Artifact Verification Methods

| Artifact Type | Verification Methods | Security Level | What It Proves |
|--------------|---------------------|----------------|----------------|
| **Container Images** | SLSA provenance, SBOM attestations | High/Maximum | Built by official CI, unmodified, with traceable dependencies |
| **Maven JARs** | GPG signatures, checksums | High | Signed, unchanged since publication |
| **NPM Packages** | npm registry integrity and package metadata | Medium | Package available from the intended registry/version |
| **Release Assets** | GPG / Sigstore-keyless / KMS signatures (cosign), SHA256 checksums | High | Authentic release files from official builds; identity bound to workflow (sigstore) or HSM (KMS) |
| **Git Tags** | GPG/SSH signatures | High | Release tags created by authorized developers |
| **Git Commits** | GPG/SSH signatures | High | Commits made by verified developers |

High-security verification methods use industry-standard cryptographic signatures and attestations. NPM package verification is currently registry-integrity based.

## Signing Methods for Release Artifacts

`reusable-ci release sign` supports three signing backends — pick per repo via `--method` (or `sign.method` in `.reusable-ci/artifacts.yml`). All three produce verification material the consumer reads via `reusable-ci validate artifact-signature`, which auto-detects the method from the sidecar layout (`.asc` vs `.bundle`).

| | `--method=gpg` (default) | `--method=sigstore` | `--method=kms` |
|---|---|---|---|
| Private key lifetime | long-lived (operator-managed) | ephemeral (~10 min) | long-lived, never leaves the KMS/HSM |
| Identity claim | "someone holding key X" | "this workflow ran this commit" | "someone authorized to call KMS key Y" |
| External-service dependency | none | Fulcio + Rekor (Sigstore public infrastructure) | KMS provider (or self-hosted OpenBao) |
| Air-gap compatible | yes | no | yes (with private KMS / TPM / OpenBao) |
| Sidecar file | `<art>.asc` | `<art>.bundle` (v3 Sigstore bundle) | `<art>.bundle` (v3 Sigstore bundle) |
| Consumer command | `gpg --verify` (universal) | `reusable-ci validate artifact-signature --cert-identity-regexp=...` | `reusable-ci validate artifact-signature --key=...` |
| Swap policy applies | yes (decrypted key in heap) | no | no |

The cosign methods share infrastructure: cosign 3.x is baked into the runtime image and emits the [Sigstore v3 bundle format](https://docs.sigstore.dev/cosign/key_management/overview/) (one JSON file containing signature, optional Fulcio cert, and Rekor proof) regardless of backend.

### `--method=gpg` — long-lived OpenPGP key (default)

```yaml
# .reusable-ci/artifacts.yml
sign:
  method: gpg
```

```yaml
# .github/workflows/release.yml — workflow-side env
env:
  GPG_PRIVATE_KEY: ${{ secrets.GPG_PRIVATE_KEY }}
  GPG_PASSPHRASE:  ${{ secrets.GPG_PASSPHRASE }}
```

Produces `<artifact>.asc`. Consumer verifies with `gpg --verify <art>.asc <art>` against an out-of-band-distributed public key, or with `reusable-ci validate artifact-signature --artifact <art> --public-key-file pubkey.asc`.

This is the lowest-friction backend for downstream consumers (gpg is in every distro) but has the heaviest operator burden: the long-lived private key requires rotation discipline, swap-page-safe handling (see [Swap policy](#swap-policy)), and a distribution mechanism for the matching pubkey.

### `--method=sigstore` — Sigstore-keyless via OIDC + Fulcio + Rekor

```yaml
# .reusable-ci/artifacts.yml
sign:
  method: sigstore
```

No workflow secrets to manage. The runner's OIDC token is sent to Fulcio, which issues a 10-minute X.509 certificate binding an ephemeral signing key to the workflow's identity. The signature and certificate are uploaded to the Rekor public transparency log.

OIDC issuer auto-detection per platform:

| Platform | Detected via | OIDC issuer URL |
|---|---|---|
| GitHub Actions | `$GITHUB_ACTIONS` | `https://token.actions.githubusercontent.com` |
| GitLab SaaS | `$GITLAB_CI`, no `$CI_SERVER_URL` | `https://gitlab.com` |
| GitLab self-hosted | `$CI_SERVER_URL` set | value of `$CI_SERVER_URL` |
| Forgejo | `$FORGEJO_*` | identity is resolved, but the issuer is left empty — public Fulcio does not trust a Forgejo issuer, so supply `--oidc-issuer` + a trusting Fulcio explicitly |

Override the detected default with `--oidc-issuer <URL>` when needed.

Produces `<artifact>.bundle` (v3 Sigstore bundle JSON).

**Verifying inside CI (zero-config):** when `validate artifact-signature` runs on
a keyless-capable forge (GitHub / GitLab) and you pass neither
`--cert-identity-regexp` nor `--cert-oidc-issuer`, both are derived from the
forge's `SigningIdentityResolver`: the issuer from the detected platform and the
identity anchored to the running repository (`^<repo-url>/`, regexp-escaped).
"Verify as this repo on this forge" therefore needs no flags:

```bash
reusable-ci validate artifact-signature --artifact app.tgz --method sigstore
```

**Verifying elsewhere (or pinning a different repo):** supply the constraints
explicitly — explicit flags always win over the derived defaults:

```bash
reusable-ci validate artifact-signature \
    --artifact app.tgz \
    --cert-identity-regexp '^https://github.com/<owner>/<repo>/' \
    --cert-oidc-issuer 'https://token.actions.githubusercontent.com'
```

The cert-identity regexp is the load-bearing trust claim: "I trust signatures from workflows in this repo." Because it is derived from the trusted runner environment and anchored to the repository, the CI default is both safer and less error-prone than a hand-typed pattern (which can silently be too loose). Branch protection / required reviews on the producing repo *become* the signing-security posture, because forging a signature requires getting code to run as the release workflow.

### `--method=kms` — cosign with explicit key reference

```yaml
# .reusable-ci/artifacts.yml
sign:
  method: kms
  key: hashivault://transit/keys/release-signing
```

The key reference is a cosign URI. All KMS-style URIs cosign supports work here:

| Provider | URI shape |
|---|---|
| AWS KMS | `awskms:///alias/release-signing` (or full ARN) |
| GCP Cloud KMS | `gcpkms://projects/<p>/locations/<l>/keyRings/<r>/cryptoKeys/<k>` |
| Azure Key Vault | `azurekms://<vault-name>.vault.azure.net/<key-name>` |
| **HashiCorp Vault / OpenBao** | `hashivault://transit/keys/<key-name>` |
| PKCS#11 (TPM / HSM) | `pkcs11:object=<label>` |
| Local key file (testing) | `./signing.key` (plain path) |

The OpenBao integration is identical to Vault (OpenBao is the LF-managed Vault fork, API-compatible). Set `VAULT_ADDR` and authenticate via OIDC (recommended) or AppRole:

```yaml
# .github/workflows/release.yml
env:
  VAULT_ADDR: https://bao.example.internal
  VAULT_AUTH_METHOD: jwt              # OpenBao's JWT auth method consumes the GHA OIDC token
  VAULT_AUTH_ROLE: github-actions-releases
```

One-time OpenBao setup:

```bash
bao secrets enable transit
bao write -f transit/keys/release-signing type=ed25519
bao read -format=json transit/keys/release-signing \
  | jq -r '.data.keys."1".public_key' > release-pubkey.pem
git add release-pubkey.pem && git commit -m "Add release signing pubkey"
```

Produces `<artifact>.bundle`. The signature was computed inside OpenBao; the private key has never been in the Go heap (so the [swap policy](#swap-policy) doesn't apply). Consumer verifies with the committed pubkey:

```bash
reusable-ci validate artifact-signature --artifact app.tgz --key release-pubkey.pem
```

### `sign.transparency` — what the signing run publishes about itself

Signing is not only a local act. cosign writes an entry to the **public**
Sigstore transparency log (Rekor) for every signature it makes — on `kms` as
well as `sigstore`. This surprises people, because the KMS key is yours and
nothing about the signature obviously needs a public service.

What the entry contains: the artifact's SHA-256, the signature, the public key,
and a timestamp. **Not** the artifact's contents, and not its filename. So what
becomes public is the *existence and timing* of a signing event, plus a
fingerprint someone who already has the artifact can use to confirm it is the
one you signed. The entry is permanent and append-only — it cannot be withdrawn.

For anything you publish openly, this is the point rather than a cost: it makes
your release independently auditable and gives the signature a trusted
timestamp. That is why it is the default:

```yaml
sign:
  method: kms
  key: hashivault://transit/keys/release-signing
  # transparency: public   ← the default; no need to write it
```

For an artifact you do **not** publish — an internal-only build — the metadata
is a release-cadence signal you may not want to emit, and the log buys you
nothing since nobody outside can obtain the artifact to verify anyway:

```yaml
sign:
  method: kms
  key: hashivault://transit/keys/release-signing
  transparency: none
```

The signature still verifies against your public key indefinitely — that is
ordinary PKI. What you give up is public verifiability, so verification needs
`--insecure-ignore-tlog`, and cosign will warn on every verify. That is
accurate, not pedantic: an unlogged signature has no independent evidence of
*when* it was made.

Two rules the config enforces so they fail at `config-validate` rather than deep
inside cosign:

- `transparency` is rejected for `method: gpg`. OpenPGP has no transparency log
  and cosign is never invoked.
- `transparency: none` is rejected for `method: sigstore`. Keyless signs with a
  ~10-minute Fulcio certificate, so a verifier needs a Rekor inclusion proof to
  know the certificate was valid when it signed. Without one the signature is
  unverifiable by anyone, including you. Use `method: kms` to sign without a log.

**Egress:** if you run Harden Runner with `egress-policy: block`, a run that
publishes needs `rekor.sigstore.dev:443` and `tuf-repo-cdn.sigstore.dev:443`
allowed — including on `method: kms`. A blocked Rekor does not degrade to an
unlogged signature; it fails the signing step hard, at release time. The config
plan precomputes `sign.requires_sigstore_egress` so a caller can derive the
allowlist instead of guessing.

With `transparency: none`, signing needs **no Sigstore endpoint at all** — not
even TUF. That is deliberate and slightly subtle: turning off the transparency
log alone would still leave cosign fetching its trust root from
`tuf-repo-cdn.sigstore.dev` on every signature, because it verifies each
signature it has just written. reusable-ci supplies that trust material locally
instead, so a `transparency: none` signing run makes no outbound connection.
The black-box suite enforces this through a recording proxy rather than trusting
the flag (`release_sign_no_egress_test.go`), because "makes no outbound
connection" is the property people actually assume and argv alone cannot prove.

One exception worth knowing: **verifying a keyless signature still fetches the
trust root**, regardless of this setting. It has to — validating a short-lived
Fulcio certificate is exactly what the trust root is for. `transparency` governs
signing; keyless verification is inherently online.

### Snapshot-release trust model

Snapshot releases (`release-snapshot-orchestrator.yml`) are intentionally **not**
attested, and nothing on this path is cosign-signed. They exist for fast
iteration on branch pushes, not for distribution to external consumers. A
snapshot run publishes a content-addressed npm snapshot (dist-tag `snapshot`,
version `<base>-snapshot-<branch>-<sha>`) and, when opted in, SBOMs — it builds
**no container images**.

The one exception is `publish-gradle` (opt-in, off by default). Maven artifacts
have no post-hoc signing step: a Gradle project configured with the `signing`
plugin signs its publications as it produces them, so a Maven Central snapshot
carries a detached PGP signature made by the same release key the production
path uses. That makes the gradle snapshot legs the only credentialed jobs in
this flow — they receive `MAVEN_CENTRAL_*` and `RELEASE_GPG_*`. Everything else
here runs on the auto-provided forge token alone. Central does not *require*
signatures on snapshots; this is a property of how Gradle publishes, not an
attestation guarantee, and it says nothing about provenance.

Container images are built **once on the release path** (signed with cosign,
SLSA-attested, SBOM-attested) and then promoted to the moving `:dev` → `:staging`
→ `:release` tags by the build-once/promote-many ladder. A `:dev`-tagged image is
therefore the *same signed digest* as a release build — but `:dev` is a moving
pointer that advances on every qualifying build, so pin by digest (`@sha256:…`)
when you need a stable reference.

If you need a signed pre-release for external testing, use a `v1.0.0-rc.1`-style
prerelease tag — that goes through the full production release pipeline and
carries the full attestation stack.

### Container image attestation & verification

Container images carry three independent, registry-attached pieces of evidence,
all verifiable on **any** registry or forge (GitHub / Forgejo / GitLab) with the
`cosign` CLI — no GitHub attestation API required:

| Evidence | How it is produced | How to verify |
| --- | --- | --- |
| **Signature** (integrity + identity) | `reusable-ci container sign` (cosign, `--recursive` over per-arch children) | `cosign verify` / `reusable-ci validate container-signature` |
| **SLSA v1.0 provenance** (how/where it was built) | `reusable-ci container attest --type slsaprovenance1` — a **signed** in-toto attestation; predicate generated from the CI environment | `cosign verify-attestation --type slsaprovenance1` |
| **SBOM** (what is inside) | `container attest --type cyclonedx` — a **signed** in-toto attestation, one per per-arch image digest (syft) | `cosign verify-attestation --type cyclonedx` |

Why a **signed** cosign attestation rather than BuildKit's in-index provenance:
BuildKit writes provenance as an *unsigned* in-index in-toto statement — it has
no signature of its own and no general verifier, so tampering is only caught
indirectly (it would break the image signature). `cosign attest` produces a
signed attestation that `cosign verify-attestation` checks directly, on any
forge. This is the portable replacement for `actions/attest-build-provenance`,
which only records to GitHub's (ghcr.io-only) attestation store.

Verify provenance (sigstore-keyless example):

```bash
cosign verify-attestation \
  --type slsaprovenance1 \
  --certificate-identity-regexp '^https://github.com/<owner>/<repo>/\.github/workflows/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  <registry>/<owner>/<image>@sha256:...
```

We deliberately do **not** also push to GitHub's attestation store
(`actions/attest-build-provenance`): it is GitHub-only and would be a redundant
second copy of the same provenance. The single cosign attestation serves GitHub
consumers too (they run `cosign verify-attestation` like everyone else).

When verifying the image *signature* from inside CI, `reusable-ci validate
container-signature <image>` derives `--cert-identity-regexp` /
`--cert-oidc-issuer` from the forge exactly like `validate artifact-signature`
above — no flags needed on GitHub / GitLab, explicit flags still win, and a
`--key` (KMS) verification skips the derivation. External consumers running
`cosign verify` directly must supply the identity constraints themselves, as
shown.

**SLSA level — be precise.** The portable cosign provenance is **signed and
verifiable, ~SLSA Build L2**: the attestation is produced and signed *inside the
build job*, so the signing identity is reachable by the build steps. **True SLSA
Build L3 is not achievable portably** — it requires an *isolated* builder whose
provenance-signing material the build steps cannot reach (e.g.
`slsa-framework/slsa-github-generator`, which runs in a separate, tamper-isolated
reusable workflow). That isolation is a property of the forge's control plane and
is therefore inherently forge-specific (GitHub-only). Note `attest-build-provenance`
would *also* be in-job (≈L2), so it buys no level upgrade — reaching L3 on GitHub
means adopting `slsa-github-generator`. So: **portable ⇒ L2; L3 ⇒ GitHub-specific.**

#### SLSA Build L3 (experimental — roadmap)

> **Status: experimental, not yet exercised in CI.** The pieces below ship so a
> private/self-hosted operator *can* wire a genuine SLSA Build L3 pipeline with
> standard Sigstore components, but the flow has not been run end-to-end in this
> repo's CI yet. Treat it as a documented capability on the roadmap, not a
> turnkey default.

L3's two requirements — **isolation** (ephemeral build environment) and
**non-forgeability** (the provenance signer is unreachable by the build steps) —
are properties of *your* platform, not of a CLI flag. reusable-ci can't
manufacture them, but it makes them easy to *wire* and *verify* with pure
Sigstore (`cosign` + a KMS key), no forge-specific machinery:

```text
build job   (no KMS creds, ephemeral)  ──push by digest──▶  registry
    │  digest only (immutable, self-verifying)
    ▼
attestor    (KMS sign creds ONLY here, ephemeral)  ──signed provenance──▶  registry
            .github/workflows/slsa-attestor.yml
```

- **Signer:** `reusable-ci container attest --method=kms --key <kms-uri>` — works
  with any cosign KMS (`hashivault://` OpenBao/Vault, `awskms://`, `gcpkms://`,
  `azurekms://`, PKCS#11/HSM). `--builder-id` sets the provenance `builder.id` to
  your documented builder identity.
- **Component:** the [`slsa-attestor.yml`](../.github/workflows/slsa-attestor.yml)
  reusable workflow is the isolated signer — call it as a **separate job** that
  takes only the image digest; it never trusts build-supplied metadata.
- **The two operator obligations** (inherent to L3): grant the KMS sign
  permission to the attestor identity **only** (never the build job), and run
  both jobs on **ephemeral** runners.

The verifier pins the **KMS public key** as the builder root of trust — a valid
attestation then proves it was signed by a key only the isolated attestor can
use:

```bash
cosign verify-attestation --key kms-builder.pub \
  --type slsaprovenance1 <registry>/<image>@sha256:...
```

This is **L3-in-substance, verifiable against your own builder key**, on any
forge. What it does *not* give is a pre-blessed `slsa-verifier` builder badge
(those are wired to GitHub/GitLab SaaS builders); third parties verify against
*your* documented KMS builder identity instead — which is exactly right for a
private deployment, where the trust root is your KMS.

**Roadmap (not yet built):** a `reusable-ci validate slsa-topology` static check
that fails if a build job is granted the signing credential (the one
anti-pattern that silently drops you back to L2), and per-arch SBOM attestation
moved into the isolated attestor.

### Choosing a method

- **Sigstore-keyless — recommended default for new projects** on a keyless-capable forge (GitHub / GitLab). No secret management, no key on the runner; in-CI verification needs no flags (the identity is derived and anchored, see above). The `examples/signing/sigstore-keyless` project is the copy-from reference.
- **GPG**: required for **PGP-native ecosystems** — Maven Central, apt/rpm repos — where consumers expect a `.asc`. Also the right pick for existing pipelines and operators who want to own the trust anchor entirely. This is the `examples/maven-app` shape.
- **KMS (OpenBao recommended)**: regulated / sovereignty-conscious deployments where the trust anchor must stay inside your own infrastructure. Air-gap compatible. See `examples/signing/openbao-kms`.

A repo can switch backends by changing one line in `artifacts.yml`. No swap-policy implications on the cosign branches (the decrypted private key never enters our process).

**Default when `sign.method` is unset:** `gpg`, unchanged — this preserves the pre-cosign contract for existing repos. New projects should set `method: sigstore` explicitly rather than rely on the fallback; the recommendation lives here and in the example projects, not in a silent default flip (which would change behaviour for every repo that never configured signing).

### Signing the release commit & tag (git objects)

The `sign:` block above selects how *release artifacts* are signed. Signing the **release commit and tag** (git objects) is a separate, independent axis, configured under `git-signing:`:

```yaml
# .reusable-ci/artifacts.yml
git-signing:
  method: ssh    # gpg (default) | ssh
```

- **`gpg` (default)** — the release commit/tag are signed with the imported `RELEASE_GPG_PRIVATE_KEY`; the committer identity comes from the key's UID. Unchanged for existing repos; omit the block entirely to keep it.
- **`ssh`** — git-native SSH signing (`gpg.format=ssh`). No OpenPGP key is imported onto the runner. `gitsign` (Sigstore git signing) is intentionally **not** offered: SSH signing is forge-portable and verifiable via `allowed_signers` without the extra dependency.

**To enable `ssh`, provide three things:**

1. **The signing key** — set the `RELEASE_SSH_SIGNING_KEY` secret to an OpenSSH **private** key (the workflow writes it 0600, configures git, and removes it in an `if: always()` cleanup).
2. **The committer identity** — pass `committer-name` and `committer-email` to the release orchestrator. Unlike a GPG key, an SSH key carries no name/email, so this identity is supplied explicitly.
3. **The allowlist entry** — commit the matching SSH **public** key to `.reusable-ci/allowed_signers`, keyed on the **same email** you pass as `committer-email`.

> ⚠️ `committer-email` is **not cosmetic** in SSH mode: it is the verification *principal*. `git verify-tag` runs `ssh-keygen -Y verify -I <committer-email>` and matches it against `.reusable-ci/allowed_signers`. If the email is not listed there, `validate tag signature` (and any consumer running `git verify-tag`) fails. With GPG this coupling doesn't exist — verification keys off the key fingerprint, not the email.

Verification is identical to any SSH-signed tag — see [SSH Key Setup](#5-ssh-key-setup-for-git-verification) and [Git Tag Verification](#6-git-tag-verification) below. `reusable-ci doctor` cross-checks the setup: with `git-signing.method: ssh` it warns when `.reusable-ci/allowed_signers` is absent and reminds you the configured `committer-email` must be one of its principals.

## Release Authorisation

Release authorisation is the policy "who may trigger a non-SNAPSHOT release of this project?" In reusable-ci it lives in **committed files** under `.reusable-ci/`. No CI secret. No platform-specific user database. No leakage when migrating between Git hosts.

The committed-file model lets the signature do the heavy lifting — for SSH, `git verify-tag` natively reads `gpg.ssh.allowedSignersFile`; for GPG, an in-process verifier checks the tag signature against the project's committed public keys. A release fails closed when the policy is on but the signer cannot be established.

### Request → promote: release tags are immutable

A release is requested by pushing a **signed `release-request/vX.Y.Z` tag**. reusable-ci verifies *that* tag's signature (this is what the allowlist checks), bumps the version + changelog into a commit, then **creates the final `vX.Y.Z` tag once** at that commit and pushes it without `--force`.
No tag is ever moved, deleted, or force-pushed: both `release-request/vX.Y.Z` and `vX.Y.Z` are immutable.
The signed request tag stays in the repo as the cryptographic anchor of who authorised the release, and the bot's release commit records the original tagger in `Release-Request` / `Release-Authorized-By` / `Co-authored-by` trailers. So the verified authorisation and the shipped commit always correspond.
(The `reusable-ci version derive-release` and `version tag-release` commands implement this; workflows never parse refs or move tags in bash.)

### Org policy: allowlisting is on by default at the caller layer

The engine input `release.requireallowlistedsigner` defaults to **false** so the tool stays usable in any repo — but **org policy turns it on in the shared release-workflow template**, so every diggsweden release is signed by an allowlisted maintainer. The reference consumer (`wallet-backend-reference`) shows the live wiring:

```yaml
# .github/workflows/release-workflow.yml
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v3.0.0-pre
    with:
      release.requireallowlistedsigner: true   # org default; override only for private repos
```

Setting it at the caller layer (not hardcoding it non-overridable in the engine) leaves a genuinely-private repo an **auditable** escape hatch: dropping the line to `false` is a visible change in the repo's own workflow, reviewed like any other.

### When the gate runs

Two switches enable it:

- **Per-artifact**: `require-authorization: true` on any artifact in `artifacts.yml`. Use this when a *specific* deliverable (e.g. a public library) needs the gate; other artifacts in the same repo aren't gated.
- **Per-release**: `release.requireallowlistedsigner: true` on `release-orchestrator.yml` (the org default). Use this when *every* release of the repo must pass the gate.

If either is true, the gate runs. Behaviour when the gate is on:

| Scenario | Outcome |
|---|---|
| Signer's key/fingerprint is in the allowlist | release proceeds |
| Signer's key/fingerprint is NOT in the allowlist | `EX_NOPERM` (exit 77) |
| Allowlist files are all missing or empty | `EX_NOPERM` (exit 77) — fails closed |
| Tag signed but signature can't be verified against any allowed key | `EX_NOPERM` (exit 77) — fails closed |
| Tag is SNAPSHOT (`-SNAPSHOT` suffix) | gate skipped (SNAPSHOTs are pre-releases) |

When the gate is **off**, a present allowlist is still honoured, but a missing allowlist (or an unverifiable signature) is **not** silent: the run emits a prominent `::warning::` in the Annotations pane and a `> [!WARNING]` callout in the prerequisites summary — "this release ran with no signer allowlist; any valid signature was accepted." A release without an allowlist can't slip by unnoticed.

### The allowlist files

`.reusable-ci/allowed_signers` — for SSH-signed tags. OpenSSH `allowed_signers` format (see `man ssh-keygen`, *ALLOWED SIGNERS*). The same file `git verify-tag` reads when you set `gpg.ssh.allowedSignersFile`. Each line carries the public key inline, so it is both the authorisation list and the verification material:

```text
# .reusable-ci/allowed_signers
alice@example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI...alice's release key
bob@example.com   ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI...bob's release key valid-before=2027-01-01
```

The `valid-before` / `valid-after` options let a key's authority expire without removing audit history.

`.reusable-ci/allowed_gpg_keys.asc` — for GPG-signed tags. An armored public-key bundle (one or more `PGP PUBLIC KEY BLOCK` sections concatenated). Like the SSH file, it is **both the verification material and the authorised set** (single source): the primary-key fingerprints of the keys in the bundle *are* the allowlist, so the two can never drift apart. Export with `gpg --armor --export <email> >> .reusable-ci/allowed_gpg_keys.asc`.

```text
# .reusable-ci/allowed_gpg_keys.asc
-----BEGIN PGP PUBLIC KEY BLOCK-----
...alice's release key...
-----END PGP PUBLIC KEY BLOCK-----
-----BEGIN PGP PUBLIC KEY BLOCK-----
...bob's release key...
-----END PGP PUBLIC KEY BLOCK-----
```

Because the signer's public key travels with the repo, the in-process verifier can actually check the signature — so `release.requireallowlistedsigner: true` genuinely enforces: a missing/empty allowlist or a signature that can't be verified against any committed key fails closed, rather than waving an unverifiable signature through. (There is no separate fingerprints-only file — a fingerprint with no key material can't be verified, which was the gap this design removes.)

All allowlist files are reviewed via the same PR process that protects `main`. Adding or removing a signer is a commit; the audit trail is `git log`.

### Verifying locally

You can run the gate against a tag without pushing:

```bash
reusable-ci validate tag signature \
    --tag v1.2.3 \
    --require-allowlisted-signer
```

For SSH signatures, `git verify-tag` itself does the same check:

```bash
git -c gpg.ssh.allowedSignersFile=.reusable-ci/allowed_signers verify-tag v1.2.3
# exit 0 = signed and signer is in the file
# exit 1 = "No principal matched" or unsigned
```

For GPG signatures, `gpg --verify` produces a fingerprint you can grep:

```bash
git cat-file tag v1.2.3 | gpg --status-fd 1 --verify 2>/dev/null | grep VALIDSIG
# VALIDSIG <fingerprint> ... — must be a key committed in .reusable-ci/allowed_gpg_keys.asc
```

### Why a committed file, not a CI secret

- **Platform-neutral.** A repo cloned to GitLab or Forgejo carries its allowlist intact.
- **Auditable.** `git blame .reusable-ci/allowed_signers` tells you who added each entry and when.
- **Code-reviewed.** Adding a signer is a PR. The same branch-protection rules that protect `main` protect the allowlist.
- **Cryptographic, not nominal.** The match is on a signing-key fingerprint, not on the platform-issued `actor` string. The latter is spoofable in some contexts (forked-PR workflows, app-token impersonation); the former is bound to the human at the keyboard.

### Edge cases worth knowing

- **SNAPSHOT tags bypass the gate** by default. Any user with `tag.push` access can release `v1.2.3-SNAPSHOT`. This is by design — SNAPSHOTs are the "internal pre-release" channel. If you need to gate SNAPSHOTs too, sign every tag and require both files for every release.
- **Multi-module Maven**: the gate is per-release-tag, not per-module. One tag, one signer, one allowlist check.
- **Key rotation**: add the new entry, leave the old entry until everyone's switched, then remove it in a PR. `valid-before` lets you pre-stage a removal date.
- **The allowlist is missing but the gate is off**: tolerated, but **loudly** — a `::warning::` annotation and a summary callout flag that the release ran with no allowlist. The project hasn't opted in; turn on `require-authorization: true` or `release.requireallowlistedsigner: true` (the org default) to enforce.
- **Fingerprints-only list, signer's key not available**: with the gate **on**, a signature that can't be verified against any allowed key fails closed (exit 77) — commit the signer's public key to `allowed_gpg_keys.asc`. With the gate **off**, it's a loud warning, not a failure.

## Secret-handling model

The CI binary handles four classes of sensitive material during a release: the **GPG private key** (release-signing), the **GPG passphrase**, the **release token** (platform API access), and **registry passwords**. The defences are layered to keep each one inside the smallest possible blast radius.

### Argv hygiene

No secret value reaches `argv`. The CLI accepts secrets either via:

- a `--<name>-file` flag whose value is a path (`-` reads from stdin), or
- a documented environment variable (`GPG_PRIVATE_KEY`, `GPG_PASSPHRASE`, `RELEASE_TOKEN`, …).

Process-listing tools (`ps`, `/proc/<pid>/cmdline`, container introspection) never see the secret value. The discipline is documented in `internal/cli/secret/secret.go` and enforced consistently across every subcommand that touches a secret.

### In-process OpenPGP signer

`release sign` and `release sbom-zip --sign` use an in-process OpenPGP signer (`internal/adapters/openpgp/signer.go`, backed by `github.com/ProtonMail/go-crypto`). The private key:

- Lives only in heap memory for the lifetime of the signing process.
- Is never written to disk by reusable-ci.
- Is never spilled into `gpg-agent`'s cache (which would persist for the runner's lifetime — a vector for follow-on jobs on the same self-hosted runner).

`release gpg import` (used by `git tag -s` / `git commit -S` paths that shell out to the gpg CLI) sends the armored key to `gpg --import` **via stdin**, not a tmpfile. The key never lands on disk between our process and gpg's own keyring.

### Subprocess output redaction

`internal/safeexec/RedactKeyMaterial` scans every subprocess's
combined-output for PEM private-key markers (`BEGIN PGP PRIVATE KEY`,
`BEGIN OPENSSH PRIVATE KEY`, `BEGIN RSA PRIVATE KEY`,
`BEGIN EC PRIVATE KEY`, `BEGIN ENCRYPTED PRIVATE KEY`,
`BEGIN PRIVATE KEY`). If any marker is found, the entire output body
is replaced with a redaction notice before it propagates into an
error message. Defends against a future gpg / ssh-keygen / openssl
version that echoes input key material on stderr — current versions
don't, but the cost of the safety net is ~10 LOC and a small test.

### Process-level hardening (Linux)

At process startup, `safeexec.HardenProcess()` (called from `main()`) sets:

- `RLIMIT_CORE = 0` — the kernel won't write a core dump if the process crashes, so a segfault holding a decrypted key in heap never produces a disk file containing it.
- `PR_SET_DUMPABLE = 0` — additionally suppresses ptrace attach and core dumps regardless of RLIMIT. Defends against a low-privilege shell on the runner attaching gdb/strace to read decrypted key bytes from `/proc/<pid>/mem`.

Both are Linux-only; the helper is a no-op on other platforms (reusable-ci's CI runs on Linux runners, so portability isn't a goal here). Both are best-effort — kernels with seccomp filters that block the syscalls degrade silently, because logging the failure would leak runner configuration.

### Cleanup is always-on

Every workflow that imports a GPG key runs `reusable-ci release gpg cleanup` under `if: always()`:

- Deletes the secret half of the key from the gpg keyring.
- Deletes the public half.
- Kills `gpg-agent` (so any cached passphrase is gone).

Idempotent under failure; the cleanup runs even when the previous step exited non-zero.

### Inter-workflow data passing

The release pipeline crosses several `workflow_call` boundaries (orchestrator → prepare → build → publish → create-release). Data flowing across those boundaries is **either non-secret structured JSON** (config-plan / release-plan / publish-stage-plan) **or registered GitHub secrets** (`secrets.X`). Specifics:

- **JSON payloads** between stages contain artifact names, project types, working directories, publish-target enums, SBOM layer selections. Derived from the committed `.reusable-ci/artifacts.yml`; no field shape carries a secret value. The schema is in `internal/domain/pipeline/configplan.go`.
- **`$GITHUB_OUTPUT`** writes are limited to public identifiers — GPG fingerprints, key IDs, key UserID name/email, tag names, SHAs, basenames, container digests. No private material.
- **`$GITHUB_ENV`** is written once across the codebase: `build gradle-android decode-keystore` emits `ANDROID_KEYSTORE_PATH=<path>` — a filesystem path, not key bytes.
- **Secret-presence reporting** uses `${{ secrets.X != '' }}` — the workflow env receives a boolean ("is X configured?"), never the value.
- **`secrets: inherit`** between reusable workflows lets the callee read `secrets.X` by name; it does NOT automatically populate process env. Step-level `env:` declarations remain authoritative.

### Artifact visibility contract

Every `actions/upload-artifact` upload becomes downloadable for `retention-days`:

| Repo visibility | Who can download |
|---|---|
| Private | Members with read access |
| Public | Anyone (anonymous web users, after sign-in) |

The reusable-ci uploads, with their retention windows and content:

| Artifact | Retention | Content |
|---|---|---|
| Build artifacts (jar / tarball / APK / AAB) | 7 days | The compiled release artifact — designed to be public |
| Build-layer SBOMs (`bom.json`) | 7 days | Dependency list; no credentials |
| Analyzed-artifact SBOMs (SPDX / CycloneDX) | 7 days | Syft scan output; no credentials |
| Container digest markers | 1 day | Short ASCII digest IDs |
| Release artifacts bundle (`release-files/`, release notes) | 30 days | Files designed to attach to the GitHub Release |
| SARIF security reports | 5 days | Vulnerability findings; may contain code snippets, not credentials |

**Two rules that must hold for the upload-artifact contract to stay safe:**

1. **Globs must be narrow.** A `path: *.jar` is fine; `path: .` or `path: **` is not — those patterns pick up anything that happens to be in the working directory, including a misplaced keystore, certificate, or env-leaked secret file. The contract is locked by an integration test that scans every workflow YAML and rejects bare-dot and double-star patterns.
2. **Secret-bearing files live outside the project working directory.** `release.keystore` is decoded into `$RUNNER_TEMP` (or `os.MkdirTemp`), never cwd. The runner-temp path is cleaned between jobs on hosted runners. This is the v4 hardening — it removes the keystore from any path glob a future contributor might write.

### Containerfile secrets — caller responsibility

Containerfiles compiled by `publish-container.yml` get a forge-neutral **registry** layer cache (a dedicated `<registry>/<owner>/buildcache` package; opt out with `build-cache: false`). The cached layers live in the registry and may be readable to anyone who can read that package. **A Containerfile that writes a secret to a layer ships that secret to the cache** (and to the image). Always use:

```dockerfile
RUN --mount=type=secret,id=npmrc,target=/root/.npmrc \
    npm publish --access=public
```

…rather than baking the secret into a `RUN echo $SECRET >` step. The `--mount=type=secret` form gives the secret only to the running step, never persists it as a layer. See [Docker BuildKit secrets docs](https://docs.docker.com/build/building/secrets/).

### Threat model boundary

What this design defends against:

- **Argv inspection** by other processes on the runner. ✓
- **Disk forensics** on `/tmp` after the run. ✓ (Both the in-process signer and the stdin-import paths never write the key to disk.)
- **Stack-trace dumps** on panic. ✓ (Go's `debug.Stack` doesn't emit local-variable values; `os.Args` is the only argv we echo and secrets never reach it.)
- **Core dumps** capturing heap memory on segfault. ✓ (`RLIMIT_CORE=0` + `PR_SET_DUMPABLE=0`.)
- **ptrace attach** by another process on the runner. ✓ (`PR_SET_DUMPABLE=0`.)
- **gpg-agent persistence** across follow-on jobs. ✓ (Cleanup kills the agent.)
- **Future-tool stderr echoing** of input key bytes. ✓ (`RedactKeyMaterial` scrubs marker'd output.)

What this design does NOT defend against:

- **Swap-page extraction.** Addressed by refusing to run signing on swap-enabled hosts — see the [Swap policy](#swap-policy) below. The defence is policy-enforced, not cryptographic.
- **A root attacker on the runner.** A process running as root can read `/proc/<pid>/mem` regardless of `PR_SET_DUMPABLE` (the flag affects non-root ptrace; root bypasses it). The threat model assumes the runner OS itself is trusted.
- **Side-channel attacks** on the host CPU (Spectre, Rowhammer, etc.). Out of scope; mitigated by the runner platform.

### Swap policy

`release sign` and `release sbom-zip --sign` refuse to run when the kernel has an active swap area. The check reads `/proc/swaps`; if any line beyond the header is present, the binary exits `EX_CONFIG` (78) with an actionable error message.

**No CLI flag. No lookalike env-var bypass.** The fix is at the runner. The error message lists:

1. The one-line runner fix: `sudo swapoff -a` (persist in `/etc/fstab`).
2. Switching to a hosted runner (GitHub-hosted, GitLab-shared, Forgejo-Actions runners have swap disabled by default).
3. Architectural alternatives for operators who genuinely cannot disable swap: **Sigstore keyless** (`cosign sign` + OIDC; no long-lived private key) or **KMS-backed signing** (AWS KMS / GCP KMS / TPM; key never leaves the HSM). Both are external paths reusable-ci does not itself implement today — they run alongside the rest of reusable-ci's release flow.

**Debug-only override.** Local debugging sometimes needs to exercise the signing path on a developer laptop where swap is on for unrelated reasons. The narrow escape hatch is the `--debug-allow-swap` CLI flag:

```bash
reusable-ci release sign --debug-allow-swap ...
```

When set, the policy passes AND a loud `::warning::` annotation lands in the CI log naming the override and the risk. The flag is shown in `--help` (not hidden) on purpose: discoverability is the audit signal. A `--debug-allow-swap` in workflow YAML is reviewable in a PR; the flag must reappear in argv each invocation.

**The override is for local debugging only. Production releases must never set it.** A `::warning::` annotation on a production release is a release-quality bug; the operator should be running on a swap-off runner instead.

**Soft-pass on indeterminate state.** If `/proc/swaps` cannot be read (restricted containers, hardened sandboxes), the policy soft-passes. Refusing on indeterminacy would block legitimate restricted-container deployments where the visibility loss is structural; positive evidence is required to claim swap is active.

The policy is enforced in `internal/safeexec/swap_linux.go::RequireNoSwap`. Unit tests + black-box integration tests pin the contract:

- Active swap area → exit 78 with the documented error text.
- No swap area → normal signing flow.
- `/proc/swaps` unreadable → soft-pass.
- `--debug-allow-swap` bypasses with a loud `::warning::` annotation. The flag is the only opt-out surface; `safeexec.RequireNoSwap(bool)` takes the operator decision as a parameter, so the policy code has no env lookup at all.

The scope is narrow on purpose: only the two CLI paths that bring decrypted private-key material into the Go heap. `release gpg import`, `validate tag signature`, `version commit-push`, and so on are unaffected — they use public-key material or delegate to gpg-agent (libgcrypt's own secure-memory pool).

## Deterministic Pipeline

A deterministic pipeline produces consistent, repeatable verdicts: the same
inputs always yield the same outputs and the same pass/fail. reusable-ci is
built around this stance — the table below maps the standard principles
to where they live in this repo.

| Principle | How reusable-ci implements it |
|---|---|
| Version control everything | Reusable workflows live in this repo, not pasted into adopters'. Plans, configs, allowlists, and runtime-image Containerfiles are all checked in. |
| Lock dependency versions | Per-ecosystem lockfile presence is validated before every release (`validate cargo`, `validate prerequisites`'s lockfile branches). Cargo's `--locked` is enforced at fetch + compile. GitHub Actions are SHA-pinned with Renovate version comments. |
| Eliminate environmental variance | All build / test / publish jobs run inside reusable-ci-runtime container images. `runs-on:` is pinned to a specific Ubuntu major (`ubuntu-24.04`), not `ubuntu-latest`, so the runner doesn't roll forward silently. `SOURCE_DATE_EPOCH` is baked from `git log -1 --format=%ct HEAD` for Go + Cargo binaries AND for security-report timestamps (`reportTimestamp` in `internal/app/security/trivy.go`). |
| Remove human intervention | Tag push triggers `release-orchestrator.yml` end-to-end; there is no `workflow_dispatch` in the critical path. Release authorization is policy code (`allowed_signers` / `allowed_gpg_keys.asc` checked against the tag signature), not a human approval. |
| Fix flaky tests immediately | reusable-ci's own test suite is CI-pinned and required-green per release; the Go test conventions that keep it deterministic (parallel-safe fixtures, injected clocks, tool-presence skips instead of fails) are documented in [docs/testing.md](testing.md). On the adopter side the corresponding obligation is the same: a release whose tests sometimes pass and sometimes fail is not a deterministic pipeline. |

### What Renovate already pins

Three layers of dependency pinning ship in the inherited Renovate setup —
this is operative, not aspirational:

- **Third-party GitHub Actions** (`uses: actions/checkout@…`,
  `step-security/harden-runner@…`, etc.) carry `@sha256:…` digests plus
  `# vX.Y.Z` comments. Enforced by `pinDigests: true` on the
  `github-actions` manager in `local>diggsweden/.github:renovate-base`.
- **Containerfile FROM lines** (the runtime image build) are
  digest-pinned. `TestRuntimeContainerfileExternalFromImagesAreDigestPinned`
  rejects external bases without `@sha256:…`; Renovate's `dockerfile` manager
  updates the tag and digest under `pinDigests: true`. The Debian pin is held in
  the global `DEBIAN_VERSION` ARG consumed by `FROM`.
- **Pinned tool versions** in Containerfiles, workflow env defaults, and
  mise tool refs are updated via `# renovate: datasource=…` markers and
  customManagers — a Rust toolchain or `cyclonedx-gomod` bump goes
  through a reviewable PR, not a silent rebuild.

`container: image:` references in workflow YAML come from inputs
(caller-controlled), never hardcoded — there used to be one exception
(`lint-swift.yml`) which now reads from the standard `runtime-image`
input. The default values for those inputs are tied to this repo's own
release version (bumped at release time, not by Renovate).

### Trust boundaries

What's left after Renovate is one named choice, not a silent gap:

- **Self-published GHCR images** (`ghcr.io/diggsweden/reusable-ci-runtime-*`)
  are referenced by tag (`:v3.0.0`), not by digest. These tags come from
  workflow input defaults that this repo bumps at release time, not from
  Renovate. The trust model is registry-side immutability of our own
  org's tagged releases — we do not rewrite tags. Adopters who need
  stricter determinism may pin to a digest (`@sha256:…`) on their
  override of `runtime-image-*` inputs.

For the full picture of trust boundaries (consumer code, build extensions,
multi-tenant secret reuse, etc.), see [docs/threat-model.md](threat-model.md).

### Hard gates, no silent failures

Every "definition of deployable" check now fails the pipeline on
violation. Each has a visible, explicit opt-out so the decision to
release without the check is auditable.

| Gate | Default | Opt-out |
|---|---|---|
| **Build SBOM** generation (cyclonedx) | Required | `enable-build-sbom: false` per builder, or `release.sboms: none` at orchestrator |
| **Container vulnerability scan** (trivy, CRITICAL+HIGH) | Required | `enable-scan: false` to skip entirely, or `scan-severity: CRITICAL` to relax the threshold |
| **Artifact-presence verification** in publish-container | Required | Drop the `from:` entry in artifacts.yml so the verify step is skipped |
| **JVM reproducibility** (Maven `outputTimestamp`, Gradle archive task settings) | Required | Drop the artifact from the matrix; there is no per-artifact opt-out for the reproducibility invariant |
| **Cargo lockfile + toolchain pin** | Required | None — `Cargo.lock` and `rust-toolchain.toml` are mandatory for every Cargo artifact |

A passing pipeline now implies: artifacts built, signed, SBOM'd,
attested, reproducible, scan-clean. No `continue-on-error: true` on any
of the above. Adopters who need a non-default policy set it explicitly
per-call.

## Reproducible Builds

A reproducible build produces a byte-identical artifact every time the same source is built with the same toolchain. The artifact's SHA256 then becomes a meaningful fingerprint: any verifier can rebuild the tag from scratch and confirm the published artifact matches what the source declares it should be. Without reproducibility, SBOMs and signatures attest to *a* build, not *the* build the source implies.

### Per-ecosystem status

| Ecosystem | Artifact | Knob | Wired by reusable-ci? |
|---|---|---|---|
| Go (artifact-first OR container-first) | binary | `-trimpath -buildvcs=false` + `SOURCE_DATE_EPOCH` ldflag | Yes, automatic |
| Cargo (artifact-first) | release binary | `cargo build --release --locked --target <triple>` + `Cargo.lock` + `SOURCE_DATE_EPOCH` honoured via metadata derivation | Yes, automatic in `build-cargo.yml` |
| Cargo (container-first) | image-embedded binary | Stock cargo + `Cargo.lock` checked in | Caller-owned (validated via `validate cargo` across both build-modes) |
| Maven | main jar, sources jar | `<project.build.outputTimestamp>` in `pom.xml` | Caller-owned, **enforced** by `validate jvm-reproducibility` (release fails if missing) |
| Gradle (JVM + Android) | jar, war, distZip, distTar, APK | `preserveFileTimestamps = false` + `reproducibleFileOrder = true` on `AbstractArchiveTask` | Caller-owned, **enforced** by `validate jvm-reproducibility` (release fails if missing) |
| NPM | `.tgz` | npm ≥ 10 (fixed in npm/cli#3536) | Runtime image pins node 24 LTS (npm 10+) |
| Container image (both flows) | OCI image-config + layer blobs | `SOURCE_DATE_EPOCH` env on the build step (buildah `--timestamp`) | Yes, automatic (computed once in `prep` from `git log -1 --format=%ct HEAD`) |

The integration testsuite at [`diggsweden/reusable-ci-blackbox-tests`](https://github.com/diggsweden/reusable-ci-blackbox-tests) under `tests/integration/reproducibility_test.go` exercises every cell of this table on a clean checkout and asserts byte-identical SHAs across rebuilds. It runs in CI via the testsuite repo's own `Integration Tests` workflow — push/PR/nightly schedule, plus `workflow_dispatch` for ad-hoc validation against any reusable-ci ref.

### What `validate jvm-reproducibility` does

The release-orchestrator runs `validate prerequisites` before every release, and `prerequisites` now includes a `jvm-reproducibility` check when the plan contains a Maven, Gradle, or Gradle-Android artifact.

For each artifact it:

- Reads `pom.xml` (Maven) or `build.gradle{,.kts}` (Gradle) under the artifact's `working-directory`.
- For Maven: real XML parse looking for `<project.build.outputTimestamp>` under `<properties>`.
- For Gradle: line-by-line substring check for `preserveFileTimestamps = false` AND `reproducibleFileOrder = true`. Tolerates Kotlin and Groovy DSL forms, with or without whitespace around `=`.
- Emits a GitHub Actions `::error::` annotation when the setting is missing, prints an actionable fix snippet (the exact `<project.build.outputTimestamp>` or `tasks.withType(AbstractArchiveTask)` block to paste), and exits non-zero so the release stops at `validate prerequisites`.

The check is a hard gate — reproducibility is foundational to the deterministic-pipeline contract, so a non-reproducible JVM artifact cannot reach publish. To run standalone (e.g. from the CLI):

```bash
export CONFIG_PLAN_JSON="$(reusable-ci config parse-artifacts ...)"
reusable-ci validate jvm-reproducibility
```

#### Scope: each registered artifact's manifest is checked standalone

The validator iterates `artifacts.all[]` from the config plan and reads
`pom.xml` (or `build.gradle{,.kts}`) under each artifact's
`working-directory`. It does not compute Maven's *effective POM* — i.e. it
does not resolve `<parent>` chains across the workspace.

Practical implications:

- **Single-project Maven (typical case)**: one artifact entry, one pom.xml,
  one check. The validator's reading matches Maven's.
- **Multi-module Maven registered as one artifact** (pointing at the parent
  POM): the parent POM is the only one inspected, which is correct — the
  build is invoked at the parent and inheritance handles children.
- **Multi-module Maven where each child is registered as a separate
  artifact**: each child's pom.xml is inspected standalone. If a child
  relies on inherited `<project.build.outputTimestamp>` from a parent POM
  that is NOT itself a registered artifact, the validator will warn even
  though the effective build is reproducible. Workarounds:
    - Set `<project.build.outputTimestamp>` explicitly in each child POM
      (acceptable duplication; Maven supports either pattern).
    - Or register the parent POM as a `meta` artifact too, so the
      validator inspects it (the parent's property then satisfies the
      check at the parent level; children still warn unless they also
      declare the property explicitly).

This is a deliberate trade-off: computing the effective POM would require
shelling out to `mvn help:effective-pom`, adding a JVM dependency to the
validator path. The current standalone check covers the typical case
without that cost.

Gradle has the analogous gap: the validator inspects each artifact's
`build.gradle{,.kts}` standalone. If a multi-project build uses
`subprojects { tasks.withType<AbstractArchiveTask>().configureEach { ... }}`
in the root build script and registers only subproject directories as
artifacts (rare), the validator would miss the inherited config. The
substring heuristic does catch the typical case where the config block
lives in each subproject's own build script or in a shared convention
plugin applied per-project.

### Container reproducibility: what's stable, what isn't

`publish-container.yml` computes `SOURCE_DATE_EPOCH` once from `git log -1 --format=%ct HEAD` in the `prep` job and passes it as the env on both the main `reusable-ci container build` step and the optional `extract-binary` step. buildah reads it (via `--timestamp`) and:

- Writes the value into the OCI image-config `created` field.
- Pins every layer entry's `mtime` to it (`--timestamp` sets all timestamps to
  the value, a fully deterministic superset of an `mtime` clamp).

Result: two rebuilds of the same tag produce the same **image-config digest** and the same **per-layer blob digests**. This is the identity registries and verifiers care about (`crane manifest`, `cosign verify`).

Two things are NOT byte-stable, by design — verifiers must compare contents, not the wrapping bytes:

- **SLSA provenance attestation** (`enable-slsa: true`, signed via `reusable-ci container attest`) embeds the run-id, invocation, and build timestamps. The attestation differs every run; that's correct — it's identifying a specific build event, not the artifact.
- **Analyzed-container SBOM attestation** (`enable-analyzed-container-sbom: true`) uses syft, which generates fresh UUIDs and embeds scan time per run. The SBOM content (components, versions) is stable, but the document bytes are not.

### Non-issues that look like reproducibility bugs

- **Registry layer cache** (the dedicated `buildcache` package, tag-scoped per `(image, arch)`) keeps matrix legs from evicting each other. buildah pins the image-config `created` field and layer mtimes to `SOURCE_DATE_EPOCH` via `--timestamp`, so the image-config digest is stable whether the build hits or misses the cache.
- **`actions/upload-artifact`** wraps uploaded files in a zip whose internal timestamps drift across runs. The **content inside** is byte-identical; only the transport wrapper differs. When hashing artifacts handed off between stages, hash the unpacked content, not the bundle.

### Verifying reproducibility yourself

```bash
# 1. Re-run the same release build on a clean checkout.
git checkout v1.2.3 -- .
SOURCE_DATE_EPOCH="$(git log -1 --format=%ct HEAD)"

# 2. Rebuild your artifact with the ecosystem's repro knobs honoured
#    (see the per-ecosystem table above).
mvn -B -ntp clean package           # Maven
./gradlew clean jar                 # Gradle
SOURCE_DATE_EPOCH=$SOURCE_DATE_EPOCH docker buildx build --load -t test .

# 3. Compare SHAs.
sha256sum target/*.jar              # Maven JAR
sha256sum build/libs/*.jar          # Gradle JAR
docker image inspect test --format '{{.Id}}'  # container image config
```

If the SHAs differ on a tag that was published with reusable-ci ≥ v3 and the manifest opts into the repro knobs, that's a regression — please file an issue with the diff so the testsuite can pick it up.

## Application Configuration vs Environment Configuration

reusable-ci follows the [12-Factor App config split](https://12factor.net/config) — application configuration ships with the immutable artifact; environment configuration is supplied at deploy time and never bakes into the build. The split is enforced structurally:

| | Application configuration | Environment configuration |
|---|---|---|
| **What** | What to build, how to test, what to sign | Where to publish, who to push as, what credentials |
| **Where it lives** | `artifacts.yml`, workflow YAML defaults, runtime-image versions | Org secrets, workflow inputs, GitHub OIDC token |
| **Varies by env?** | Never — same value across staging + production | Per deployment target |
| **Travels in the artifact?** | Yes — baked into the binary's `main.version`/`main.commit`/`main.date` ldflags, the OCI image layers, the SBOM | No — passed through `secrets:` blocks at publish time, dropped after the step |
| **How reusable-ci enforces it** | `artifacts.yml` is parsed literally (no env-var expansion); `validateWorkingDirectory` rejects absolute paths / `${…}` refs / `..` escapes; the typed `PlannedArtifact` plan is pure-data with no secret material | every internal `workflow_call` site names the secrets it forwards (no `secrets: inherit` between reusable workflows); no secret ever passes through `with:` (would log to the run UI); `release sign` reads keys from env not argv |

**Concrete violations the validator now catches at parse-time** (in `validateWorkingDirectory`):

- `working-directory: /srv/build/svc` — absolute path, couples the build to a runner layout
- `working-directory: ${WORKSPACE}/svc` — shell-style ref that artifacts.yml does not expand
- `working-directory: ../outside-the-repo` — escapes the workspace, no longer reproducible from a fresh checkout

**Things that look like env config but aren't:**

- `version` / `commit` / `build-date` baked into binaries via ldflags — these *identify* the artifact, derived from the git tag + SHA + `SOURCE_DATE_EPOCH`. Same bytes on rebuild = same fingerprint, which is the whole point.
- `runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-*:v3.0.0` defaults — pinned to this repo's release version. The default *is* the application config; adopters override per-environment via workflow inputs.
- `github.run_id` in cross-job artifact-upload names — workflow-internal scoping, never reaches the released artifact.

**What adopters get for free:**

- An artifact that passes staging is byte-identical to what runs in production (reproducible builds + SBOM attestation)
- Rolling back = redeploy the previous tag's signed artifact; all bundled app config rolls back with it
- Environment-specific behavior changes (different DB URL, different feature-flag values per env) happen at the consumer's deploy step, not in the build pipeline — the artifact stays one

## Software Bill of Materials (SBOM)

Every release produces SBOMs at the three CISA layers the toolchain
can observe: Build (from the ecosystem's own dependency resolver),
Analyzed-Artifact (Syft scan of the built artifact), and
Analyzed-Container (Syft scan of the pushed image). The mapping
below names the tool used per layer per ecosystem.

### CISA SBOM Type Mapping

Layer names align with [CISA SBOM Types](https://www.cisa.gov/resources-tools/resources/types-software-bill-materials-sbom) (April 2023).

| CISA Type | Layer name | Tool / mechanism | Maven | NPM | Gradle | Gradle Android | Cargo | Go |
|-----------|------------|------------------|-------|-----|--------|----------------|-------|----|
| **Build** | `build` | Ecosystem CycloneDX tool | `cyclonedx-maven-plugin` | `@cyclonedx/cyclonedx-npm` | `cyclonedx-gradle-plugin` | `cyclonedx-gradle-plugin` | `cargo-cyclonedx` | `cyclonedx-gomod` |
| **Analyzed** | `analyzed-artifact` | Syft | JAR files | `.tgz` tarball | JAR files | Not generated today | extracted binaries | binaries |
| **Analyzed** | `analyzed-container` | Syft | container image | container image | container image | container image | container image | container image |

**Not implemented:** Source, Design, Deployed, and Runtime SBOM types are outside the current CI/CD scope. Build SBOMs supersede Source SBOMs for supported ecosystems because they capture the resolved dependency graph.

### 3-Layer SBOM Architecture

Every release includes up to **three layers** of SBOMs:

| Layer | Source | Captures | Use Case | Formats |
|-------|--------|----------|----------|---------|
| **Build** | Ecosystem CycloneDX tool (`bom.json`) | Precise build-time dependency resolution | Build reproducibility, dependency verification | CycloneDX 1.6 |
| **Analyzed Artifact** | Built artifacts (JAR, `.tgz`, extracted binaries) | Actual packaged libraries and runtime files | Runtime dependency verification, binary analysis | SPDX 2.3, CycloneDX 1.6 |
| **Analyzed Container** | Container image | OS packages, JRE, runtime environment | Deployment security, runtime vulnerability scanning | SPDX 2.3, CycloneDX 1.6 |

**Total SBOMs per release:** 5-9+ files (Build CycloneDX + analyzed layers in SPDX/CycloneDX, more if multiple artifacts are scanned)

### SBOM Naming Convention

SBOMs follow a consistent, CISA-aligned naming scheme. The short commit SHA is injected for traceability when run inside a git repo:

```text
<project>-<version>-<short-sha>-build-sbom.cyclonedx.json
<artifact-basename>-<short-sha>-analyzed-<artifact-type>-sbom.{spdx,cyclonedx}.json
<project>-<version>-<short-sha>-analyzed-container-sbom.{spdx,cyclonedx}.json

Examples:
- PROJECT-VERSION-abc1234-build-sbom.cyclonedx.json
- PROJECT-VERSION-abc1234-analyzed-jar-sbom.spdx.json        (library/fat JAR)
- PROJECT-VERSION-abc1234-analyzed-tararchive-sbom.spdx.json (npm tarball)
- PROJECT-VERSION-abc1234-analyzed-binary-sbom.spdx.json     (Rust/Go binary)
- PROJECT-VERSION-abc1234-analyzed-wheel-sbom.spdx.json      (Python wheel)
- PROJECT-VERSION-abc1234-analyzed-container-sbom.spdx.json
- PROJECT-VERSION-abc1234-analyzed-container-sbom.cyclonedx.json
```

`<artifact-basename>` is the basename of the scanned file (so multi-JAR projects get unique SBOMs per jar). The `build` layer uses `<project>-<version>` since one Build SBOM is produced per project, not per output file. The `analyzed-container` layer omits the artifact-type modifier (container is its own type).

**Multiple JAR Artifacts:**

Maven/Spring Boot projects may produce multiple JARs, each with its own SBOM:

| JAR Type | Filename | SBOM Size | Dependencies | Use Case |
|----------|----------|-----------|--------------|----------|
| **Library JAR** | `app-1.0.0.jar` | Small (5-10 KB) | ~2 packages (application code only) | Library consumers, Maven dependency |
| **Fat/Uber JAR** | `app.jar` | Large (500+ KB) | 100+ packages (all embedded deps) | Deployment, security scanning, runtime analysis |

The fat JAR SBOM shows the dependency tree deployed to production.

### Standards alignment

What the generated SBOMs satisfy, and where the line is drawn:

- **[NTIA Minimum Elements for SBOM](https://www.ntia.gov/sites/default/files/publications/sbom_minimum_elements_report_0.pdf)** — every SBOM carries supplier, component, version, dependencies, and unique identifiers.
- **[CISA SBOM types](https://www.cisa.gov/sbom)** — the three implemented layers (Build, Analyzed-Artifact, Analyzed-Container) map to the CISA taxonomy as in the table above; Source, Design, Deployed, Runtime are out of scope.
- **[EU Cyber Resilience Act (CRA)](https://digital-strategy.ec.europa.eu/en/policies/cyber-resilience-act)** — the produced SBOMs cover the transparency requirements for shipped artifacts. CRA also imposes obligations on update handling and incident reporting that live outside CI.
- **[SLSA Provenance](https://slsa.dev/spec/v1.0/levels)** — tag releases produce a SIGNED SLSA v1.0 build-provenance attestation (cosign, portable across registries/forges) when `enable-slsa: true`, verifiable with `cosign verify-attestation`. It is in-job-signed ⇒ ~SLSA Build L2; true L3 requires an isolated builder such as `slsa-github-generator` (GitHub-only, not portable, not adopted here).

### SBOM Delivery & Access

SBOMs are delivered in two ways:

#### 1. SBOM Archive (GitHub Release Asset)

All SBOMs packaged in a signed ZIP archive:

```bash
# Download complete SBOM package
gh release download v1.0.0 -p "*-sboms.zip"
gh release download v1.0.0 -p "*-sboms.zip.asc"

# Verify GPG signature
gpg --verify PROJECT-VERSION-sboms.zip.asc

# Extract all SBOMs
unzip PROJECT-VERSION-sboms.zip
```

Contents can include:
- Build SBOM (CycloneDX)
- Analyzed-artifact SBOMs for built artifacts (SPDX + CycloneDX)
- Analyzed-container SBOMs for published container images (SPDX + CycloneDX)

#### 2. Container Image Attestation (Analyzed-Container Layer)

`publish-container.yml` attaches the analyzed-container SBOM (CycloneDX, syft) as
a **signed cosign attestation** via `reusable-ci container attest --type
cyclonedx`. One attestation is bound to **each per-arch image digest** (the SBOM
differs per architecture), registry-attached and verifiable on any
registry/forge — no GitHub attestation API. Verify/download the platform image
digest that was built, not only the multi-arch manifest tag:

```bash
# Verify the signed SBOM attestation for a platform image digest
cosign verify-attestation \
  --type cyclonedx \
  --certificate-identity-regexp '^https://github.com/<owner>/<repo>/\.github/workflows/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/<owner>/<repo>@sha256:PLATFORM_DIGEST

# Pull the SBOM document itself out of the attestation
cosign download attestation --predicate-type=https://cyclonedx.org/bom \
  ghcr.io/<owner>/<repo>@sha256:PLATFORM_DIGEST \
  | jq -r '.payload | @base64d | fromjson | .predicate'
```

#### 3. Checksum and SBOM ZIP Verification

The release workflow creates the SBOM ZIP before generating checksums, so the
checksum manifest covers both normal release assets and the SBOM ZIP. Verify the
signed checksum manifest first, then verify release files with `sha256sum`. The
SBOM ZIP also has its own detached signature when release signing is enabled:

```bash
# Download checksums and signature
gh release download v1.0.0 -p "checksums.sha256*"

# Verify GPG signature on checksums
gpg --verify checksums.sha256.asc checksums.sha256

# Verify release asset and SBOM ZIP integrity for files you downloaded
sha256sum -c checksums.sha256 --ignore-missing

# Verify the SBOM ZIP signature when present
gh release download v1.0.0 -p "*-sboms.zip*"
gpg --verify PROJECT-1.0.0-sboms.zip.asc PROJECT-1.0.0-sboms.zip
```

### Using SBOMs for Security Analysis

Build SBOMs are generated with ecosystem CycloneDX tools. For analyzed-artifact and analyzed-container layers, CI uses **Syft** for generation and **Trivy** for scanning.

#### Vulnerability Scanning with Trivy

Trivy scans SBOMs for vulnerabilities and is used by the CI/CD workflows.

```bash
# Scan Build layer (declared dependencies)
trivy sbom PROJECT-VERSION-abc1234-build-sbom.cyclonedx.json

# Scan analyzed-artifact layer - JAR (library or fat)
trivy sbom PROJECT-VERSION-abc1234-analyzed-jar-sbom.spdx.json

# Scan analyzed-container layer (runtime environment)
trivy sbom PROJECT-VERSION-abc1234-analyzed-container-sbom.spdx.json

# Scan with severity filtering
trivy sbom --severity HIGH,CRITICAL PROJECT-VERSION-abc1234-analyzed-container-sbom.spdx.json

# Output as JSON for processing
trivy sbom -f json -o vulnerabilities.json PROJECT-VERSION-abc1234-analyzed-jar-sbom.spdx.json
```

#### License Compliance Analysis

```bash
# Extract license information from SBOM
jq '.packages[] | {name: .name, version: .versionInfo, license: .licenseConcluded}' \
  PROJECT-VERSION-abc1234-analyzed-jar-sbom.spdx.json

# Scan for license issues with Trivy
trivy sbom --scanners license PROJECT-VERSION-abc1234-build-sbom.cyclonedx.json
```

#### Dependency Graph Visualization with Syft

Syft generates SBOMs and can display dependency information.

```bash
# Generate dependency tree from SBOM
syft packages PROJECT-VERSION-abc1234-analyzed-jar-sbom.spdx.json -o table

# Export to dependency graph format
syft packages PROJECT-VERSION-abc1234-build-sbom.cyclonedx.json -o json | \
  jq '.artifacts[] | {name: .name, version: .version, type: .type}'
```

### SBOM Generation Workflow

SBOMs are generated and packaged automatically during the release process:

1. **Build workflows** → Generate Build SBOMs when the effective `sboms` includes `build`
2. **Release SBOM step** → Generates analyzed-artifact SBOMs for built artifacts
3. **Container publish** → Generates analyzed-container SBOMs for pushed images
4. **Release assembly** → Stages the canonical release assets and SBOM inputs in `release-files/`
5. **SBOM ZIP** → Packages all selected SBOM inputs into a ZIP archive
6. **Checksums** → Hashes staged release assets and the SBOM ZIP
7. **Signing** → Signs staged release assets, the SBOM ZIP, and checksums
8. **Upload** → Uploads the staged assets, signatures, SBOM ZIP, and checksums to the release
9. **Attestation** → Container SBOM (CycloneDX) and SLSA provenance attached to the image as signed cosign attestations via `reusable-ci container attest`

### SBOM Format Comparison

| Feature | SPDX 2.3 | CycloneDX 1.6 |
|---------|----------|---------------|
| **Standards Body** | Linux Foundation | OWASP |
| **Primary Use** | License compliance, legal | Security, vulnerability management |
| **Vulnerability Mapping** | CPE, PURL | CPE, PURL, SWID |
| **License Expression** | SPDX License List | SPDX License List |
| **Tool Ecosystem** | Broader legal/compliance tools | Security-focused tools (Dependency-Track) |
| **Government Adoption** | NTIA recommended | CISA recommended |

Both formats are produced for every layer so consumers can pick whichever their tooling already speaks.

### References & Further Reading

- [NTIA SBOM Minimum Elements](https://www.ntia.gov/sites/default/files/publications/sbom_minimum_elements_report_0.pdf)
- [CISA SBOM Guidance](https://www.cisa.gov/sbom)
- [CISA SBOM Types](https://www.cisa.gov/resources-tools/resources/types-software-bill-materials-sbom)
- [EU Cyber Resilience Act](https://digital-strategy.ec.europa.eu/en/policies/cyber-resilience-act)
- [SPDX Specification 2.3](https://spdx.github.io/spdx-spec/v2.3/)
- [CycloneDX Specification 1.6](https://cyclonedx.org/specification/overview/)
- [SLSA Framework](https://slsa.dev/)
- [Syft SBOM Generator](https://github.com/anchore/syft)
- [Trivy Vulnerability Scanner](https://github.com/aquasecurity/trivy)

## Full Verification Guide

### 1. Container Image Verification

Verifies the image's identity and that it came out of this repository's CI.

#### Verify SLSA Provenance

SLSA v1.0 build provenance is attached to the OCI image as a **signed cosign
attestation** by `reusable-ci container attest` (the predicate is generated from
the CI environment). It is registry-attached and verifiable on **any** registry
or forge — the primary, portable path:

```bash
cosign verify-attestation \
  --type slsaprovenance1 \
  --certificate-identity-regexp '^https://github.com/diggsweden/.+' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/diggsweden/my-app@sha256:PLATFORM_DIGEST
```

Manifest-list SLSA provenance is generated for tag builds with
`enable-slsa: true`, on any OCI registry the publish stage targets. `--recursive`
(default) attests each per-arch child too, so a per-platform digest verifies as
well.

Because this provenance is signed, production planning requires release artifact
signing plus a cosign-capable `sign.method` (`sigstore` or `kms`) whenever a
pushed container has `enable-slsa: true`. Disabled or GPG-only signing is
rejected before build or registry work.

This is the single, portable provenance path — reusable-ci deliberately does not
also push to GitHub's attestation store (`actions/attest-build-provenance`),
which is GitHub-only and would be a redundant second copy. GitHub consumers run
the same `cosign verify-attestation` as everyone else.

#### Verify / inspect the SBOM

The analyzed-container SBOM (CycloneDX, syft) is a **signed cosign attestation**
bound to each per-arch image digest. Verify and pull it from the registry on any
forge (no GitHub attestation API needed):

```bash
# Verify the signed SBOM attestation
cosign verify-attestation --type cyclonedx \
  --certificate-identity-regexp '^https://github.com/diggsweden/.+' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/diggsweden/${PROJECT}@sha256:PLATFORM_DIGEST

# Pull the CycloneDX document out of the attestation
cosign download attestation --predicate-type=https://cyclonedx.org/bom \
  ghcr.io/diggsweden/${PROJECT}@sha256:PLATFORM_DIGEST \
  | jq -r '.payload | @base64d | fromjson | .predicate'

# The same syft SBOM is also published as downloadable release assets (SPDX +
# CycloneDX); download them straight from the release
gh release download "$TAG" --pattern '*-analyzed-container-sbom.*'
```

### 2. Maven Artifact Verification

Verifies release signature and artifact integrity.

#### Verify GPG Signature

```bash
# Import the publishing key (Digg OSPO bot)
curl -sSfL https://github.com/diggsweden/.github/raw/main/pubkey/ospo.digg.pub.asc -o ospo.digg.pub.asc

# Verify fingerprint before importing
gpg --show-keys ospo.digg.pub.asc
# Expected: 94DC AF60 8AA5 3E16 4F94 F2C8 5D23 336A 384E D816

gpg --import ospo.digg.pub.asc

# Download artifact and signature from GitHub Packages
curl -H "Authorization: token GITHUB_TOKEN" \
  -L https://maven.pkg.github.com/diggsweden/${PROJECT}/${ARTIFACT}/${VERSION}/${ARTIFACT}-${VERSION}.jar \
  -o ${ARTIFACT}.jar

curl -H "Authorization: token GITHUB_TOKEN" \
  -L https://maven.pkg.github.com/diggsweden/${PROJECT}/${ARTIFACT}/${VERSION}/${ARTIFACT}-${VERSION}.jar.asc \
  -o ${ARTIFACT}.jar.asc

# Verify signature
gpg --verify ${ARTIFACT}.jar.asc ${ARTIFACT}.jar
```

### 3. NPM Package Verification

Verifies that the expected package/version exists in the intended registry and
that npm reports registry integrity metadata. Current workflows do not publish
npm provenance or package attestations.

```bash
# Inspect registry metadata and integrity hash
npm view @${GITHUB_REPOSITORY_OWNER}/${PACKAGE}@${VERSION} \
  version dist.integrity dist.tarball \
  --registry=https://npm.pkg.github.com

# Dry-run package resolution without installing
npm pack @${GITHUB_REPOSITORY_OWNER}/${PACKAGE}@${VERSION} \
  --registry=https://npm.pkg.github.com \
  --dry-run
```

### 4. Release Artifact Verification

Verifies release artifact authenticity and signatures.

#### Download and Verify Release Assets

```bash
# Set version
VERSION="v1.0.0"

# Download release asset and checksums
gh release download ${VERSION} -p "*.tar.gz"
gh release download ${VERSION} -p "checksums.sha256"
gh release download ${VERSION} -p "checksums.sha256.asc"

# Verify GPG signature on checksums
gpg --verify checksums.sha256.asc checksums.sha256

# Verify file integrity
sha256sum -c checksums.sha256 --ignore-missing
```

#### Verify the binary SLSA provenance (portable, cosign)

Binary releases carry a **signed SLSA v1.0 provenance** — generated by
`reusable-ci release provenance` and signed keyless with `cosign sign-blob`
(the portable, forge-neutral replacement for `actions/attest-build-provenance`;
no GitHub attestation API). It ships as two release assets: the in-toto
statement (`*.intoto.json`) and its Sigstore bundle (`*.intoto.jsonl`).

```bash
gh release download ${VERSION} -p "*.intoto.json" -p "*.intoto.jsonl"

cosign verify-blob \
  --bundle reusable-ci.intoto.jsonl \
  --certificate-identity-regexp '^https://github.com/diggsweden/reusable-ci/\.github/workflows/release-binary\.yml@refs/heads/main$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  reusable-ci.intoto.json
```

The statement's `subject` digests come from `checksums.txt`, so a verified
statement transitively vouches for every tarball + SBOM listed there. The same
cosign bundle on `checksums.txt` answers "is this the authentic release"; the
provenance answers "which workflow + commit produced it".

### 5. SSH Key Setup for Git Verification

SSH signature verification requires configuring Git with signer public keys.

#### Configure SSH Signature Verification

```bash
# Download a user's SSH public keys from GitHub
curl https://github.com/<username>.keys -o /tmp/<username>.keys

# Configure Git to use the allowed signers file
git config gpg.ssh.allowedSignersFile ~/.ssh/allowed_signers

# Add the user's keys to allowed signers
# Format: <email> <key-type> <public-key>
echo "developer@example.com $(cat /tmp/<username>.keys)" >> ~/.ssh/allowed_signers

# For multiple keys from the same user, add each on a separate line
while IFS= read -r key; do
  echo "developer@example.com $key" >> ~/.ssh/allowed_signers
done < /tmp/<username>.keys

# Example: trust the diggsweden publishing bot
curl https://github.com/diggsweden-bot.keys -o /tmp/diggsweden-bot.keys
echo "ospo@digg.se $(cat /tmp/diggsweden-bot.keys)" >> ~/.ssh/allowed_signers
```

### 6. Git Tag Verification

Verifies tag signatures and authenticity.

```bash
# Fetch tags
git fetch --tags

# Verify GPG signed tag
git verify-tag v1.0.0

# Verify SSH signed tag (requires SSH key setup from section 5)
git verify-tag v1.0.0 --raw
```

### 7. Git Commit Verification

Verifies developer identity via GPG or SSH signatures. SSH signature verification requires SSH key setup from section 5.

```bash
# Verify a specific commit signature
git verify-commit <commit-hash>

# Show commit signature details
git show --show-signature <commit-hash>

# List commits with signature status
git log --show-signature

# Check signature status in one-line format
git log --pretty="format:%h %G? %aN %s" --abbrev-commit
# Where %G? shows: G=good GPG, B=bad GPG, U=untrusted GPG, X=expired GPG, Y=expired key GPG, R=revoked key GPG, E=missing key, N=no signature

# Verify SSH signed commits (requires SSH key setup from section 5)
git verify-commit <commit-hash> --raw

# Configure git to show signatures by default
git config --local log.showSignature true
```

#### Automated Commit Verification

```bash
# Verify all commits in a branch
git log --format='%H' origin/main..HEAD | while read commit; do
  echo "Verifying $commit..."
  git verify-commit $commit || echo "WARNING: Unsigned commit $commit"
done

# Ensure all commits in PR are signed
git log --format='%G? %h %s' origin/main..HEAD | grep -E '^[NBU]' && echo "Found unsigned commits!" && exit 1 || echo "All commits signed"
```

### 8. Podman/Docker Inspection

Verify the SLSA/SBOM attestations first, then pull and inspect the image:

```bash
# Pull after attestation verification
podman pull ghcr.io/diggsweden/${PROJECT}:${VERSION}

# Inspect image manifest/config
skopeo inspect --raw ghcr.io/diggsweden/${PROJECT}:${VERSION}
```

### 9. Getting Public Keys from GitHub

GitHub provides access to users' public keys:

- **GPG keys**: `https://github.com/<username>.gpg`
- **SSH keys**: `https://github.com/<username>.keys`

```bash
# Download GPG public keys
curl https://github.com/<username>.gpg | gpg --import

# Download SSH public keys
curl https://github.com/<username>.keys >> ~/.ssh/allowed_signers
```

## Useful Tools

### Install Verification Tools

Using [mise](https://mise.jdx.dev/) with aqua backend:

```bash
# Install tools via mise with aqua backend
mise use -g aqua:sigstore/cosign       # signature + attestation verifier
mise use -g aqua:cli/cli               # gh — release asset download / git verification
mise use -g aqua:containers/skopeo

# Verify installation
cosign version
gh --version
skopeo --version
```

## Additional Resources

### Container & Supply Chain Security

- [Sigstore Documentation](https://docs.sigstore.dev/)
- [SLSA Framework](https://slsa.dev/)
- [GitHub Artifact Attestations](https://docs.github.com/en/actions/concepts/security/artifact-attestations)
- [SBOM (CycloneDX) Specification](https://cyclonedx.org/)
- [Skopeo Documentation](https://github.com/containers/skopeo)
- [Podman Image Trust](https://docs.podman.io/en/latest/markdown/podman-image-trust.1.html)

### Code Signing & Verification

- [Git Signing Documentation](https://git-scm.com/book/en/v2/Git-Tools-Signing-Your-Work)
- [GitHub SSH Commit Verification](https://docs.github.com/en/authentication/managing-commit-signature-verification/about-commit-signature-verification#ssh-commit-signature-verification)
- [GPG Best Practices](https://www.gnupg.org/documentation/manuals/gnupg/OpenPGP-Key-Management.html)
- [Maven GPG Plugin](https://maven.apache.org/plugins/maven-gpg-plugin/)

### GitHub Security Features

- [GitHub Security Hardening](https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions)
- [GitHub OIDC Token](https://docs.github.com/en/actions/deployment/security-hardening-your-deployments/about-security-hardening-with-openid-connect)
- [GitHub Packages Authentication](https://docs.github.com/en/packages/learn-github-packages/introduction-to-github-packages#authenticating-to-github-packages)
