<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Scripts

Shell scripts used by reusable CI workflows for building, validating, publishing, and summarizing.

```text
scripts/
├── android/
│   ├── build-gradle.sh                       # Run Gradle build tasks with optional test skip
│   ├── decode-keystore.sh                    # Decode Base64 Android keystore to file
│   ├── generate-artifact-names.sh            # Generate standardized APK/AAB artifact names
│   ├── get-version-info.sh                   # Read version from gradle.properties
│   ├── list-built-artifacts.sh               # List discovered Android build artifacts
│   └── resolve-build-tasks.sh               # Resolve Gradle build tasks from flavor/type
├── apple/
│   ├── archive-app.sh                        # Build Xcode archive (.xcarchive)
│   ├── export-ipa.sh                         # Export IPA from Xcode archive
│   ├── get-version-info.sh                   # Read version from Xcode project/workspace
│   ├── list-built-artifacts.sh               # List discovered Apple build artifacts
│   └── setup-code-signing.sh                 # Configure Xcode code signing keychain
├── ci/
│   ├── env.sh                                # CI platform environment abstraction
│   ├── install-syft.sh                       # Install Syft SBOM generator
│   ├── install-trivy.sh                      # Install Trivy vulnerability scanner
│   ├── output.sh                             # CI output and summary helpers
│   └── stage-result.sh                       # Stage result aggregation helpers
├── config/
│   ├── expand-sboms.sh                       # Expand the sboms enum to a JSON array of CISA layer names
│   ├── get-file-pattern.sh                   # Get file pattern by project type (supports env vars or positional args)
│   └── parse-artifacts-config.sh             # Parse artifacts.yml configuration
├── container/
│   ├── build-project.sh                      # Build project before container image creation
│   ├── extract-npm-tarball.sh                # Extract NPM tarball for container builds
│   ├── resolve-image-name.sh                # Resolve full container image name with registry
│   ├── validate-artifacts.sh                 # Validate artifacts exist before container build
│   ├── validate-containerfile.sh             # Validate Containerfile/Dockerfile exists
│   ├── validate-namespace.sh                 # Validate container image namespace matches repo
│   └── write-image-details.sh               # Write container image details to GITHUB_OUTPUT
├── plan/
│   ├── resolve-release-plan.sh               # Resolve release plan from policy and context JSON
│   ├── write-dev-release-interface.sh        # Write dev release orchestrator interface outputs
│   ├── write-pr-interface.sh                 # Write PR orchestrator interface outputs
│   └── write-release-interface.sh            # Write release orchestrator interface outputs
├── registry/
│   └── validate-auth.sh                      # Validate registry authentication (supports env vars or positional args)
├── security/
│   └── scan-dependencies.sh                  # Scan dependencies for vulnerabilities (Trivy)
├── release/
│   ├── create-github-release.sh              # Create GitHub Release with artifacts
│   ├── create-sbom-zip.sh                    # Package SBOM layers into ZIP archive (optional GPG signing)
│   ├── generate-checksums.sh                 # Generate SHA256 checksums for release artifacts
│   ├── prepare-release-notes.sh              # Prepare release notes from changelog
│   ├── resolve-artifact-name.sh             # Resolve artifact name from repo/config
│   ├── resolve-release-metadata.sh           # Resolve release metadata (version, tag, branch)
│   └── sign-release-artifacts.sh             # GPG-sign release artifacts
├── sbom/
│   ├── find-container-sbom.sh                # Find container SBOM files in artifacts
│   ├── generate-container-sbom-artifacts.sh  # Generate container SBOM artifact outputs
│   ├── generate-container-sbom.sh            # Generate container image SBOMs with Syft
│   └── generate-sboms.sh                     # Generate SPDX/CycloneDX SBOMs for all layers
├── summary/
│   ├── write-android-build-summary.sh        # Android build step summary
│   ├── write-appstore-summary.sh             # Apple App Store publish step summary
│   ├── write-build-stage-result.sh           # Release build stage result output
│   ├── write-dev-build-stage-result.sh       # Dev build stage result output
│   ├── write-dev-publish-stage-result.sh     # Dev publish stage result output
│   ├── write-dev-release-summary.sh          # Dev release step summary
│   ├── write-google-play-summary.sh          # Google Play publish step summary
│   ├── write-gradle-build-summary.sh         # Gradle build step summary
│   ├── write-maven-build-summary.sh          # Maven build step summary
│   ├── write-npm-build-summary.sh            # NPM build step summary
│   ├── write-pr-quality-stage-result.sh      # PR quality stage result output
│   ├── write-pr-summary.sh                   # PR pipeline step summary
│   ├── write-prepare-stage-result.sh         # Release prepare stage result output
│   ├── write-prerequisites-summary.sh        # Release validation report step summary
│   ├── write-publish-stage-result.sh         # Release publish stage result output
│   ├── write-quality-check-status.sh         # Quality check status table step summary
│   ├── write-release-summary.sh              # Release pipeline step summary
│   └── write-xcode-build-summary.sh          # Xcode build step summary
├── validate/
│   ├── authorization.sh                     # Validate release actor authorization
│   ├── bot-permissions.sh                   # Validate bot account permissions
│   ├── workflow-input-defaults.sh            # Validate workflow input defaults
│   ├── github-token.sh                      # Validate GitHub token type and permissions
│   ├── mavencentral-credentials.sh          # Validate Maven Central credentials are set
│   ├── tag-commit.sh                        # Verify tag commit in branch history
│   ├── tag-format.sh                        # Verify semantic version format
│   ├── tag-signature.sh                     # Verify GPG/SSH tag signature
│   └── tag-uniqueness.sh                    # Verify tag is unique across remotes
├── version/
│   ├── bump-version.sh                       # Update version in build config files
│   ├── generate-dev-version.sh               # Generate dev version string from branch/SHA
│   ├── move-tag.sh                           # Move existing git tag to new commit
│   ├── read-minimal-changelog.sh             # Read latest changelog entry
│   └── validate-full-changelog.sh            # Validate CHANGELOG.md exists and has content
```

