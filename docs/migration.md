<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Migration To v3.0.0: Permissions And Secret Names

All consumers upgrading from v2 to v3.0.0 must use the canonical secret names and explicit secret mappings. Update the workflow revision and release permissions too. Keep your existing build/publish inputs and add any ecosystem-specific secrets your setup needs.

## 1. Upgrade Both References And Allow PR Reads

The example below pins a revision containing the canonical secret names, OpenGrep suppression-counting fix, and changelog permission fix. Use it once published, or a release containing it. Update **both** `uses: ...@ref` and `with.reusable-ci-ref` to the same revision. Updating only the helper-script ref does not change workflow permissions.

Add `pull-requests: read` to the consumer's release job, retaining its other permissions. This is required for **public and private repositories** when adopting the fixed workflows. Changelog generation uses the automatic `GITHUB_TOKEN`; no additional permission on the release PAT is needed for this fix.

## 2. Use The Canonical Secret Names

| Previous name | Canonical name |
|---------------|----------------|
| `RELEASE_BOT_TOKEN` | `RELEASE_TOKEN` |
| `OSPO_BOT_GPG_PRIV` | `RELEASE_GPG_PRIVATE_KEY` |
| `OSPO_BOT_GPG_PASS` | `RELEASE_GPG_PASSPHRASE` |
| `OSPO_BOT_GPG_PUB` | `RELEASE_GPG_PUBLIC_KEY` |
| `MAVENCENTRAL_USERNAME` | `MAVEN_CENTRAL_USERNAME` |
| `MAVENCENTRAL_PASSWORD` | `MAVEN_CENTRAL_PASSWORD` |
| `SARIF_UPLOAD_TOKEN` | `CODE_SCANNING_TOKEN` |

The workflows and scripts provide **no aliases or fallbacks** for the previous names. Configure the canonical repository/organization secrets and grant each consuming repository access. Keep the GPG private key, public key, and passphrase consistent. Custom Maven settings must use `${env.MAVEN_CENTRAL_USERNAME}` and `${env.MAVEN_CENTRAL_PASSWORD}`.

## 3. Replace Inheritance With Explicit Secret Mappings

For a reusable-workflow call, use **`secrets:`**, not `env:`:

```yaml
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@747ac6d4ff82d409a48c125266939a15df9a3270
    permissions:
      contents: write
      packages: write
      id-token: write
      actions: read
      attestations: write
      pull-requests: read
    secrets:
      RELEASE_TOKEN: ${{ secrets.RELEASE_TOKEN }}
      RELEASE_GPG_PRIVATE_KEY: ${{ secrets.RELEASE_GPG_PRIVATE_KEY }}
      RELEASE_GPG_PASSPHRASE: ${{ secrets.RELEASE_GPG_PASSPHRASE }}
      RELEASE_GPG_PUBLIC_KEY: ${{ secrets.RELEASE_GPG_PUBLIC_KEY }}
      CODE_SCANNING_TOKEN: ${{ secrets.CODE_SCANNING_TOKEN }} # Optional SARIF upload
    with:
      reusable-ci-ref: 747ac6d4ff82d409a48c125266939a15df9a3270
      artifacts-config: .github/artifacts.yml
      # Retain your existing release inputs here.
```

Map only what the called workflow needs. Add Maven Central, Android, or Apple credentials when those features are enabled; see the [ecosystem examples](../examples/README.md). PR callers normally need only the optional `CODE_SCANNING_TOKEN`. Omit the secrets block if no custom secrets are needed. `GITHUB_TOKEN` is automatic.

`env:` is used only when running a script directly in a normal job or step, for example after checking out the helper scripts:

```yaml
- name: Validate signing public key
  env:
    RELEASE_GPG_PUBLIC_KEY: ${{ secrets.RELEASE_GPG_PUBLIC_KEY }}
  run: bash .github-shared/scripts/validate/gpg-public-key.sh
```

**Required for v3.0.0:** replace `secrets: inherit` before adopting this release. There is no grace period for inheritance or old secret names in v3. A `nosemgrep` suppression is not a substitute for migrating the caller. See [Passing Secrets](reference.md#passing-secrets).

## 4. Create Release Tags On The Updated Branch Tip

Merge the PR, update your local release branch (normally `main`), and create the signed version tag on that branch's HEAD. A tag on the PR commit before a merge, or on any older commit, is rejected before version changes are pushed. The workflow sets the project version from the tag name.

The tag-handling fix preserves full checkout history and uses the triggering tag explicitly. Workflow and helper refs must both include this fix: the helper now requires the recorded base commit and tag object from the updated workflow. Direct script callers must use the [documented arguments](scripts.md#move-tagsh).

If an earlier run already pushed a release commit but failed before moving its tag, inspect and update your local release branch before starting another release with a new version tag. This fix does not silently recover such partially completed releases. A no-change tag step can succeed without recreating the tag, but that does not make every publishing step in the full release pipeline rerunnable.

Before upgrading production, run the PR and release paths using the new mappings, including changelog generation in a private repo when applicable. Consumers staying on older pinned workflows are unaffected by the new permission requirement. Private repositories should also review the [remaining release conditions](private-repositories.md), particularly SLSA container provenance and private package dependencies.
