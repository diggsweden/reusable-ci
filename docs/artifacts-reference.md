<!--
SPDX-FileCopyrightText: 2025 The Reusable CI Authors

SPDX-License-Identifier: CC0-1.0
-->

# Artifacts Reference

Complete reference for `artifacts.yml` configuration format.

## Overview

The `artifacts.yml` file defines what to build and where to publish. It consists of two main sections:

```yaml
artifacts:
  - name: my-artifact
    # ... artifact configuration

containers:
  - name: my-container
    from: [my-artifact]
    # ... container configuration
```

## Artifacts Section

### Artifact Required Fields

#### Artifact `name`

- **Type:** `string`
- **Description:** Unique identifier for this artifact
- **Used for:** Referencing in containers `from:` field, artifact upload names
- **Example:** `backend-api`, `frontend-ui`, `shared-lib`

#### `project-type`

- **Type:** `string`
- **Description:** Build system type
- **Valid values:** `maven`, `npm`, `gradle`
- **Example:** `project-type: maven`

#### `working-directory`

- **Type:** `string`
- **Description:** Path to project root (relative to repository root)
- **Contains:** `pom.xml` (Maven), `package.json` (NPM), `build.gradle` (Gradle)
- **Example:** `.`, `services/backend`, `packages/frontend`

---

### Artifact Optional Fields

#### `build-type`

- **Type:** `string`
- **Description:** Build type (affects Maven/Gradle behavior)
- **Valid values:** `application` (default), `library`
- **Default:** `application`
- **Applies to:** Maven and Gradle only
- **Example:** `build-type: library`
- **Behavior:**
  - `application`: Builds with `mvn package`
  - `library`: Builds with `mvn install`, generates javadoc and sources JARs

#### `require-authorization`

- **Type:** `boolean`
- **Description:** Require user to be in authorized list for releases
- **Default:** `false`
- **Use case:** Production libraries that need release approval
- **Example:** `require-authorization: true`
- **Requires:** `AUTHORIZED_RELEASE_DEVELOPERS` secret set

#### `publish-to`

- **Type:** `array of strings`
- **Description:** Publishing targets for built artifacts
- **Default:** `[github-packages]`
- **Valid values:** `github-packages`, `maven-central`, `npmjs`
- **Example:**

  ```yaml
  publish-to:
    - github-packages
    - maven-central
  ```

- **Behavior:** Workflows only run if target is listed

---

### Configuration Fields (Maven/Gradle)

#### `config.java-version`

- **Type:** `string` or `number`
- **Description:** JDK version for Maven/Gradle builds
- **Default:** `21`
- **Valid values:** `8`, `11`, `17`, `21`, `23`
- **Example:** `java-version: 21`

#### `config.settings-path`

- **Type:** `string`
- **Description:** Path to Maven settings.xml (relative to working-directory)
- **Default:** None (uses default Maven settings)
- **Example:** `settings-path: .mvn/settings.xml`
- **Use case:** Custom Maven repository configuration

---

### Configuration Fields (NPM)

#### `config.node-version`

- **Type:** `string` or `number`
- **Description:** Node.js version for NPM builds
- **Default:** `22`
- **Valid values:** `18`, `20`, `22`, `23`
- **Example:** `node-version: 22`

#### `config.npm-tag`

- **Type:** `string`
- **Description:** NPM distribution tag for publishing
- **Default:** `latest`
- **Valid values:** `latest`, `next`, `beta`, `alpha`
- **Example:** `npm-tag: latest`

---

### Configuration Fields (Gradle)

#### `config.gradle-tasks`

- **Type:** `string`
- **Description:** Gradle tasks to execute
- **Default:** `build`
- **Example:** `gradle-tasks: build assembleDemoRelease`

#### `config.gradle-version-file`

- **Type:** `string`
- **Description:** File containing version properties
- **Default:** `gradle.properties`
- **Example:** `gradle-version-file: gradle.properties`

---

## Containers Section

Containers reference artifacts via the `from:` field and are built after all artifacts complete.

### Container Required Fields

#### Container `name`

- **Type:** `string`
- **Description:** Container image name (becomes part of image tag)
- **Example:** `backend-api`, `frontend-ui`
- **Resulting image:** `ghcr.io/org/repo/backend-api:v1.0.0`

#### `from`

- **Type:** `array of strings`
- **Description:** List of artifact names to include in this container
- **Must reference:** Existing artifact names from `artifacts[]` section
- **Example:** `from: [backend-api]` (single artifact)
- **Example:** `from: [api, worker, web]` (multi-artifact container)

#### `container-file`

- **Type:** `string`
- **Description:** Path to Containerfile/Dockerfile (relative to repository root)
- **Example:** `Containerfile`, `services/backend/Containerfile`

---

### Container Optional Fields

#### `context`

- **Type:** `string`
- **Description:** Docker build context directory
- **Default:** `.` (repository root)
- **Example:** `context: services/backend`

#### `platforms`

- **Type:** `string` (comma-separated)
- **Description:** Target CPU architectures for multi-platform builds
- **Default:** `linux/amd64`
- **Example:** `platforms: linux/amd64,linux/arm64`
- **Performance:** Multi-platform builds take ~2x longer

#### `enable-slsa`

- **Type:** `boolean`
- **Description:** Generate SLSA provenance attestation
- **Default:** `true`
- **Requires:** `id-token: write`, `actions: read` permissions
- **Example:** `enable-slsa: true`