---

## Android

Scripts for Android Gradle builds.

| Script | Purpose |
|--------|---------|
| `build-gradle.sh` | Runs `./gradlew` with task list, optionally skipping tests |
| `decode-keystore.sh` | Decodes Base64-encoded `ANDROID_KEYSTORE_BASE64` to a keystore file, outputs path |
| `generate-artifact-names.sh` | Generates standardized names for APK/AAB files with date stamp |
| `get-version-info.sh` | Reads `versionName` and `versionCode` from `gradle.properties` |
| `list-built-artifacts.sh` | Lists APK/AAB files found after build |
| `resolve-build-tasks.sh` | Resolves Gradle task list from flavor, build types, and AAB settings |

---

## Apple

Scripts for Xcode/iOS builds, code signing, and version info.

| Script | Purpose |
|--------|---------|
| `archive-app.sh` | Runs `xcodebuild archive` with workspace/project, scheme, and code signing config |
| `export-ipa.sh` | Exports IPA from `.xcarchive` using decoded export options plist |
| `get-version-info.sh` | Reads version and build number from Xcode project/workspace |
| `list-built-artifacts.sh` | Lists IPA and xcarchive files found after build |
| `setup-code-signing.sh` | Configures code signing keychain with certificate and provisioning profile |

---

## Config

Scripts for build configuration resolution.

| Script | Purpose |
|--------|---------|
| `get-file-pattern.sh` | Returns git file pattern by project type (supports env vars or positional args) |
| `parse-artifacts-config.sh` | Parses `artifacts.yml` to resolve build targets, container config, and publish targets |

---

## Container

Scripts for container image builds and validation.

| Script | Purpose |
|--------|---------|
| `build-project.sh` | Runs project build (Maven/NPM/Gradle) before container image creation |
| `extract-npm-tarball.sh` | Extracts NPM tarball into context directory for container builds |
| `resolve-image-name.sh` | Resolves full image name with registry prefix from inputs |
| `write-image-details.sh` | Writes image name, digest, and tags to `GITHUB_OUTPUT` |
| `validate-artifacts.sh` | Checks build artifacts exist before container build; warns on Containerfile rebuilds |
| `validate-containerfile.sh` | Validates that the Containerfile/Dockerfile exists at the given path |
| `validate-namespace.sh` | Validates container image namespace matches repository owner |

