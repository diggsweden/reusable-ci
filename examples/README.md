<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Configuration Examples

Complete working examples for different project types.

> Each example follows one of the two ecosystem patterns (A: build artefact + container COPY; B: container is the build environment). See **[docs/ecosystems.md](../docs/ecosystems.md)** for the full framing and per-ecosystem capability matrices.
>
> Examples target the v3.0.0 workflow contract. Before that release tag and its
> matching runtime images exist, use the branch-testing flow in
> **[docs/runtime-images.md](../docs/runtime-images.md)**.

## Available Examples

- **[Maven Application](maven-app/)** — Java service shipped as a
  multi-platform container to GHCR. Pattern A (build artefact +
  container COPY).
- **[NPM Application](npm-app/)** — Node service published to GitHub
  Packages and shipped as a container. Pattern A.
- **[Gradle JVM](gradle-app/)** — JVM-only Gradle build (libraries,
  plugins, multi-module). Container path not wired; for Android use the
  next example.
- **[Android Application](android-app/)** — APK / AAB build with
  product flavors, signed with the keystore in `secrets:`, optional
  Google Play upload. Uses the Android runtime image.
- **[Rust Application](cargo-app/)** — Cargo workspace shipped as one
  or more containers, with `cargo build` inside the Containerfile
  (Pattern B) and a separate cargo-cyclonedx SBOM step at release.
  Shows the private-registry build-secrets pattern.
- **[Go CLI](go-cli/)** — Standalone binary release via
  `build-go.yml`, cross-compiled per platform with reproducible
  timestamps, attached to the GitHub Release.
- **[Go Service](go-service/)** — Go service shipped as a multi-arch
  container with `cargo build`-equivalent compile inside the
  Containerfile (Pattern B). Optional binary extraction for the
  GitHub Release.
- **[Monorepo](monorepo/)** — Multiple artefacts in one repo, mixed
  project types, one container per artefact. Includes a
  `multi-artifact-container.yml` variant that combines multiple
  artefacts into a single image.

---

## Quick Start

### Using an Example

1. **Navigate to example directory:**
   ```bash
   cd examples/maven-app/  # or npm-app, gradle-app, android-app, cargo-app, go-cli, go-service, monorepo
   ```

2. **Copy files to your project:**
   ```bash
   # Copy artifacts configuration
   cp artifacts.yml /path/to/your/project/.github/

   # Copy workflows
   [ -f pullrequest-workflow.yml ] && cp pullrequest-workflow.yml /path/to/your/project/.github/workflows/
   cp release-workflow.yml /path/to/your/project/.github/workflows/
   ```

3. **Customize configuration:**
   - Update artifact `name`
   - Review artifact config versions and paths
   - Verify paths (working-directory, container-file)

4. **Configure release secrets:**
   - `RELEASE_TOKEN` for version updates and GitHub release creation
   - `RELEASE_GPG_PRIVATE_KEY`, `RELEASE_GPG_PUBLIC_KEY`, and `RELEASE_GPG_PASSPHRASE` for release validation, version bump signing, and artifact signing
   - Target-specific secrets such as Maven Central or Google Play credentials

5. **Create release:**
   ```bash
   git tag -s v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

---

## At a glance

| Example     | Project type   | Container path     | Publishing target               |
|-------------|----------------|--------------------|---------------------------------|
| Maven App   | Maven          | single, GHCR       | GHCR                            |
| NPM App     | NPM            | single, GHCR       | GitHub Packages                 |
| Gradle JVM  | Gradle         | optional           | not wired today                 |
| Android App | Gradle Android | optional           | Google Play                     |
| Cargo App   | Cargo          | one or many        | container only                  |
| Go CLI      | Go             | none               | GitHub Release binaries         |
| Go Service  | Go             | single, multi-arch | container only                  |
| Monorepo    | Maven + NPM    | one per artefact   | GHCR + GitHub Packages          |

---

## Common Modifications

### Add Maven Central Publishing

In a Maven library example:
```yaml
artifacts:
  - name: my-lib
    project-type: maven
    build-type: library  # Required
    require-authorization: true  # Recommended
    publish-to:
      - github-packages
      - maven-central  # Add this
