# Verification Guide

How to verify the artifacts these workflows produce (checksums,
signatures, SBOM attestations, and SLSA provenance), and how the
matching guarantees are generated on the producing side.

> **Unpublished v3:** `@v3.0.0` references below are prospective. Until the tag
> and matching runtime images exist, use the reviewed-branch procedure in
> [Runtime Images](runtime-images.md); do not substitute an unreviewed moving
> ref in production.

## Code Quality Verification

### Linting

PR linting runs through `lint-nanolinter.yml` (called automatically by the PR
orchestrator when `lint-engine: nanolinter`, the default). It runs `nanolinter
verify` inside the nanolinter flavour image (which bakes nanolinter and its
check toolchain) against your project's `nanolinter.toml` verify plan. Security
findings (SAST, dependencies, secrets) are uploaded to GitHub Code Scanning as
SARIF. Swift/iOS projects additionally run `lint-swift.yml` on macOS.

A **mandated lint floor** is enforced on every run: the job passes `nanolinter
verify --require "$REQUIRED_LINTS"` (default `secrets,sast`), forcing those
checks to run *and* block regardless of the project's `nanolinter.toml`. A
project cannot drop SAST or secret detection. It can only add to the floor
(by `extends`-ing a base with `[policy].require`), never remove from it. The
set is the `required-lints` orchestrator input.