#### `enable-sbom`

- **Type:** `boolean`
- **Description:** Generate SPDX and CycloneDX SBOMs
- **Default:** `true`
- **Requires:** `attestations: write` permission
- **Example:** `enable-sbom: true`

#### `enable-scan`

- **Type:** `boolean`
- **Description:** Run Trivy vulnerability scan
- **Default:** `true`
- **Requires:** `security-events: write` permission
- **Example:** `enable-scan: true`

---

## Publishing Targets

### `github-packages`

- **Description:** GitHub Packages registry
- **Requirements:** `GITHUB_TOKEN` (automatic)
- **Applies to:** Maven, NPM, Gradle
- **Registry:** `ghcr.io` (containers), `npm.pkg.github.com` (NPM)

### `maven-central`

- **Description:** Maven Central (Sonatype OSSRH)
- **Requirements:**
  - `MAVENCENTRAL_USERNAME` secret
  - `MAVENCENTRAL_PASSWORD` secret
  - `build-type: library` (required)
- **Applies to:** Maven only
- **Note:** Requires Sonatype account and approved groupId

### `npmjs`

- **Description:** Public npmjs.org registry
- **Requirements:** `NPM_TOKEN` secret
- **Applies to:** NPM only
- **Note:** Package must be scoped or publicly available

---

## Quick Start Examples

### Single Artifact (Maven)

**`.github/artifacts.yml`**
```yaml
artifacts:
  - name: my-app
    project-type: maven
    working-directory: .
    build-type: application
    config:
      java-version: 21
```

**`.github/workflows/release-workflow.yml`**
```yaml
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2
    with:
      artifacts-config: .github/artifacts.yml
      release-publisher: github-cli
```

### Single Artifact with Container (Maven)

**`.github/artifacts.yml`**
```yaml
artifacts:
  - name: my-app
    project-type: maven
    working-directory: .
    build-type: application
    config:
      java-version: 21

containers:
  - name: my-app
    from: [my-app]
    container-file: Containerfile
    context: .
    platforms: linux/amd64,linux/arm64
```

### Maven Library (Multiple Targets)

```yaml
artifacts:
  - name: my-lib
    project-type: maven
    working-directory: library
    build-type: library
    require-authorization: true
    publish-to:
      - github-packages
      - maven-central
    config:
      java-version: 21
      settings-path: .mvn/settings.xml
```

### NPM Application

```yaml
artifacts:
  - name: my-ui
    project-type: npm
    working-directory: frontend
    config:
      node-version: 22
```

### Gradle Android App

```yaml
artifacts:
  - name: my-android-app
    project-type: gradle
    working-directory: .
    config:
      java-version: 21
      gradle-tasks: build assembleDemoRelease bundleDemoRelease
      gradle-version-file: gradle.properties
```

---

## Monorepo Configuration

Build multiple artifacts from a single repository.

### Separate Containers (One Artifact → One Container)

**`.github/artifacts.yml`**
```yaml
artifacts:
  - name: backend
    project-type: maven
    working-directory: java-backend
    build-type: application
    config:
      java-version: 21

  - name: frontend
    project-type: npm
    working-directory: frontend
    config:
      node-version: 22

containers:
  - name: backend
    from: [backend]
    container-file: java-backend/Containerfile
    context: java-backend

  - name: frontend
    from: [frontend]
    container-file: frontend/Containerfile
    context: frontend
```

### Combined Container (Multiple Artifacts → One Container)

**`.github/artifacts.yml`**
```yaml
artifacts:
  - name: backend
    project-type: maven
    working-directory: java-backend

  - name: frontend
    project-type: npm
    working-directory: frontend

containers:
  - name: full-stack-app
    from: [backend, frontend]          # Multiple artifacts in one container
    container-file: Containerfile
    context: .
```

**`Containerfile`**
```dockerfile
FROM registry.access.redhat.com/ubi9/openjdk-21-runtime:latest
COPY java-backend/target/*.jar app.jar
COPY frontend/dist/ /app/static/
CMD ["java", "-jar", "app.jar"]
```

### Workflow Configuration

**`.github/workflows/release-workflow.yml`**
```yaml
name: Release Workflow

on:
  push:
    tags:
      - "v[0-9]+.[0-9]+.[0-9]+"

permissions:
  contents: read

jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2
    permissions:
      contents: write
      packages: write
      id-token: write
      actions: read
      security-events: write
      attestations: write
    secrets: inherit
    with:
      artifacts-config: .github/artifacts.yml
      changelog-creator: git-cliff
      release-publisher: github-cli
```

### Monorepo Limitations

- **Unified versioning**: All artifacts share the same version (from git tag)
- **Single changelog**: One changelog for the entire repository
- **No change detection**: All artifacts build on every release (smart builds coming in future)
- **Sequential version bumps**: Artifacts bump versions one at a time (parallel coming in future)

## Complete Working Examples

For complete working examples, see the [`examples/`](../examples/) directory:

- **Maven Application**: [`examples/maven-app/`](../examples/maven-app/)
- **NPM Application**: [`examples/npm-app/`](../examples/npm-app/)
- **Gradle Application**: [`examples/gradle-app/`](../examples/gradle-app/)
- **Monorepo**: [`examples/monorepo/`](../examples/monorepo/)

---
