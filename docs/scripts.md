<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Scripts

Shell scripts for SBOM generation and release validation.

```text
scripts/
├── sbom/
│   └── generate-sbom.sh                    # Generate SPDX/CycloneDX SBOMs with Syft
├── container/
│   ├── determine-image-name.sh             # Resolve full container image name with registry
│   └── verify-artifacts.sh                 # Check artifacts exist before container build
├── release/
│   ├── create-sbom-zip.sh                  # Package all SBOM layers into ZIP archive
│   └── generate-checksums.sh               # Generate SHA256 checksums for all artifacts
├── validation/
│   ├── generate-prerequisites-summary.sh   # Generate release validation report
│   ├── validate-tag-format.sh              # Verify semantic version format
│   ├── validate-tag-signature.sh           # Check GPG/SSH tag signature
│   └── validate-tag-commit.sh              # Verify tag commit in branch history
└── version/
    └── bump-version.sh                     # Update version in pom.xml/package.json/gradle.properties
```

---

## SBOM Generation

### generate-sbom.sh

Generates SBOM files in SPDX 2.3 and CycloneDX 1.6 JSON formats using Syft.

**Syntax:**

```bash
bash generate-sbom.sh [PROJECT_TYPE] [LAYERS] [VERSION] [PROJECT_NAME] [WORKING_DIR] [CONTAINER_IMAGE]
```

**Parameters:**

| Parameter | Default | Example |
|-----------|---------|---------|
| `PROJECT_TYPE` | `auto` | `maven`, `npm`, `gradle` |
| `LAYERS` | `source` | `source,artifact,containerimage` |
| `VERSION` | auto-detect | `1.0.0` |
| `PROJECT_NAME` | auto-detect | `my-app` |
| `WORKING_DIR` | `.` | `/path/to/project` |
| `CONTAINER_IMAGE` | - | `ghcr.io/org/app@sha256:...` |

**Layer outputs by project type:**

| Layer | Parameter | Maven | NPM | Gradle |
|-------|-----------|-------|-----|--------|
| Source | `source` | `*-pom-sbom.*` | `*-package-sbom.*` | `*-gradle-sbom.*` |
| Artifact | `artifact` | `*-jar-sbom.*` | `*-tararchive-sbom.*` | `*-jar-sbom.*` |
| Container | `containerimage` | `*-container-sbom.*` | `*-container-sbom.*` | `*-container-sbom.*` |

**Examples:**

```bash
# Auto-detect from pom.xml
bash generate-sbom.sh maven source

# Explicit version and name
bash generate-sbom.sh maven "source,artifact" "1.0.0" "my-app"

# Container SBOM
bash generate-sbom.sh maven "containerimage" "1.0.0" "my-app" "." "ghcr.io/org/app@sha256:..."
```

**Auto-detection:**

- Project type: pom.xml → maven, package.json → npm, build.gradle → gradle
- Version: `mvn help:evaluate -Dexpression=project.version` or `jq -r .version package.json`
- Name: artifactId, package name, or repository name

**Tool:**

