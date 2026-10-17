<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Private Repository Release Notes

The changelog permission fix resolves the missing PR-read permission; it does not guarantee every optional release feature is available in a private repository. The findings below come from reviewing the workflow chain and the pinned upstream actions, not from a live release against a consumer's private repository.

## Remaining Conditions And Limitations

| Stage | Condition | Effect / action |
|-------|-----------|-----------------|
| Version commit and tag move | Repository rules restrict direct pushes or tag updates | `RELEASE_TOKEN` needs repository access and Contents read/write. Its account must be allowed to perform those operations under the rules. `move-tag.sh` updates the signed tag with a lease on its original object; Contents write alone does not bypass rules. |
| Maven/NPM build | Dependencies live in a private package registry | Current build jobs grant only Contents read and do not wire package-auth tokens into dependency installation. This needs builder-side authentication support as well as access to the package. Adding permissions only to the consumer is insufficient. Public dependencies do not have this requirement. |
| GitHub Packages / GHCR publish | The package already exists with restricted Actions access | The publishing jobs have Packages write, but the package must also allow the consuming repository's workflow to access it. Check the package's Manage Actions access settings. |
| Container SARIF upload | `CODE_SCANNING_TOKEN` is mapped but Code Scanning is unavailable for the private repo | GitHub requires Code Security to be enabled for private/internal SARIF uploads. This upload step is blocking in `publish-container.yml`. If uploads are not used, omit this optional secret mapping; scan results still remain in workflow artifacts. |
| Container SBOM attestation | GitHub artifact attestations are unavailable on the repository's plan | The pinned `actions/attest-sbom` documents Enterprise Cloud as required for private/internal repositories. This step is blocking when analyzed-container SBOMs and SLSA are enabled. |
| Container SLSA provenance | Tagged GHCR release from a private repo with `enable-slsa: true` (the default) | The provenance job is skipped with a notice. The pinned SLSA generator publishes the repository name to the public Rekor log and cannot skip that upload. Set `slsa-private-repository: true` on the container entry to opt in deliberately. |

### SLSA For Private Containers

The upstream private-repository opt-in publishes the repository name to the public Rekor transparency log. The generator signs with keyless cosign and has no option to skip that upload, so there is no way to get L3 provenance for a private repository without the public log entry. Opting in must be a deliberate consumer choice; it is never inferred from repository visibility.

By default a private repository therefore **skips** the external provenance job and prints a notice in the build job. The release still succeeds. BuildKit's own per-platform provenance is still attached to the image, since it does not touch Rekor.

To opt in, set `slsa-private-repository: true` on the relevant **existing container entries** in `artifacts.yml`:

```yaml
containers:
  - name: my-app
    from: [my-app]
    container-file: Containerfile
    slsa-private-repository: true
```

To turn off all provenance, including BuildKit provenance and the GitHub SBOM attestation step, set `enable-slsa: false` instead:

```yaml
containers:
  - name: my-app
    from: [my-app]
    container-file: Containerfile
    enable-slsa: false
```

Use a workflow revision containing the container-flag fix: older forwarding expressions treated `false` as equal to `null` and incorrectly re-enabled SLSA. Both `uses:` and `reusable-ci-ref` should select the fixed revision.

Retain your own container names, source artifacts, and other settings. SBOM file generation and container scanning remain separately configured.

### What Is Already Wired

- Source checkout uses the consumer's automatic `GITHUB_TOKEN` with Contents read.
- Changelog generation now has PR read access through both nested call paths and uses the automatic token.
- Version pushes and GitHub release creation use `RELEASE_TOKEN`.
- Publishing jobs use the automatic token with Packages write where required.
- Workflow artifacts are downloaded from the same run using `actions/download-artifact`; these calls do not require a separate PAT.

Before rollout, run the consumer's selected release paths with its actual secrets, package access, and repository rules. See the short [Migration Guide](migration.md) for the required YAML changes.

## Upstream References

- [SLSA container generator v2.1.0: private repositories](https://github.com/slsa-framework/slsa-github-generator/blob/v2.1.0/internal/builders/container/README.md#private-repositories)
- [Pinned SBOM attestation action: availability](https://github.com/actions/attest-sbom/blob/4651f806c01d8637787e274ac3bdf724ef169f34/README.md)
- [GitHub: SARIF upload requires Code Security in private repositories](https://docs.github.com/en/code-security/reference/code-scanning/sarif-files/troubleshoot-sarif-uploads/ghas-required)