```

**Requirements:**
- Sonatype account
- MAVEN_CENTRAL_USERNAME secret
- MAVEN_CENTRAL_PASSWORD secret

See [Publishing Guide](../docs/publishing.md#maven-central) for setup.

---

### Add NPM GitHub Packages Publishing

In NPM example:
```yaml
artifacts:
  - name: my-package
    project-type: npm
    publish-to:
      - github-packages
```

**Requirements:**
- GitHub Packages access
- Scoped package name such as `@org/package`

npmjs.org production publishing is not implemented yet. Current config
validation rejects `npmjs`; it is reserved for future support.

See [Publishing Guide](../docs/publishing.md#npm-packages-github-packages) for setup.

---

### Disable Container Build

Remove the `containers:` section:
```yaml
artifacts:
  - name: my-app
    project-type: maven
    working-directory: .

# containers: []  # Remove or comment out
```

---

### Multi-Platform Containers

Change platform list:
```yaml
containers:
  - name: my-app
    from: [my-app]
    platforms: linux/amd64,linux/arm64  # Multi-platform (slower)
    # or: linux/amd64                  # Single platform (faster)
```

---

### Per-container security overrides

Every container gate defaults to **on**. Explicit per-container overrides
in `artifacts.yml` propagate through the typed `PlannedContainer` plan
to `publish-container.yml` — no workflow input plumbing needed:

```yaml
containers:
  - name: my-app
    from: [my-app]
    enable-slsa: false        # Disable SLSA provenance for this container only
    enable-scan: false        # Disable the Trivy CVE gate (also skips the scan)
    scan-severity: "CRITICAL" # Or: keep the scan but only fail on CRITICAL
# Container SBOM (analyzed-container) scan is derived from each source
# artefact's `sboms`. To skip the scan, set the source artefact's sboms to
# exclude `analyzed-container`, e.g. `sboms: build,analyzed-artifact`.
```

**Note:** Disabling gates removes the corresponding pipeline guarantee —
not recommended for production unless you have a compensating control
elsewhere (e.g. policy-as-code in the registry).

---

## Testing Your Configuration

### 1. Validate YAML Syntax

```bash
# Install yamllint
pip install yamllint

# Check syntax
yamllint .reusable-ci/artifacts.yml
```

### 2. Test with Snapshot Workflow

Create `.github/workflows/release-snapshot-workflow.yml`:
```yaml
on:
  push:
    branches: [feat/test-config]

jobs:
  snapshot-release:
    uses: diggsweden/reusable-ci/.github/workflows/release-snapshot-orchestrator.yml@v3.0.0
    with:
      reusable-ci-binary-ref: v3.0.0
      artifacts-config: .reusable-ci/artifacts.yml
    permissions:
      contents: read
      packages: write
    secrets: inherit
```

Push to test branch:
```bash
git checkout -b feat/test-config
git push origin feat/test-config
```

### 3. Create Test Release

```bash
git tag -s v0.0.1-rc.1 -m "Test configuration"
git push origin v0.0.1-rc.1
```

---

## Troubleshooting

### "artifacts.yml not found"
**Problem:** Workflow can't find configuration

**Solution:** Verify path in workflow:
```yaml
with:
  artifacts-config: .reusable-ci/artifacts.yml  # Must match actual path
```

### "Containerfile not found"
**Problem:** Container build fails

**Solution:** Check `container-file` path:
```yaml
containers:
  - container-file: Containerfile  # Must exist at this path
```

### Build succeeds but nothing published
**Problem:** Missing publishing configuration

**Solution:** Add a supported publishing target for package-registry outputs, or
configure `containers:` for container publishing. Maven applications generally
publish as containers, not GitHub Packages.

```yaml
publish-to:
  - github-packages  # NPM packages and supported libraries
```

---

## See Also

- [Artifacts Reference](../docs/artifacts-reference.md) - Complete field documentation
- [Publishing Guide](../docs/publishing.md) - Registry setup instructions