---

## Plan

Scripts that produce orchestrator interface outputs (context JSON, policy JSON).

| Script | Purpose |
|--------|---------|
| `resolve-release-plan.sh` | Resolves release execution plan from policy and context JSON |
| `write-dev-release-interface.sh` | Writes dev release orchestrator context and policy outputs |
| `write-pr-interface.sh` | Writes PR orchestrator context and policy outputs |
| `write-release-interface.sh` | Writes release orchestrator context and policy outputs |

---

## Registry

| Script | Purpose |
|--------|---------|
| `validate-auth.sh` | Validates registry authentication configuration (supports env vars or positional args) |

---

## Security

Scripts for dependency and vulnerability scanning.

| Script | Purpose |
|--------|---------|
| `scan-dependencies.sh` | Scans project dependencies for known vulnerabilities using Trivy. Supports diff mode (only new vulnerabilities fail) and full scan mode. Produces SARIF for GitHub Code Scanning annotations. |

---

## Release

Scripts for release artifact management and GitHub Release creation.

| Script | Purpose |
|--------|---------|
| `create-github-release.sh` | Creates GitHub Release with artifacts, signatures, SBOMs, and checksums |
| `create-sbom-zip.sh` | Packages all SBOM layers (source, analyzed-artifact, analyzed-container) into ZIP; optionally GPG-signs |
| `generate-checksums.sh` | Generates SHA256 checksums for all release artifacts |
| `prepare-release-notes.sh` | Extracts release notes from changelog for the current version |
| `resolve-artifact-name.sh` | Resolves artifact name from repository name or config |
| `resolve-release-metadata.sh` | Resolves release metadata (version, tag, branch, actor) |
| `sign-release-artifacts.sh` | GPG-signs release artifacts (JARs, ZIPs, tarballs, checksums) |

---

## SBOM

Scripts for Software Bill of Materials generation.

| Script | Purpose |
|--------|---------|
| `find-container-sbom.sh` | Finds container SBOM files in artifact directories |
| `generate-container-sbom-artifacts.sh` | Generates container SBOM artifact outputs for download |
| `generate-container-sbom.sh` | Generates container image SBOMs using Syft |
| `generate-sboms.sh` | Main SBOM generator — produces SPDX 2.3 and CycloneDX 1.6 for all layers |

### generate-sboms.sh

Generates SBOM files in SPDX 2.3 and CycloneDX 1.6 JSON formats using Syft.

**Syntax:**

```bash
bash generate-sboms.sh [PROJECT_TYPE] [LAYERS] [VERSION] [PROJECT_NAME] [WORKING_DIR] [CONTAINER_IMAGE]
```

**Parameters:**

| Parameter | Default | Example |
|-----------|---------|---------|
| `PROJECT_TYPE` | `auto` | `maven`, `npm`, `gradle` |
| `LAYERS` | `source` | `source,build,analyzed-artifact,analyzed-container` |
| `VERSION` | auto-detect | `1.0.0` |
| `PROJECT_NAME` | auto-detect | `my-app` |
| `WORKING_DIR` | `.` | `/path/to/project` |
| `CONTAINER_IMAGE` | - | `ghcr.io/org/app@sha256:...` |

**Layer outputs by project type:**

| Layer | Parameter | Maven | NPM | Gradle |
|-------|-----------|-------|-----|--------|
| Source | `source` | `*-pom-sbom.*` | `*-package-sbom.*` | `*-gradle-sbom.*` |
| Build | `build` | `*-build-sbom.cyclonedx.json` | - | - |
| Analyzed Artifact | `analyzed-artifact` | `*-jar-sbom.*` | `*-tararchive-sbom.*` | `*-jar-sbom.*` |
| Analyzed Container | `analyzed-container` | `*-container-sbom.*` | `*-container-sbom.*` | `*-container-sbom.*` |