Your project needs a `nanolinter.toml` verify plan; the flavour image provides
the tools, so no `just`/`justfile` is required. (A local `.mise.toml` + `just`
recipe is optional, for running the same checks on a developer's machine.) The
alternative engine, `lint-engine: megalinter`, reads a `.mega-linter.yml`
instead. See the [examples](../examples/).

### Scan Report Lifecycle

The engine's OpenGrep, container and dependency scan commands treat configured
report paths as outputs of the **current invocation**, not a last-success cache.
Refused engine configuration/path preflight leaves previous reports untouched. After preflight,
previous configured reports are retired and the scanner writes into private
run-owned scratch space. Only fresh, bounded, nonlinked reports are published;
JSON reports must be objects and pass the applicable domain parser.

A zero process exit without a report, or an empty/unrelated JSON object, is not
a clean scan. Trivy recognition requires a `Results` array (empty is supported)
or a nonblank `ArtifactName`. With a name, omitted and null `Results` remain
supported. Consumed fields must have the expected types; unknown fields are
allowed. Raw-container and image-evidence checks use the same parser. This is a
bounded recognition rule, not native-version schema certification or report
authentication; complete native-envelope validation remains separate from
freshness. Clean container scans publish fresh raw JSON but no secondary
reports. Dependency conversions remain best-effort:
failed, missing or malformed conversion output is not published. OpenGrep text
is cosmetic, so absent/unreadable text cannot reuse an earlier excerpt.

Reports are staged and installed individually, not as a transaction across the
report set or with guaranteed uninterrupted visibility or rollback after arbitrary
I/O failure. Consumers must honor the command outcome; do not treat files preserved
by a refused preflight as evidence from a new scan.

Standalone Trivy-to-GitLab/SARIF transforms validate paths and input before
opening their output. An input/path refusal preserves an existing destination
and does not create an absent one. Successful conversion replaces the selected
output; arbitrary later I/O failure is not covered by a rollback guarantee.

### Summary Metadata

Go, Android and Xcode build summaries, extracted-binary listings and Google Play
upload summaries render supplied metadata as literal inline data. Each Unicode
control character becomes one ASCII space, so CRLF becomes two spaces. Active
Markdown/HTML characters and automatic-link syntax are encoded in heading,
prose and table values; other Unicode and ordinary spaces are retained.

Artifact names and discovered paths use complete inline-code values. Simple
values retain Markdown backticks; empty values, boundary spaces, backticks and
table delimiters use HTML code with encoded content. Rendering does not change
raw-input branch decisions, artifact identity or selection. These protections
cover the named renderers and their inline contexts, not every report type,
arbitrary Markdown source, link destinations, HTML attributes or fenced blocks.

Extracted-file listings use the requested positive entry limit; zero and negative
limits default to 50. Fifty is not a maximum for explicit positive limits. The
listing selects globally sorted raw paths before rendering, with best-effort
discovery of empty or missing trees. Discovery still walks and retains the full
candidate set before sorting. The entry cap is not a traversal, memory, time or
report-byte budget, nor a guarantee that every resulting report is renderable.

### Summary Publication

Stage-result publication writes the manifest, canonical scalar outputs, and then
optional JSON output in that order. A collection or publication error stops later
calls and returns no stage envelope. Earlier writes remain, and a real failing
sink may itself have partially written. In particular, an optional-output failure
can leave the manifest and canonical success outputs while the command fails.
Consumers must honor the command outcome rather than infer success from those
partial outputs. Snapshot-summary completion is announced only after its summary
append succeeds. No cross-sink rollback is promised.

The file-backed result store treats stage/job names as flat filename stems, not
paths. It rejects empty/whitespace-only names, dot/parent names, separators,
controls and invalid UTF-8 before marshaling or filesystem effects. `job-result`
also validates before calling its store, retaining its existing outer-whitespace
trimming. Case, internal spaces, Unicode and safe punctuation are not sanitized.

Writes use checked directory roots and single-record staging. Existing linked
directory components, linked leaves and nonregular targets are refused. Regular
replacement preserves permission bits without changing an unrelated hardlinked
inode; new files use `0644` subject to the creation umask. Collection remains flat,
treats a missing jobs directory as empty, and reads only nonlinked regular JSON
files through the 64 MiB per-file reader. It returns no partial document list on
failure. Hardlinked regular inputs remain accepted; there is no aggregate
count/byte cap, hostile concurrent-leaf guarantee or whole-store transaction.

Within a selected job-map value, the consumed `result` member may appear at most
once. Stored job records apply the same rule to `version`, `job` and `result`,
including case-insensitive aliases recognized by the typed JSON decoder. Equal
duplicates and null-plus-value duplicates are ambiguous too. Repeated outer job
names remain separate records for conservative aggregation; additive unknown
members and nested metadata are not recursively subjected to this singleton
policy. Existing typed and status validation still applies.

Generic stage-plan stage and target names must already have their canonical
spelling; surrounding whitespace is rejected before result-source access.
Explicit result, Extra and active optional-JSON pair keys retain outer-whitespace
trimming. Validated copied keys are used for publication, preserving values,
Extra order and caller-owned slices. Extra and active optional-JSON key checks
precede collection or map parsing; inactive JSON fields remain ignored. This
does not change filename-stem rules, status normalization or adapter-specific
output-name mappings, and is not full stage-plan/envelope schema validation.

## Release Plan Preflight

Enabled version-bump plans check all declared engine-owned primary version files
against the full changelog input and every planned changelog destination before
mutation. Equal normalized paths and existing file identities, including
hardlinks and resolved changelog-input symlinks, cannot serve conflicting roles.
Cargo TOML and property writers cannot share a primary file. Repeated same-writer
targets, compatible distinct-property writers, and changelog-only sharing remain
supported, including the default root changelog serving as input and output.

These read-only checks assume one writer and stable paths. They do not enumerate
native Maven/npm-selected outputs, Cargo lock-refresh outputs or aliases beyond
the declared inventory, and do not promise rollback of later tool, filesystem or
sink failures.

## Runner Tool Exposure

Setup jointly checks runner PATH/ENV files, their identities, serialized entries
and planned directories before creating or appending. Changelog installation
also preflights run IDs, install/output/reset overlaps and archive URL syntax;
mirror base URLs are directory prefixes without queries or fragments. Only the
trusted OS-temp fallback is canonicalized through ordinary OS aliases. Explicit
linked destinations remain refused. Later failures can retain completed state;
append-only reruns are not transactions.

`toolchain expose-mise-tools` validates the combined mise/rustup selection before
publishing links or PATH entries. Absolute paths and legitimate source symlinks
are supported; their authority comes from the selected mise binary/configuration,
not from a claim that all tools must live inside the checkout.

Exposure is create-only. An existing destination resolving to the selected file
is a no-op. Conflicting basenames, unrelated regular files and different-target
links are refused rather than overwritten. This also means an old-version link
is not automatically replaced: its owner must resolve the conflict explicitly.
New links are created without clobbering an occupied leaf, and PATH remains
append-only. Later I/O failures may leave earlier newly created state; this is
not an all-files transaction or executable-content authentication mechanism.

The changelog renderer's final link/PATH publication uses the same checks for
exactly one selected executable, without exposing sibling tools. Relative bin
homes are resolved once; the published destination, exported PATH directory and
returned executable path all use that absolute value. Exported directories
cannot contain PATH-list separators, NUL, CR, LF or tab.

This preservation guarantee starts at publication entry. Earlier directory
creation, mise installation and scratch reset are separate operations. The
publisher does not authenticate the source or prove its membership in an
isolated mise installation layout; the public installation lifecycle and later
failure recovery are not established by these publication checks.

Separate hermetic public-flow tests cover download, adoption, isolated setup,
install/where requests, literal source lookup, publication and version execution
using an in-memory transport, recording runner and owned Go test child. They do
not run real mise or changelog binaries, prove source authenticity, or establish
whole-install rollback. Append helpers propagate write and close errors; delayed
close failures have static rather than injected runtime evidence.

## Build Metadata

Integrated Android signing requires explicit keystore password, alias and key
password inputs, bound from the existing CLI environment names. Password bytes
are preserved; empty or NUL-bearing credentials and whitespace-only aliases
refuse before effects. Gradle build and SBOM calls receive the same scoped
signing overrides and selected project directory without changing parent
environment variables. Inherited JDK/SDK runtime settings remain available;
this is not an environment sandbox or a guarantee that an unsigned request
cannot inherit existing ambient credentials.

The integrated keystore lives in private owned scratch through build and SBOM.
Injected `secrets.properties` is create-only: an occupied destination refuses,
and cleanup preserves a caller's replacement rather than deleting it. Cleanup
errors join primary errors and may leave explicitly reported owned residue.
Standalone secret writers retain their replacement behavior. Signing scratch
and the independently selected SBOM temp parent are preflighted separately;
init-script cleanup failures are propagated, not silently treated as success.

Android Build SBOM summaries recognize both `build/reports/bom.json` and
`build/reports/cyclonedx/bom.json`, including module-local reports. Discovery of
these files does not establish freshness, native plugin compatibility or subject
identity; the command outcome and its other verification gates still apply.

Maven release builds load and check local POM coordinates before installation,
including supported parent group/version fallback and the XML document envelope.
Captured literals are retained, but native property evaluation deliberately stays
after install and remains CWD-bound; lifecycle CLI options are not forwarded to
the existing evaluator. This is not pre-effect effective-model validation or
Maven schema interpretation.

Npm release preflight rejects blank required identities and invalid selected
build-script values before installation. It does not inspect every unused script
as an early gate: postinstall repairs and script selection changes remain
supported. The existing later script-map check can still reject unrepaired
invalid entries. No additional npm version or native workspace grammar is imposed.

Xcode metadata selectors resolve relative to the metadata root, defaulting to CWD.
An explicit workspace must identify one project through the supported group-relative
XML subset, otherwise it refuses instead of using an unrelated repository project.
Selector-free discovery retains its historical first-project behavior. The emitted
values are source PBX metadata, not proof of effective scheme/configuration settings
or native archive contents; a build-number override remains a separate archive input.

Integrated Xcode release validation decodes required signing/export blobs and a
provided xcconfig through the bounded nonempty mobile-secret reader before
reporting or tool calls. Blank optional xcconfig remains a no-op, and unsigned
commands do not resolve unused signing password files. Standalone archive calls
check a supplied readable regular xcconfig before creating output or invoking
the tool. These are input checks, not native-format or cryptographic validation.

Xcode security-command failures render only a trusted stage description, not the
opaque dependency error that may include passwords or certificate/profile data.
Original causes remain available through Go error unwrapping for `errors.Is` and
`errors.As`; they are not sanitized for display. Render the outer error rather
than logging recovered causes. This protects ordinary app diagnostics, not the
process argument list: the current macOS security commands still receive signing
passwords in argv. Signing-state teardown is a separate, unfinished guarantee.

Xcode release outputs are create-only. `build/app.xcarchive` must be absent before
the build; signed `build/export` must be absent or an empty real directory.
Success requires a new archive directory and, for signed builds, exactly one
nonempty regular file directly under `build/export` with a lowercase `.ipa`
suffix, matching the shipped upload pattern. Nested/uppercase, multiple, missing
and linked results are refused before job-output publication. Unrelated build
siblings are preserved, and standalone informational listing is unchanged.
Archive internals, effective native bundle identity and signing-state teardown
are not established by this output gate; failed builds may leave their new
outputs for explicit owner cleanup before retry.

Canonical ConfigPlans used by assembly, artifact-SBOM generation, prerequisites
summaries and Cargo/JVM checks pass the same bounded projection validator used
by release/snapshot planning before consumption. Inactive/optional inputs retain
their documented no-op behavior; an invalid selected plan is not repaired or
replaced by a fallback. This does not establish a universal schema, duplicate-key
policy or provider/runtime transfer identity.

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

`reusable-ci release sign` supports three signing backends. Pick one per repo via `--method` (or `sign.method` in `.reusable-ci/artifacts.yml`). All three produce verification material the consumer reads via `reusable-ci validate artifact-signature`, which auto-detects the method from the sidecar layout (`.asc` vs `.bundle`).

| | `--method=gpg` (default) | `--method=sigstore` | `--method=kms` |
|---|---|---|---|
| Private key lifetime | long-lived (operator-managed) | ephemeral (~10 min) | long-lived, never leaves the KMS/HSM |
| Identity claim | "someone holding key X" | "this workflow ran this commit" | "someone authorized to call KMS key Y" |
| External-service dependency | none | Fulcio + Rekor (Sigstore public infrastructure) | KMS provider (or self-hosted OpenBao) |
| Public Sigstore dependency | none | Fulcio + Rekor + TUF | configurable; `transparency: none` avoids public Sigstore but still requires the KMS endpoint |
| Sidecar file | `<art>.asc` | `<art>.bundle` (v3 Sigstore bundle) | `<art>.bundle` (v3 Sigstore bundle) |
| Consumer command | `gpg --verify` (universal) | `reusable-ci validate artifact-signature --cert-identity-regexp=...` | `reusable-ci validate artifact-signature --key=...` |
| Swap policy applies | yes (decrypted key in heap) | no | no |

The cosign methods share infrastructure: cosign 3.x is baked into the runtime image and emits the [Sigstore v3 bundle format](https://docs.sigstore.dev/cosign/key_management/overview/) (one JSON file containing signature, optional Fulcio cert, and Rekor proof) regardless of backend.

### `--method=gpg`: long-lived OpenPGP key (default)

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

This is the lowest-friction backend for downstream consumers, because gpg is in every distro. It also has the heaviest operator burden. The long-lived private key requires rotation discipline, swap-page-safe handling (see [Swap policy](#swap-policy)), and a distribution mechanism for the matching pubkey.

### `--method=sigstore`: Sigstore-keyless via OIDC + Fulcio + Rekor

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
| Forgejo | `$FORGEJO_*` | identity is resolved, but the issuer is left empty, because public Fulcio does not trust a Forgejo issuer, so supply `--oidc-issuer` + a trusting Fulcio explicitly |

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
explicitly. Explicit flags always win over the derived defaults:

```bash
reusable-ci validate artifact-signature \
    --artifact app.tgz \
    --cert-identity-regexp '^https://github.com/<owner>/<repo>/' \
    --cert-oidc-issuer 'https://token.actions.githubusercontent.com'
```

The cert-identity regexp is the load-bearing trust claim: "I trust signatures from workflows in this repo." The CI default derives it from the trusted runner environment and anchors it to the repository. That is safer and less error-prone than a hand-typed pattern, which can silently be too loose. Branch protection and required reviews on the producing repo *become* the signing-security posture: forging a signature means getting code to run as the release workflow.

### `--method=kms`: cosign with explicit key reference

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

OpenBao uses cosign's Vault-compatible KMS provider. Set `VAULT_ADDR` and supply
a short-lived `VAULT_TOKEN`; an organization-owned wrapper may obtain that token
by exchanging the runner's OIDC JWT at OpenBao's JWT auth endpoint. reusable-ci's
generic release workflow does not perform that provider-specific exchange:

```yaml
# Organization-owned wrapper/fork, at each reusable-ci/cosign signing step
- name: Sign release artifacts
  env:
    VAULT_ADDR: https://bao.example.internal
    VAULT_TOKEN: ${{ steps.openbao-auth.outputs.token }} # short-lived
  run: reusable-ci release sign
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

### `sign.transparency`: what the signing run publishes about itself

Signing is not only a local act. By default, cosign writes an entry to the
**public** Sigstore transparency log (Rekor) for `kms` as well as `sigstore`.
This surprises people, because the KMS key is yours and nothing about the
signature obviously needs a public service. KMS signing can opt out explicitly
with `transparency: none`, described below.

What the entry contains: the artifact's SHA-256, the signature, the public key,
and a timestamp. **Not** the artifact's contents, and not its filename. So what
becomes public is the *existence and timing* of a signing event, plus a
fingerprint someone who already has the artifact can use to confirm it is the
one you signed. The entry is permanent and append-only: it cannot be withdrawn.

For anything you publish openly, this is the point rather than a cost: it makes
your release independently auditable and gives the signature a trusted
timestamp. That is why it is the default:

```yaml
sign:
  method: kms
  key: hashivault://transit/keys/release-signing
  # transparency: public   ← the default; no need to write it
```

For an artifact you do **not** publish (an internal-only build), the metadata
is a release-cadence signal you may not want to emit, and the log buys you
nothing since nobody outside can obtain the artifact to verify anyway:

```yaml
sign:
  method: kms
  key: hashivault://transit/keys/release-signing
  transparency: none
```

The signature still verifies against your public key indefinitely. That is
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
allowed, including on `method: kms`. A blocked Rekor does not degrade to an
unlogged signature; it fails the signing step hard, at release time. The config
plan precomputes `sign.requires_sigstore_egress` so a caller can derive the
allowlist instead of guessing.

With `transparency: none`, signing needs **no Sigstore endpoint at all**, not
even TUF. That is deliberate and slightly subtle: turning off the transparency
log alone would still leave cosign fetching its trust root from
`tuf-repo-cdn.sigstore.dev` on every signature, because it verifies each
signature it has just written. reusable-ci supplies that trust material locally
instead, so a `transparency: none` signing run makes no outbound connection.
The black-box suite enforces this through a recording proxy rather than trusting
the flag (`release_sign_no_egress_test.go`), because "makes no outbound
connection" is the property people actually assume and argv alone cannot prove.

One exception worth knowing: **verifying a keyless signature still fetches the
trust root**, regardless of this setting. It has to: validating a short-lived
Fulcio certificate is exactly what the trust root is for. `transparency` governs
signing; keyless verification is inherently online.

### Snapshot-release trust model

Snapshot releases (`release-snapshot-orchestrator.yml`) are intentionally **not**
signed and **not** attested. They exist for fast iteration on branch pushes, not
for distribution to external consumers. A snapshot run publishes a
content-addressed npm snapshot (dist-tag `snapshot`, version
`<base>-snapshot-<branch>-<sha>`) and, when opted in, SBOMs. It builds **no
container images**.

Container images are built **once on the release path** (signed with cosign,
SLSA-attested, SBOM-attested) and then promoted to the moving `:dev` → `:staging`
→ `:release` tags by the build-once/promote-many ladder. A `:dev`-tagged image is
therefore the *same signed digest* as a release build, but `:dev` is a moving
pointer that advances on every qualifying build, so pin by digest (`@sha256:…`)
when you need a stable reference.

The production release orchestrator is stable-only: it accepts signed
`release-request/vMAJOR.MINOR.PATCH` tags and rejects SemVer prerelease or build
suffixes before planning or repository mutation. Use the unsigned snapshot
workflow for branch testing. A signed prerelease channel with the full
production attestation stack is not currently supported.

### Container image attestation & verification

Container images carry three independent, registry-attached pieces of evidence,
all verifiable on **any** registry or forge (GitHub / Forgejo / GitLab) with the
`cosign` CLI, and no GitHub attestation API is required:

| Evidence | How it is produced | How to verify |
| --- | --- | --- |
| **Signature** (integrity + identity) | `reusable-ci container sign` (cosign, `--recursive` over per-arch children) | `cosign verify` / `reusable-ci validate container-signature` |
| **SLSA v1.0 provenance** (how/where it was built) | `reusable-ci container attest --type slsaprovenance1`, a **signed** in-toto attestation whose predicate is generated from the CI environment | `cosign verify-attestation --type slsaprovenance1` |
| **SBOM** (what is inside) | `container attest --type cyclonedx`, a **signed** in-toto attestation, one per per-arch image digest (syft) | `cosign verify-attestation --type cyclonedx` |

Why a **signed** cosign attestation rather than BuildKit's in-index provenance:
BuildKit writes provenance as an *unsigned* in-index in-toto statement. It has
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
above. No flags are needed on GitHub / GitLab, explicit flags still win, and a
`--key` (KMS) verification skips the derivation. External consumers running
`cosign verify` directly must supply the identity constraints themselves, as
shown.

**SLSA level, precisely.** The default portable cosign provenance is **signed
and verifiable, ~SLSA Build L2**: the attestation is produced and signed *inside
the build job*, so the signing identity is reachable by build steps. A Build L3
claim requires a separately isolated builder or attestor whose signing material
the build cannot reach. The default reusable workflows do not make that claim.

#### Operator-managed SLSA Build L3 topology

The optional `slsa-attestor.yml` component supports an operator-managed L3
topology, but the operator owns the provider and runner isolation evidence. It
is not a turnkey level upgrade.

L3 has two requirements: **isolation** (ephemeral build environment) and
**non-forgeability** (the provenance signer is unreachable by the build steps).
Both are properties of *your* platform, not of a CLI flag. reusable-ci can't
manufacture them, but it makes them easy to *wire* and *verify* with pure
Sigstore (`cosign` + a KMS key), no forge-specific machinery:

```text
build job   (no KMS creds, ephemeral)  ──push by digest──▶  registry
    │  digest only (immutable, self-verifying)
    ▼
attestor    (KMS sign creds ONLY here, ephemeral)  ──signed provenance──▶  registry
            .github/workflows/slsa-attestor.yml
```

- **Signer:** `reusable-ci container attest --method=kms --key <kms-uri>`. Works
  with any cosign KMS (`hashivault://` OpenBao/Vault, `awskms://`, `gcpkms://`,
  `azurekms://`, PKCS#11/HSM). `--builder-id` sets the provenance `builder.id` to
  your documented builder identity.
- **Component:** the [`slsa-attestor.yml`](../.github/workflows/slsa-attestor.yml)
  reusable workflow is the isolated signer. Call it as a **separate job** that
  takes only the image digest; it never trusts build-supplied metadata.
- **The two operator obligations** (inherent to L3): grant the KMS sign
  permission to the attestor identity **only** (never the build job), and run
  both jobs on **ephemeral** runners.

The verifier pins the **KMS public key** as the builder root of trust. A valid
attestation then proves it was signed by a key only the isolated attestor can
use:

```bash
cosign verify-attestation --key kms-builder.pub \
  --type slsaprovenance1 <registry>/<image>@sha256:...
```

This is **L3-in-substance, verifiable against your own builder key**, on any
forge. What it does *not* give is a pre-blessed `slsa-verifier` builder badge
(those are wired to GitHub/GitLab SaaS builders); third parties verify against
*your* documented KMS builder identity instead, which is exactly right for a
private deployment, where the trust root is your KMS.

### Choosing a method

- **Sigstore-keyless: the recommended default for new projects** on a keyless-capable forge (GitHub / GitLab). No secret management, no key on the runner; in-CI verification needs no flags (the identity is derived and anchored, see above). The `examples/signing/sigstore-keyless` project is the copy-from reference.
- **GPG**: required for **PGP-native ecosystems** (Maven Central, apt/rpm repos) where consumers expect a `.asc`. Also the right pick for existing pipelines and operators who want to own the trust anchor entirely. This is the `examples/maven-app` shape.
- **KMS (OpenBao recommended)**: regulated / sovereignty-conscious deployments where the trust anchor must stay inside your own infrastructure. With `transparency: none`, signing avoids public Sigstore endpoints, but the runner must still reach the private KMS and normal release endpoints. See `examples/signing/openbao-kms`.

A repo can switch backends by changing one line in `artifacts.yml`. No swap-policy implications on the cosign branches (the decrypted private key never enters our process).

**Default when `sign.method` is unset:** `gpg`, unchanged. This preserves the pre-cosign contract for existing repos. New projects should set `method: sigstore` explicitly rather than rely on the fallback. The recommendation lives here and in the example projects, not in a silent default flip. Such a flip would change behaviour for every repo that never configured signing.

### Signing the release commit & tag (git objects)

The `sign:` block above selects how *release artifacts* are signed. Signing the **release commit and tag** (git objects) is a separate, independent axis, configured under `git-signing:`:

```yaml
# .reusable-ci/artifacts.yml
git-signing:
  method: ssh    # gpg (default) | ssh
```

- **`gpg` (default)**: the release commit/tag are signed with the imported `RELEASE_GPG_PRIVATE_KEY`; the committer identity comes from the key's UID. Unchanged for existing repos; omit the block entirely to keep it.
- **`ssh`**: git-native SSH signing (`gpg.format=ssh`). No OpenPGP key is imported onto the runner. `gitsign` (Sigstore git signing) is intentionally **not** offered: SSH signing is forge-portable and verifiable via `allowed_signers` without the extra dependency.

**To enable `ssh`, provide three things:**

1. **The signing key**: set the `RELEASE_SSH_SIGNING_KEY` secret to an OpenSSH **private** key (the workflow writes it 0600, configures git, and removes it in an `if: always()` cleanup).
2. **The committer identity**: pass `committer-name` and `committer-email` to the release orchestrator. Unlike a GPG key, an SSH key carries no name/email, so this identity is supplied explicitly.
3. **The allowlist entry**: commit the matching SSH **public** key to `.reusable-ci/allowed_signers`, keyed on the **same email** you pass as `committer-email`.

> ⚠️ `committer-email` is **not cosmetic** in SSH mode: it is the verification *principal*. `git verify-tag` runs `ssh-keygen -Y verify -I <committer-email>` and matches it against `.reusable-ci/allowed_signers`. If the email is not listed there, `validate tag signature` (and any consumer running `git verify-tag`) fails. With GPG this coupling doesn't exist: verification keys off the key fingerprint, not the email.

Verification is identical to any SSH-signed tag. See [Local Git signature
setup](DEVELOPMENT.md#local-git-signature-setup) and [Git Tag
Verification](#5-git-tag-verification). `reusable-ci doctor` cross-checks the
setup: with `git-signing.method: ssh` it warns when
`.reusable-ci/allowed_signers` is absent and reminds you the configured
`committer-email` must be one of its principals.

## Release Authorisation

Release authorisation is the policy "who may trigger a stable production release of this project?" In reusable-ci it lives in **committed files** under `.reusable-ci/`. No CI secret. No platform-specific user database. No leakage when migrating between Git hosts.

The committed-file model lets the signature do the heavy lifting. For SSH, `git verify-tag` natively reads `gpg.ssh.allowedSignersFile`. For GPG, an in-process verifier checks the tag signature against the project's committed public keys. A release fails closed when the policy is on but the signer cannot be established.

### Request → promote: release tags are immutable

A release is requested by pushing a **signed `release-request/vX.Y.Z` tag**. reusable-ci verifies *that* tag's signature, which is what the allowlist checks. It then bumps the version and changelog into a commit, **creates the final `vX.Y.Z` tag once** at that commit, and pushes it without `--force`.
No tag is ever moved, deleted, or force-pushed: both `release-request/vX.Y.Z` and `vX.Y.Z` are immutable.
The signed request tag stays in the repo as the cryptographic anchor of who authorised the release. The bot's release commit records the original tagger in `Release-Request` / `Release-Authorized-By` / `Co-authored-by` trailers. So the verified authorisation and the shipped commit always correspond.
(The `reusable-ci version derive-release` and `version tag-release` commands implement this; workflows never parse refs or move tags in bash.)

### Org policy: allowlisting is on by default at the caller layer

The engine input `release.requireallowlistedsigner` defaults to **false** so the tool stays usable in any repo. Org policy turns it on in the shared release-workflow template, so every diggsweden release is signed by an allowlisted maintainer. The reference consumer (`wallet-backend-reference`) shows the live wiring:

```yaml
# .github/workflows/release-workflow.yml
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v3.0.0
    with:
      branch: main
      release.requireallowlistedsigner: true   # org default; override only for private repos
```

Setting it at the caller layer, rather than hardcoding it non-overridable in the engine, leaves a genuinely-private repo an **auditable** escape hatch. Dropping the line to `false` is a visible change in the repo's own workflow, reviewed like any other.

### When the gate runs

Two switches enable it:

- **Per-artifact**: `require-authorization: true` on any artifact in `artifacts.yml`. Use this when a *specific* deliverable (e.g. a public library) needs the gate; other artifacts in the same repo aren't gated.
- **Per-release**: `release.requireallowlistedsigner: true` on `release-orchestrator.yml` (the org default). Use this when *every* release of the repo must pass the gate.

If either is true, the gate runs. Behaviour when the gate is on:

| Scenario | Outcome |
|---|---|
| Signer's key/fingerprint is in the allowlist | release proceeds |
| Signer's key/fingerprint is NOT in the allowlist | `EX_NOPERM` (exit 77) |
| Allowlist files are all missing or empty | `EX_NOPERM` (exit 77), fails closed |
| Tag signed but signature can't be verified against any allowed key | `EX_NOPERM` (exit 77), fails closed |

When the gate is **off**, a present allowlist is still honoured. A missing allowlist, or an unverifiable signature, is **not** silent. The run emits a prominent `::warning::` in the Annotations pane and a `> [!WARNING]` callout in the prerequisites summary. Both read "this release ran with no signer allowlist; any valid signature was accepted." A release without an allowlist can't slip by unnoticed.

### The allowlist files

`.reusable-ci/allowed_signers` covers SSH-signed tags. It uses the OpenSSH `allowed_signers` format (see `man ssh-keygen`, *ALLOWED SIGNERS*). The same file `git verify-tag` reads when you set `gpg.ssh.allowedSignersFile`. Each line carries the public key inline, so it is both the authorisation list and the verification material:

```text
# .reusable-ci/allowed_signers
alice@example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI...alice's release key
bob@example.com   ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI...bob's release key valid-before=2027-01-01
```

The `valid-before` / `valid-after` options let a key's authority expire without removing audit history.

`.reusable-ci/allowed_gpg_keys.asc` covers GPG-signed tags. It is an armored
public-key bundle (one or more `PGP PUBLIC KEY BLOCK` sections concatenated). Like the SSH
file, it is **both the verification material and the authorised set** (single
source): the primary-key fingerprints of the keys in the bundle *are* the
allowlist, so the two can never drift apart. Export with
`gpg --armor --export <email> >> .reusable-ci/allowed_gpg_keys.asc`. Appending is
supported: every armor block in the file is read, so a rotation that leaves the
outgoing key in place while adding the incoming one authorises both. A block
that does not parse fails the run rather than being skipped: the file must
never be read in part.

```text
# .reusable-ci/allowed_gpg_keys.asc
-----BEGIN PGP PUBLIC KEY BLOCK-----
...alice's release key...
-----END PGP PUBLIC KEY BLOCK-----
-----BEGIN PGP PUBLIC KEY BLOCK-----
...bob's release key...
-----END PGP PUBLIC KEY BLOCK-----
```

Because the signer's public key travels with the repo, the in-process verifier can actually check the signature. So `release.requireallowlistedsigner: true` genuinely enforces. A missing or empty allowlist fails closed, and so does a signature that can't be verified against any committed key. Neither is waved through. (There is no separate fingerprints-only file. A fingerprint with no key material can't be verified, which was the gap this design removes.)

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

- **Snapshots do not enter this gate.** The production orchestrator accepts only signed `release-request/vX.Y.Z` refs and creates only stable `vX.Y.Z` tags. Branch snapshots use the separate unsigned snapshot orchestrator.
- **Multi-module Maven**: the gate is per-release-tag, not per-module. One tag, one signer, one allowlist check.
- **Key rotation**: add the new entry, leave the old entry until everyone's switched, then remove it in a PR. `valid-before` lets you pre-stage a removal date.
- **The allowlist is missing but the gate is off**: tolerated, but **loudly**. A `::warning::` annotation and a summary callout flag that the release ran with no allowlist. The project hasn't opted in; turn on `require-authorization: true` or `release.requireallowlistedsigner: true` (the org default) to enforce.
- **Fingerprints-only list, signer's key not available**: with the gate **on**, a signature that can't be verified against any allowed key fails closed (exit 77). Commit the signer's public key to `allowed_gpg_keys.asc`. With the gate **off**, it's a loud warning, not a failure.
- **The GPG allowlist is present but is not a key bundle**: refused as malformed input (exit 65) before any signature is checked, with the gate on or off. A truncated or hand-edited bundle is a broken policy file, not an unverifiable signature.

## Secret-handling model

The CI binary handles four classes of sensitive material during a release. They are the **GPG private key** (release-signing), the **GPG passphrase**, the **release token** (platform API access), and **registry passwords**. The defences are layered to keep each one inside the smallest possible blast radius.

### Argv hygiene

No secret value reaches `argv`. The CLI accepts secrets either via:

- a `--<name>-file` flag whose value is a path (`-` reads from stdin), or
- a documented environment variable (`GPG_PRIVATE_KEY`, `GPG_PASSPHRASE`, `RELEASE_TOKEN`, …).

Process-listing tools (`ps`, `/proc/<pid>/cmdline`, container introspection) never see the secret value. The discipline is documented in `internal/cli/secret/secret.go` and enforced consistently across every subcommand that touches a secret.

### In-process OpenPGP signer

`release sign` and `release sbom-zip --sign` use an in-process OpenPGP signer (`internal/adapters/openpgp/signer.go`, backed by `github.com/ProtonMail/go-crypto`). The private key:

- Lives only in heap memory for the lifetime of the signing process.
- Is never written to disk by reusable-ci.
- Is never spilled into `gpg-agent`'s cache (which would persist for the runner's lifetime, a vector for follow-on jobs on the same self-hosted runner).

`release gpg import` (used by `git tag -s` / `git commit -S` paths that shell out to the gpg CLI) sends the armored key to `gpg --import` **via stdin**, not a tmpfile. The key never lands on disk between our process and gpg's own keyring.

### Subprocess output redaction

`internal/safeexec/RedactKeyMaterial` scans every subprocess's
combined-output for PEM private-key markers (`BEGIN PGP PRIVATE KEY`,
`BEGIN OPENSSH PRIVATE KEY`, `BEGIN RSA PRIVATE KEY`,
`BEGIN EC PRIVATE KEY`, `BEGIN ENCRYPTED PRIVATE KEY`,
`BEGIN PRIVATE KEY`). If any marker is found, the entire output body
is replaced with a redaction notice before it propagates into an
error message. Defends against a future gpg / ssh-keygen / openssl
version that echoes input key material on stderr. Current versions
don't, but the safety net is small and directly tested.

### Process-level hardening (Linux)

At process startup, `safeexec.HardenProcess()` (called from `main()`) sets:

- `RLIMIT_CORE = 0`: the kernel won't write a core dump if the process crashes. A segfault holding a decrypted key in heap never produces a disk file containing it.
- `PR_SET_DUMPABLE = 0`: additionally suppresses core dumps regardless of RLIMIT, and stops a same-user process without `CAP_SYS_PTRACE` from attaching gdb/strace or reading decrypted key bytes from `/proc/<pid>/mem`.

These set process attributes; they are not a proof of resistance. A process with `CAP_SYS_RESOURCE` can raise the core limit again, and root or `CAP_SYS_PTRACE` can still read memory. The core limit is inherited by child processes, but `execve` resets the dumpable flag, so gpg, cosign and git subprocesses are dumpable again unless they set it themselves. No memory is locked with `mlock`.

Both are Linux-only; the helper is a no-op on other platforms (reusable-ci's CI runs on Linux runners, so portability isn't a goal here). Both are best-effort: kernels with seccomp filters that block the syscalls degrade silently, because logging the failure would leak runner configuration.

### Cleanup is always-on

Every workflow that imports a GPG key runs `reusable-ci release gpg cleanup` under `if: always()`:

- Deletes the secret half of the key from the gpg keyring.
- Deletes the public half.
- Kills `gpg-agent` (so any cached passphrase is gone).

Idempotent under failure; the cleanup runs even when the previous step exited non-zero.

### Inter-workflow data passing

The release pipeline crosses several `workflow_call` boundaries (orchestrator → prepare → build → publish → create-release). Data flowing across those boundaries is **either non-secret structured JSON** (config-plan / release-plan / publish-stage-plan) **or registered GitHub secrets** (`secrets.X`). Specifics:

- **JSON payloads** between stages contain artifact names, project types, working directories, publish-target enums, SBOM layer selections. Derived from the committed `.reusable-ci/artifacts.yml`; no field shape carries a secret value. The schema is in `internal/domain/pipeline/configplan.go`.
- **`$GITHUB_OUTPUT`** writes are limited to public identifiers: GPG fingerprints, key IDs, key UserID name/email, tag names, SHAs, basenames, container digests. No private material.
- **`$GITHUB_ENV`** is written once across the codebase: `build gradle-android decode-keystore` emits `ANDROID_KEYSTORE_PATH=<path>`, a filesystem path, not key bytes.
- **Secret-presence reporting** uses `${{ secrets.X != '' }}`, so the workflow env receives a boolean ("is X configured?"), never the value.
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
| Build artifacts (jar / tarball / APK / AAB) | 7 days | Compiled outputs; requested Android release outputs may be unsigned when keystore signing is disabled |
| Build-layer SBOMs (`bom.json`) | 7 days | Dependency list; no credentials |
| Analyzed-artifact SBOMs (SPDX / CycloneDX) | 7 days | Syft scan output; no credentials |
| Container digest markers | 1 day | Short ASCII digest IDs |
| Release artifacts bundle (`release-files/`, release notes) | 30 days | Files designed to attach to the GitHub Release |
| SARIF security reports | 5 days | Vulnerability findings; may contain code snippets, not credentials |

Android's build workflow uploads each requested APK/AAB as a run artifact, but
the production GitHub Release transfer selects only a release AAB when
`include-aab` is true. APKs are not automatically release assets. Selection is
independent of `enable-android-signing`, so an unsigned AAB can reach release
assembly (and a configured Google Play upload); reusable-ci does not validate
that coupling.

**Two rules that must hold for the upload-artifact contract to stay safe:**

1. **Globs must be narrow.** A `path: *.jar` or
   `${{ github.workspace }}/dist/**` is fine; `path: .`, `path: **`,
   `${{ github.workspace }}/**`, or `$GITHUB_WORKSPACE/**` is not. Those
   patterns pick up anything that happens to be in the workspace, including a
   misplaced keystore, certificate, or env-leaked secret file. The guard models
   only `*`, `**`, and `?`; character classes, brace expansion, and extglobs
   fail closed rather than being guessed safe. The contract is locked by the
   default-tier repository guard in
   `internal/workflowguard/artifactupload_guard_test.go`, which rejects
   workspace-root and workspace-wide recursive forms while allowing scoped
   subdirectories.
2. **Secret-bearing files live outside the project working directory.** `release.keystore` is decoded into `$RUNNER_TEMP` (or `os.MkdirTemp`), never cwd. The runner-temp path is cleaned between jobs on hosted runners. This is the v4 hardening: it removes the keystore from any path glob a future contributor might write.

### Containerfile secrets: caller responsibility

Containerfiles compiled by `publish-container.yml` get a forge-neutral **registry** layer cache (a dedicated `<registry>/<owner>/buildcache` package; opt out with `build-cache: false`). The cached layers live in the registry and may be readable to anyone who can read that package. **A Containerfile that writes a secret to a layer ships that secret to the cache** (and to the image). Always use:

```dockerfile
RUN --mount=type=secret,id=npmrc,target=/root/.npmrc \
    npm publish --access=public
```

…rather than baking the secret into a `RUN echo $SECRET >` step. Buildah's
`--mount=type=secret` form gives the secret only to the running step and never
persists it as a layer.

### Threat model boundary

What this design defends against:

- **Argv inspection** by other processes on the runner. ✓
- **Disk forensics** on `/tmp` after the run. ✓ (Both the in-process signer and the stdin-import paths never write the key to disk.)
- **Recovered CLI panics.** The CLI panic boundary exits 70 and reports only a
  safe payload category, argument count, trusted version/commit metadata, and
  function/file/line locations from `runtime.CallersFrames`, followed by an
  issue-report URL. It omits payload contents, all argv, and raw stack argument
  words; it does not invoke payload `Error`, `String`, or `Format` methods or
  read source files. This is an omission policy, not a secret detector. It does
  not cover runtime-fatal crashes or unrecovered panics in other goroutines.
- **Core dumps** capturing heap memory on segfault. ✓ (`RLIMIT_CORE=0` + `PR_SET_DUMPABLE=0`.)
- **ptrace attach** by an unprivileged same-user process on the runner, against the reusable-ci process itself. ✓ (`PR_SET_DUMPABLE=0`; child processes are not covered.)
- **gpg-agent persistence** across follow-on jobs. ✓ (Cleanup kills the agent.)
- **Future-tool stderr echoing** of input key bytes. ✓ (`RedactKeyMaterial` scrubs marker'd output.)

What this design does NOT defend against:

- **Swap-page extraction.** Addressed by refusing to run signing on swap-enabled hosts (see the [Swap policy](#swap-policy) below). The defence is policy-enforced, not cryptographic.
- **A root attacker on the runner.** A process running as root can read `/proc/<pid>/mem` regardless of `PR_SET_DUMPABLE` (the flag affects non-root ptrace; root bypasses it). The threat model assumes the runner OS itself is trusted.
- **Side-channel attacks** on the host CPU (Spectre, Rowhammer, etc.). Out of scope; mitigated by the runner platform.

### Swap policy

`release sign` and `release sbom-zip --sign` refuse to run when the kernel has an active swap area. The check reads `/proc/swaps`; if any line beyond the header is present, the binary exits `EX_CONFIG` (78) with an actionable error message.

**No CLI flag. No lookalike env-var bypass.** The fix is at the runner. The error message lists:

1. The one-line runner fix: `sudo swapoff -a` (persist in `/etc/fstab`).
2. Switching to a hosted runner (GitHub-hosted, GitLab-shared, Forgejo-Actions runners have swap disabled by default).
3. Alternatives for operators who genuinely cannot disable swap: **Sigstore keyless** (`cosign sign` + OIDC; no long-lived private key) or reusable-ci's **KMS-backed signing** (`sign.method: kms`; AWS KMS, GCP KMS, Azure Key Vault, OpenBao/Vault, or PKCS#11). The key remains in the provider; provider authentication and reachability are operator responsibilities.

**Debug-only override.** Local debugging sometimes needs to exercise the signing path on a developer laptop where swap is on for unrelated reasons. The narrow escape hatch is the `--debug-allow-swap` CLI flag:

```bash
reusable-ci release sign --debug-allow-swap ...
```

When set, the policy passes AND a loud `::warning::` annotation lands in the CI log naming the override and the risk. The flag is shown in `--help` (not hidden) on purpose: discoverability is the audit signal. A `--debug-allow-swap` in workflow YAML is reviewable in a PR; the flag must reappear in argv each invocation.

**The override is for local debugging only. Production releases must never set it.** A `::warning::` annotation on a production release is a release-quality bug. The operator should be running on a swap-off runner instead.

**Soft-pass on indeterminate state.** If `/proc/swaps` cannot be read (restricted containers, hardened sandboxes), the policy soft-passes. Refusing on indeterminacy would block legitimate restricted-container deployments where the visibility loss is structural; positive evidence is required to claim swap is active.

The policy is enforced in `internal/safeexec/swap_linux.go::RequireNoSwap`. Unit tests + black-box integration tests pin the contract:

- Active swap area → exit 78 with the documented error text.
- No swap area → normal signing flow.
- `/proc/swaps` unreadable → soft-pass.
- `--debug-allow-swap` bypasses with a loud `::warning::` annotation. The flag is the only opt-out surface; `safeexec.RequireNoSwap(bool)` takes the operator decision as a parameter.
- `REUSABLE_CI_PROC_SWAPS` names a file read instead of `/proc/swaps`. It is how the black-box suite injects swap state into the real binary. An override that reports swap still refuses; a pass read from it is announced with a `::notice::` saying the answer was injected rather than read from the kernel.

The scope is narrow on purpose: only the two CLI paths that bring decrypted private-key material into the Go heap. `release gpg import`, `validate tag signature`, `version commit-push`, and so on are unaffected: they use public-key material or delegate to gpg-agent (libgcrypt's own secure-memory pool).

## Deterministic Pipeline

A deterministic pipeline produces consistent, repeatable verdicts: the same
inputs always yield the same outputs and the same pass/fail. reusable-ci is
built around this stance. The table below maps the standard principles
to where they live in this repo.

| Principle | How reusable-ci implements it |
|---|---|
| Version control everything | Reusable workflows live in this repo, not pasted into adopters'. Plans, configs, allowlists, and runtime-image Containerfiles are all checked in. |
| Lock dependency versions | Per-ecosystem lockfile presence is validated before every release (`validate cargo`, `validate prerequisites`'s lockfile branches). Cargo's `--locked` is enforced at fetch + compile. GitHub Actions are SHA-pinned with Renovate version comments. |
| Eliminate environmental variance | All build / test / publish jobs run inside reusable-ci-runtime container images. `runs-on:` is pinned to a specific Ubuntu major (`ubuntu-24.04`), not `ubuntu-latest`, so the runner doesn't roll forward silently. `SOURCE_DATE_EPOCH` is baked from `git log -1 --format=%ct HEAD` for Go + Cargo binaries AND for security-report timestamps (`reportTimestamp` in `internal/app/security/trivy.go`). |
| Remove human intervention | Tag push triggers `release-orchestrator.yml` end-to-end; there is no `workflow_dispatch` in the critical path. Release authorization is policy code (`allowed_signers` / `allowed_gpg_keys.asc` checked against the tag signature), not a human approval. |
| Fix flaky tests immediately | reusable-ci's own test suite is CI-pinned and required-green per release; the Go test conventions that keep it deterministic (parallel-safe fixtures, injected clocks, tool-presence skips instead of fails) are documented in [docs/testing.md](testing.md). On the adopter side the corresponding obligation is the same: a release whose tests sometimes pass and sometimes fail is not a deterministic pipeline. |

### What Renovate already pins

Three layers of dependency pinning ship in the inherited Renovate setup.
This is operative, not aspirational:

- **Third-party GitHub Actions** (`uses: actions/checkout@…`,
  `step-security/harden-runner@…`, etc.) carry full 40-character commit SHAs
  plus `# vX.Y.Z` comments. Enforced by `pinDigests: true` on the
  `github-actions` manager in `local>diggsweden/.github:renovate-base`.
- **Containerfile FROM lines** (the runtime image build) are
  digest-pinned. `TestRuntimeContainerfileExternalFromImagesAreDigestPinned`
  rejects external bases without `@sha256:…`; Renovate's `dockerfile` manager
  updates the tag and digest under `pinDigests: true`. The Debian pin is held in
  the global `DEBIAN_VERSION` ARG consumed by `FROM`.
- **Pinned tool versions** in Containerfiles, workflow env defaults, and
  mise tool refs are updated via `# renovate: datasource=…` markers and
  customManagers. A Rust toolchain or `cyclonedx-gomod` bump goes
  through a reviewable PR, not a silent rebuild.

`container: image:` references in workflow YAML come from inputs
(caller-controlled), never hardcoded. There used to be one exception
(`lint-swift.yml`) which now reads from the standard `runtime-image`
input. The default values for those inputs are tied to this repo's own
release version (bumped at release time, not by Renovate).

### Trust boundaries

What's left after Renovate is one named choice, not a silent gap:

- **Self-published GHCR images** (`ghcr.io/diggsweden/reusable-ci-runtime-*`)
  are referenced by tag (`:v3.0.0`), not by digest. These tags come from
  workflow input defaults that this repo bumps at release time, not from
  Renovate. The trust model is registry-side immutability of our own
  org's tagged releases: we do not rewrite tags. Adopters who need
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
| **Build SBOM** generation (cyclonedx) | Required by each artifact's default `sboms: all` | Exclude `build` in that artifact's `sboms`, or pass `enable-build-sbom: false` to a directly called builder |
| **Container vulnerability scan** (trivy, CRITICAL+HIGH) | Required | `enable-scan: false` to skip entirely, or `scan-severity: CRITICAL` to relax the threshold |
| **Artifact-presence verification** in publish-container | Required | Drop the `from:` entry in artifacts.yml so the verify step is skipped |
| **JVM reproducibility settings** (Maven `outputTimestamp`, Gradle archive task settings, checked statically) | Required | Drop the artifact from the matrix; there is no per-artifact opt-out for the settings check |
| **Cargo lockfile + toolchain pin** | Required | None. `Cargo.lock` and either `rust-toolchain.toml` or `rust-toolchain` are mandatory for every Cargo artifact |

A passing pipeline implies the checks selected by the committed artifact and
release policies completed; none silently degrades after selection. Adopters
who need a non-default policy set it explicitly.

## Reproducible Builds

A reproducible build produces a byte-identical artifact every time the same source is built with the same toolchain. The artifact's SHA256 then becomes a meaningful fingerprint. Any verifier can rebuild the tag from scratch and confirm the published artifact matches what the source declares. Without reproducibility, SBOMs and signatures attest to *a* build, not *the* build the source implies.

### Per-ecosystem status

| Ecosystem | Artifact | Knob | Wired by reusable-ci? |
|---|---|---|---|
| Go (artifact-first) | binary | `-trimpath -buildvcs=false` + `SOURCE_DATE_EPOCH` ldflag | Yes, automatic in `build-go.yml` |
| Go (container-first) | image-embedded binary | Caller-selected `go build` flags inside the Containerfile | Caller-owned; reusable-ci timestamps the image, not the compile sandbox |
| Cargo (artifact-first) | release binary | `cargo build --release --locked --target <triple>` + `Cargo.lock` + `SOURCE_DATE_EPOCH` honoured via metadata derivation | Yes, automatic in `build-cargo.yml` |
| Cargo (container-first) | image-embedded binary | Stock cargo + `Cargo.lock` checked in | Caller-owned (validated via `validate cargo` across both build-modes) |
| Maven | main jar, sources jar | `<project.build.outputTimestamp>` in `pom.xml` | Caller-owned; its presence is required by `validate jvm-reproducibility` (release fails if missing) |
| Gradle (JVM + Android) | jar, war, distZip, distTar, APK | `preserveFileTimestamps = false` + `reproducibleFileOrder = true` on `AbstractArchiveTask` | Caller-owned; its text is required by `validate jvm-reproducibility` (release fails if missing) |
| NPM | `.tgz` | npm ≥ 10 (fixed in npm/cli#3536) | Runtime image pins node 24 LTS (npm 10+) |
| Container image (both flows) | OCI image-config + layer blobs | `SOURCE_DATE_EPOCH` env on the build step (buildah `--timestamp`) | Yes, automatic (computed once in `prep` from `git log -1 --format=%ct HEAD`) |

Black-box reproducibility coverage is distributed across the companion suite.
`tests/blackbox/reproducibility_test.go` compares rebuild hashes for Maven
main/sources JARs, Gradle JAR/distZip/distTar, Cargo binaries/crates, npm
tarballs, and a generic buildah image-config digest.
`tests/blackbox/build_go_test.go` owns the Go binary and multi-platform rebuild
checks. This is available coverage, not every cell in the implementation table:
Android APK/AAB, the Cargo container-first embedded binary, and every
ecosystem/container-flow combination are not rebuilt there.

The companion suite has two explicit modes. Its default local mode keeps
`requireTool` skips so a developer can run the available subset without first
installing every ecosystem toolchain. Authoritative companion CI and the parent
PR/release gates set `REUSABLE_CI_BLACKBOX_PROFILE=strict`: required tools are
preflighted, post-preflight Buildah failures are fatal, and a minimum scenario
floor rejects partial runs. Go test-selection flags such as `-run` and `-skip`
are rejected before execution rather than left for the floor to infer. A green
strict run therefore cannot
silently mean that Maven, Gradle, Cargo, npm, Buildah, or signing coverage was
omitted.

The parent gates pin
`b9f7ab2a22c2fae30ec8df5194c64ff094ff0843`, which contains the strict-profile
implementation. Both gates validate the checked-out source contract before tool
setup and then run the suite with the strict profile enabled. Future updates
must deliberately bump both gates to one reviewed immutable commit SHA; a
mutable branch or tag is not an acceptable fallback.

### What `validate jvm-reproducibility` does

The release-orchestrator runs `validate prerequisites` before every release, and `prerequisites` now includes a `jvm-reproducibility` check when the plan contains a Maven, Gradle, or Gradle-Android artifact.

For each artifact it:

- Reads `pom.xml` (Maven) or `build.gradle{,.kts}` (Gradle) under the artifact's `working-directory`.
- For Maven: real XML parse looking for `<project.build.outputTimestamp>` under `<properties>`.
- For Gradle: line-by-line substring check for `preserveFileTimestamps = false` AND `reproducibleFileOrder = true`. Tolerates Kotlin and Groovy DSL forms, with or without whitespace around `=`.
- Emits a GitHub Actions `::error::` annotation when the setting is missing, and prints an actionable fix snippet: the exact `<project.build.outputTimestamp>` or `tasks.withType(AbstractArchiveTask)` block to paste. It then exits non-zero, so the release stops at `validate prerequisites`.

The check is a hard gate on the settings, and a static one: it reads the
committed build files and never runs Maven or Gradle. A release cannot reach
publish with the settings missing, but a pass is not proof that the archive is
byte-identical. The Gradle check reads line text and skips `//` line comments,
with no block-comment, string or evaluation-order awareness, so a setting that
appears only in a block comment, or is assigned the opposite value later in the
script, still passes; the Maven check reads the POM's own `<properties>` and not
the effective POM. Byte-identical output is shown only by rebuilding, as the
black-box reproducibility tests above do. To run standalone (e.g. from the CLI):

```bash
export CONFIG_PLAN_JSON="$(reusable-ci config parse-artifacts ...)"
reusable-ci validate jvm-reproducibility
```

#### Scope: each registered artifact's manifest is checked standalone

The validator iterates `artifacts.all[]` from the config plan and reads
`pom.xml` (or `build.gradle{,.kts}`) under each artifact's
`working-directory`. It does not compute Maven's *effective POM*, which means it
does not resolve `<parent>` chains across the workspace.

Practical implications:

- **Single-project Maven (typical case)**: one artifact entry, one pom.xml,
  one check. The validator's reading matches Maven's.
- **Multi-module Maven registered as one artifact** (pointing at the parent
  POM): the parent POM is the only one inspected, which is correct, because the
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

`publish-container.yml` computes `SOURCE_DATE_EPOCH` once from `git log -1 --format=%ct HEAD` in the `prep` job. It passes that as the env on both the main `reusable-ci container build` step and the optional `extract-binary` step. buildah reads it (via `--timestamp`) and:

- Writes the value into the OCI image-config `created` field.
- Pins every layer entry's `mtime` to it (`--timestamp` sets all timestamps to
  the value, a fully deterministic superset of an `mtime` clamp).

Result: two rebuilds of the same tag produce the same **image-config digest** and the same **per-layer blob digests**. This is the identity registries and verifiers care about (`crane manifest`, `cosign verify`).

Two things are NOT byte-stable, by design, so verifiers must compare contents rather than the wrapping bytes:

- **SLSA provenance attestation** (`enable-slsa: true`, signed via `reusable-ci container attest`) embeds the run-id, invocation, and build timestamps. The attestation differs every run, which is correct: it identifies a specific build event, not the artifact.
- **Analyzed-container SBOM attestation** (`enable-analyzed-container-sbom: true`) uses syft, which generates fresh UUIDs and embeds scan time per run. The SBOM content (components, versions) is stable, but the document bytes are not.

### Non-issues that look like reproducibility bugs

- **Registry layer cache** (the dedicated `buildcache` package, tag-scoped per `(image, arch)`) keeps matrix legs from evicting each other. buildah pins the image-config `created` field and layer mtimes to `SOURCE_DATE_EPOCH` via `--timestamp`. The image-config digest is therefore stable whether the build hits or misses the cache.
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
buildah bud --timestamp "$SOURCE_DATE_EPOCH" --layers -t test .

# 3. Compare SHAs.
sha256sum target/*.jar              # Maven JAR
sha256sum build/libs/*.jar          # Gradle JAR
buildah images --noheading --format '{{.ID}}' test  # container image ID
```

If the SHAs differ on a tag that was published with reusable-ci ≥ v3 and the manifest opts into the repro knobs, that's a regression. Please file an issue with the diff so the testsuite can pick it up.

## Application Configuration vs Environment Configuration

reusable-ci follows the [12-Factor App config split](https://12factor.net/config): application configuration ships with the immutable artifact, and environment configuration is supplied at deploy time and never bakes into the build. The split is enforced structurally:

| | Application configuration | Environment configuration |
|---|---|---|
| **What** | What to build, how to test, what to sign | Where to publish, who to push as, what credentials |
| **Where it lives** | `artifacts.yml`, workflow YAML defaults, runtime-image versions | Org secrets, workflow inputs, GitHub OIDC token |
| **Varies by env?** | Never (same value across staging and production) | Per deployment target |
| **Travels in the artifact?** | Yes, baked into the binary's `main.version`/`main.commit`/`main.date` ldflags, the OCI image layers, the SBOM | No, passed through `secrets:` blocks at publish time and dropped after the step |
| **How reusable-ci enforces it** | `artifacts.yml` is parsed literally (no env-var expansion); `validateWorkingDirectory` rejects absolute paths / `${…}` refs / `..` escapes; the typed `PlannedArtifact` plan is pure-data with no secret material | every internal `workflow_call` site names the secrets it forwards (no `secrets: inherit` between reusable workflows); no secret ever passes through `with:` (would log to the run UI); `release sign` reads keys from env not argv |

**Concrete violations the validator now catches at parse-time** (in `validateWorkingDirectory`):

- `working-directory: /srv/build/svc`: absolute path, couples the build to a runner layout
- `working-directory: ${WORKSPACE}/svc`: shell-style ref that artifacts.yml does not expand
- `working-directory: ../outside-the-repo`: escapes the workspace, no longer reproducible from a fresh checkout

**Things that look like env config but aren't:**

- `version` / `commit` / `build-date` baked into binaries via ldflags: these *identify* the artifact, derived from the git tag + SHA + `SOURCE_DATE_EPOCH`. Same bytes on rebuild = same fingerprint, which is the whole point.
- `runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-*:v3.0.0` defaults: pinned to this repo's release version. The default *is* the application config; adopters override per-environment via workflow inputs.
- `github.run_id` in cross-job artifact-upload names: workflow-internal scoping, never reaches the released artifact.

**What adopters get for free:**

- An artifact that passes staging is byte-identical to what runs in production (reproducible builds + SBOM attestation)
- Rolling back = redeploy the previous tag's signed artifact; all bundled app config rolls back with it
- Environment-specific behavior changes (different DB URL, different feature-flag values per env) happen at the consumer's deploy step, not in the build pipeline. The artifact stays one

## SBOM Verification

[SBOM generation](sbom.md) is the canonical reference for layers, defaults,
naming, formats, delivery, analysis, and failure semantics. The verification
recipes below retain only the trust-order steps for release assets and signed
container attestations.

## Full Verification Guide

### 1. Container Image Verification

Verifies the image's identity and that it came out of this repository's CI.

#### Verify SLSA Provenance

SLSA v1.0 build provenance is attached to the OCI image as a **signed cosign
attestation** by `reusable-ci container attest` (the predicate is generated from
the CI environment). It is registry-attached and verifiable on **any** registry
or forge, which is the primary, portable path:

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

This is the single, portable provenance path. reusable-ci deliberately does not
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

Binary releases carry a **signed SLSA v1.0 provenance**, generated by
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

### 5. Git Tag Verification

Verifies tag signatures and authenticity.

```bash
git fetch --tags
git verify-tag v1.0.0
git verify-tag v1.0.0 --raw
```

### 6. Git Commit Verification

Verifies developer identity via GPG or SSH signatures.

```bash
git verify-commit <commit-hash>
git show --show-signature <commit-hash>
git log --pretty='format:%h %G? %aN %s' --abbrev-commit
```

`%G?` reports the signature state (`G` good, `B` bad, `U` untrusted, `X`/`Y`
expired, `R` revoked, `E` missing key, `N` unsigned). Repository policy should
decide which states fail rather than relying on formatted output alone.

### 7. OCI Inspection

Verify the signature and attestations first, then inspect or pull the exact
digest you authenticated:

```bash
skopeo inspect --raw docker://ghcr.io/diggsweden/${PROJECT}@sha256:DIGEST
podman pull ghcr.io/diggsweden/${PROJECT}@sha256:DIGEST
```

## Local setup and references

Tool installation, public-key retrieval, and local Git SSH/GPG configuration are
developer-machine concerns and live in
[Development](DEVELOPMENT.md#local-verification-tools). Repository release
policy continues to use the reviewed allowlists under `.reusable-ci/`.
