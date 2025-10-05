# Scripts

Shell scripts for SBOM generation and release validation.

```
scripts/
├── sbom/
│   └── generate-sbom.sh
└── validation/
    ├── validate-tag-format.sh
    ├── validate-tag-signature.sh
    └── validate-tag-commit.sh
```

---

## SBOM Generation

### generate-sbom.sh

Generates SBOM files in SPDX 2.3 and CycloneDX 1.6 JSON formats.

**Syntax:**
```bash
bash generate-sbom.sh [PROJECT_TYPE] [LAYERS] [VERSION] [PROJECT_NAME] [WORKING_DIR] [CONTAINER_IMAGE]
```

**Parameters:**

| Parameter | Default | Example |
|-----------|---------|---------|
| `PROJECT_TYPE` | `auto` | `maven`, `npm`, `gradle` |
| `LAYERS` | `source` | `source,artifact,containerimage` |
| `VERSION` | auto-detect | `0.5.13` |
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
bash generate-sbom.sh maven "source,artifact" "0.5.13" "my-app"

# Container SBOM
bash generate-sbom.sh maven "containerimage" "0.5.13" "my-app" "." "ghcr.io/org/app@sha256:..."
```

**Auto-detection:**
- Project type: pom.xml → maven, package.json → npm, build.gradle → gradle
- Version: `mvn help:evaluate -Dexpression=project.version` or `jq -r .version package.json`
- Name: artifactId, package name, or repository name

**Output:**
- SPDX: `{name}-{version}-{layer}-sbom.spdx.json`
- CycloneDX: `{name}-{version}-{layer}-sbom.cyclonedx.json`

---

## Tag Validation

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