- Uses [Syft](https://github.com/anchore/syft) for SBOM generation
- Auto-installs if not available

**Output:**

- SPDX: `{name}-{version}-{layer}-sbom.spdx.json`
- CycloneDX: `{name}-{version}-{layer}-sbom.cyclonedx.json`

---

## Container Scripts

### determine-image-name.sh

Determines full container image name with registry prefix.

**Syntax:**

```bash
bash determine-image-name.sh <registry> <image-name> <repository> <repository-owner>
```

**Parameters:**

| Parameter | Example |
|-----------|---------|
| `REGISTRY` | `ghcr.io`, `docker.io` |
| `IMAGE_NAME` | `my-app` or `org/my-app` |
| `REPOSITORY` | `reusable-ci` |
| `REPOSITORY_OWNER` | `diggsweden` |

**Logic:**

- If `IMAGE_NAME` is empty, uses `REPOSITORY` name
- If `IMAGE_NAME` has no `/` or `.`, adds registry prefix:
  - Docker Hub: `owner/image-name`
  - Other registries: `registry/image-name`
- Otherwise uses `IMAGE_NAME` as-is

**Examples:**

```bash
# ghcr.io with simple name
bash determine-image-name.sh ghcr.io my-app reusable-ci diggsweden
# Output: ghcr.io/my-app

# Docker Hub with simple name
bash determine-image-name.sh docker.io my-app reusable-ci diggsweden
# Output: diggsweden/my-app

# Full name (unchanged)
bash determine-image-name.sh ghcr.io ghcr.io/org/app reusable-ci diggsweden
# Output: ghcr.io/org/app
```

---

### verify-artifacts.sh

Verifies artifacts exist before container build.

**Syntax:**

```bash
bash verify-artifacts.sh <project-type> <artifact-dir>
```

**Parameters:**

| Parameter | Example |
|-----------|---------|
| `PROJECT_TYPE` | `maven`, `npm` |
| `ARTIFACT_DIR` | `./target`, `./dist` |

**Checks:**

- Maven: Verifies `*.jar` files exist
- NPM: Verifies directory contains files
- Warnings only (non-blocking) - container may build from source

**Examples:**

```bash
# Verify Maven artifacts
bash verify-artifacts.sh maven ./target

# Verify NPM artifacts
bash verify-artifacts.sh npm ./dist
```

---

## Release Scripts

### create-sbom-zip.sh

Creates ZIP archive containing all 3 SBOM layers.

**Syntax:**

```bash
bash create-sbom-zip.sh [project-name] [version]
```

**Parameters:**

| Parameter | Default | Example |
|-----------|---------|---------|
| `PROJECT_NAME` | Git repo name | `my-app` |
| `VERSION` | `unknown` | `1.0.0` or `v1.0.0` |

**Behavior:**

- Skips if no SBOMs found
- Includes all 3 layers:
  - Layer 1: Source SBOMs (`*-pom-sbom.*`, `*-package-sbom.*`, `*-gradle-sbom.*`)
  - Layer 2: Artifact SBOMs (`*-jar-sbom.*`, `*-tararchive-sbom.*`)
  - Layer 3: Container SBOMs (from `./sbom-artifacts/`)
- Output: `{project-name}-{version}-sboms.zip`

**Examples:**

```bash
# Auto-detect from git
bash create-sbom-zip.sh

# Explicit name and version
bash create-sbom-zip.sh my-app 1.0.0
```

---

### generate-checksums.sh

Generates SHA256 checksums for all release artifacts.

**Syntax:**

```bash
bash generate-checksums.sh [output-file] [release-dir] [attach-patterns] [sbom-dir]
```

**Parameters:**

| Parameter | Default | Example |
|-----------|---------|---------|
| `OUTPUT_FILE` | `checksums.sha256` | `SHA256SUMS.txt` |
| `RELEASE_ARTIFACTS_DIR` | `./release-artifacts` | `./dist` |
| `ATTACH_PATTERNS` | - | `*.jar,*.zip` |
| `SBOM_DIR` | `./sbom-artifacts` | `./sboms` |

**Checksums:**

1. Release artifacts directory (`*.jar`, `*.zip`, etc.)
2. Attached files matching patterns
3. Container SBOMs from sbom-artifacts
4. All 3-layer SBOMs (source, artifact, container)

**Examples:**

```bash
# Default behavior
bash generate-checksums.sh

# Custom output and patterns
bash generate-checksums.sh SHA256SUMS.txt ./dist "*.jar,*.tar.gz"
```

---

## Tag Validation

### generate-prerequisites-summary.sh

Generates comprehensive release prerequisites validation report in GitHub Actions summary.

**Syntax:**

```bash
bash generate-prerequisites-summary.sh
```

**Environment Variables:**

| Variable | Purpose |
|----------|---------|
| `TAG_NAME` | Release tag name |
| `COMMIT_SHA` | Tagged commit SHA |
| `REF_TYPE` | `tag` or `branch` |
| `PROJECT_TYPE` | `maven`, `npm`, `gradle` |
| `BUILD_TYPE` | `app` or `lib` |
| `CONTAINER_REGISTRY` | Container registry URL |
| `SIGN_ARTIFACTS` | `true`/`false` |
| `CHECK_AUTHORIZATION` | `true`/`false` |
| `JOB_STATUS` | `success`/`failure` |

**Report Sections:**

1. **Release Tag** - Tag info, signature status, message
2. **Tagged Commit** - Commit author, date, signature, message
3. **Configuration** - Project settings, registry, signing
4. **Required Secrets** - Validation of all required secrets:
   - GPG keys (if signing enabled)
   - GitHub tokens
   - Maven Central credentials (if publishing)
   - NPM token (if publishing)
5. **Validation Results** - Summary table with pass/fail status

**Examples:**

```bash
# In GitHub Actions workflow
- env:
    TAG_NAME: ${{ github.ref_name }}
    COMMIT_SHA: ${{ github.sha }}
    REF_TYPE: tag
    PROJECT_TYPE: maven
    SIGN_ARTIFACTS: "true"
  run: bash .reusable-ci/scripts/validation/generate-prerequisites-summary.sh
```

---

### validate-tag-format.sh

Checks tag follows semantic versioning.

**Syntax:**

```bash
./validate-tag-format.sh <tag-name>
```

**Valid formats:**

- `v1.0.0`
- `v2.3.4-beta.1`
- `v1.0.0-rc.2`
- `v1.0.0-SNAPSHOT`

---

### validate-tag-signature.sh

Checks tag is signed with GPG or SSH.

**Syntax:**

```bash
./validate-tag-signature.sh <tag-name> <github-repository> [gpg-public-key]
```

**Checks:**

- Tag is annotated (not lightweight)
- Has GPG or SSH signature
- Verifies signature if public key provided

---

### validate-tag-commit.sh

Checks tag commit exists in branch history.

**Syntax:**

```bash
./validate-tag-commit.sh <tag-name> <branch-name>
```

**Checks:**

- Tag commit in branch history
- Tag not ahead of branch HEAD
- Valid commit reference

---

## Usage in Workflows

For advanced users building custom workflows (most projects use the orchestrators).

**SBOM generation:**

```yaml
- uses: actions/checkout@v4
  with:
    repository: diggsweden/reusable-ci
    path: .reusable-ci
    sparse-checkout: scripts/sbom

- run: |
    bash .reusable-ci/scripts/sbom/generate-sbom.sh \
      maven "source,artifact" "$VERSION" "$PROJECT_NAME"
```

**Tag validation:**

```yaml
- uses: actions/checkout@v4
  with:
    repository: diggsweden/reusable-ci
    path: .reusable-ci
    sparse-checkout: scripts/validation

- run: bash .reusable-ci/scripts/validation/validate-tag-format.sh "${{ github.ref_name }}"

- env:
    OSPO_BOT_GPG_PUB: ${{ secrets.OSPO_BOT_GPG_PUB }}
  run: bash .reusable-ci/scripts/validation/validate-tag-signature.sh "${{ github.ref_name }}" "${{ github.repository }}" "$OSPO_BOT_GPG_PUB"

- run: bash .reusable-ci/scripts/validation/validate-tag-commit.sh "${{ github.ref_name }}" "main"
```

---

## Version Management

### bump-version.sh

Updates project version in build configuration files.

**Syntax:**

```bash
bash bump-version.sh <project-type> <version> [working-dir] [gradle-version-file]
```

**Parameters:**

| Parameter | Default | Example |
|-----------|---------|---------|
| `PROJECT_TYPE` | - | `maven`, `npm`, `gradle` |
| `VERSION` | - | `1.0.0` |
| `WORKING_DIR` | `.` | `/path/to/project` |
| `GRADLE_VERSION_FILE` | `gradle.properties` | `version.properties` |

**Project-specific behavior:**

| Type | Action | Files Updated |
|------|--------|---------------|
| Maven | `mvn versions:set` | `pom.xml` |
| NPM | `npm version` | `package.json` |
| Gradle | Updates properties | `gradle.properties` |

**Gradle specifics:**

- Updates `versionName=` property
- Auto-increments `versionCode=` (for Android)
- Creates properties if missing

**Examples:**

```bash
# Maven project
bash bump-version.sh maven 1.0.0

# NPM in subdirectory
bash bump-version.sh npm 2.3.4 ./packages/app

# Gradle with custom properties file
bash bump-version.sh gradle 3.0.0 . version.properties
```

---

## Examples

**SBOM:**

```bash
generate-sbom.sh maven "source,artifact"
```

**Validation:**

```bash
validate-tag-format.sh v1.0.0 && \
validate-tag-signature.sh v1.0.0 diggsweden/repo && \
validate-tag-commit.sh v1.0.0 main
```