---

## Summary

Scripts that write GitHub Actions step summaries and stage result outputs.

**Stage result scripts** produce structured JSON outputs consumed by orchestrators:

| Script | Used by |
|--------|---------|
| `write-build-stage-result.sh` | Release build stage |
| `write-prepare-stage-result.sh` | Release prepare stage |
| `write-publish-stage-result.sh` | Release publish stage |
| `write-dev-build-stage-result.sh` | Dev build stage |
| `write-dev-publish-stage-result.sh` | Dev publish stage |
| `write-pr-quality-stage-result.sh` | PR quality stage |

**Validation and quality summary scripts**:

| Script | Used by |
|--------|---------|
| `write-prerequisites-summary.sh` | Release prerequisites validation |
| `write-quality-check-status.sh` | PR quality check results |

**Pipeline summary scripts** produce human-readable step summaries:

| Script | Used by |
|--------|---------|
| `write-release-summary.sh` | Release orchestrator |
| `write-dev-release-summary.sh` | Dev release orchestrator |
| `write-pr-summary.sh` | PR orchestrator |

**Build summary scripts** produce per-build-type step summaries:

| Script | Used by |
|--------|---------|
| `write-maven-build-summary.sh` | Maven build workflow |
| `write-npm-build-summary.sh` | NPM build workflow |
| `write-gradle-build-summary.sh` | Gradle build workflow |
| `write-android-build-summary.sh` | Android build workflow |
| `write-xcode-build-summary.sh` | Xcode build workflow |
| `write-appstore-summary.sh` | App Store publish workflow |
| `write-google-play-summary.sh` | Google Play publish workflow |

---

## Validate

Scripts for release prerequisite validation.

| Script | Purpose |
|--------|---------|
| `authorization.sh` | Validates release actor is authorized |
| `bot-permissions.sh` | Validates bot account has required permissions |
| `workflow-input-defaults.sh` | Validates workflow input defaults match documentation |
| `github-token.sh` | Validates GitHub token type and permissions |
| `mavencentral-credentials.sh` | Validates Maven Central credentials are set |
| `tag-commit.sh` | Verifies tag commit exists in target branch history |
| `tag-format.sh` | Verifies tag follows semantic versioning (`vX.Y.Z[-prerelease]`) |
| `tag-signature.sh` | Verifies tag is annotated and GPG/SSH signed |
| `tag-uniqueness.sh` | Verifies tag is unique across remotes |

---

## Version

Scripts for version management.

| Script | Purpose |
|--------|---------|
| `bump-version.sh` | Updates version in pom.xml / package.json / gradle.properties / xcconfig |
| `generate-dev-version.sh` | Generates dev version string from branch name and short SHA |
| `move-tag.sh` | Moves existing git tag to current HEAD |
| `read-minimal-changelog.sh` | Reads the latest entry from CHANGELOG.md |
| `validate-full-changelog.sh` | Validates CHANGELOG.md exists and has content |

### bump-version.sh

**Syntax:**

```bash
bash bump-version.sh <project-type> <version> [working-dir] [gradle-version-file]
```

| Type | Action | Files Updated |
|------|--------|---------------|
| Maven | `mvn versions:set` | `pom.xml` (all modules) |
| NPM | `npm version` | `package.json`, `package-lock.json` |
| Gradle (JVM) | Updates `version=` | `gradle.properties` |
| Gradle Android | Updates `versionName=` and increments `versionCode=` | `gradle.properties` |
| Xcode iOS | Updates MARKETING_VERSION | `versions.xcconfig` |
| Meta | No file update | Changelog only |

---

## Usage in Workflows

Most projects use the orchestrators directly. For custom workflows:

```yaml
- uses: actions/checkout@v4
  with:
    repository: diggsweden/reusable-ci
    path: .reusable-ci
    sparse-checkout: scripts/sbom

- run: |
    bash .reusable-ci/scripts/sbom/generate-sboms.sh \
      maven "source,analyzed-artifact" "$VERSION" "$PROJECT_NAME"
```
