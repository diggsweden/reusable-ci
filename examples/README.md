<!--
SPDX-FileCopyrightText: 2025 The Reusable CI Authors

SPDX-License-Identifier: CC0-1.0
-->

# Configuration Examples

Complete working examples for different project types.

## Available Examples

### 1. [Maven Application](maven-app/)
**Use case:** Java/Spring Boot application with container

**Contains:**
- Maven application configuration
- Container build with multi-platform support
- GitHub Packages publishing

**Good for:**
- Spring Boot services
- Java microservices
- Backend APIs

---

### 2. [NPM Application](npm-app/)
**Use case:** Node.js/TypeScript application with container

**Contains:**
- NPM application configuration
- Container build with multi-platform support
- GitHub Packages publishing
- Optional npmjs.org publishing

**Good for:**
- Node.js services
- React/Vue/Angular apps
- Express APIs

---

### 3. [Gradle Application](gradle-app/)
**Use case:** Gradle/Android application

**Contains:**
- Gradle build configuration
- Android-specific tasks (APK, AAB)
- Version code auto-increment
- GitHub Packages publishing

**Good for:**
- Android applications
- Gradle-based Java projects
- Multi-module Gradle builds

---

### 4. [Monorepo](monorepo/)
**Use case:** Multiple artifacts in one repository

**Contains:**
- Multiple artifact configuration
- Mixed project types (Maven + NPM)
- Separate containers per artifact
- Shared library publishing to Maven Central

**Good for:**
- Microservices architecture
- Full-stack applications (backend + frontend)
- Projects with shared libraries

**Includes sub-examples:**
- `artifacts.yml` - Basic monorepo (separate containers)
- `multi-artifact-container.yml` - Combined container

---

## Quick Start

### Using an Example

1. **Navigate to example directory:**
   ```bash
   cd examples/maven-app/  # or npm-app, gradle-app, monorepo
   ```

2. **Copy files to your project:**
   ```bash
   # Copy artifacts configuration
   cp artifacts.yml /path/to/your/project/.github/

   # Copy workflow
   cp release-workflow.yml /path/to/your/project/.github/workflows/
   ```

3. **Customize configuration:**
   - Update artifact `name`
   - Adjust versions (java-version, node-version)
   - Verify paths (working-directory, container-file)

4. **Create release:**
   ```bash
   git tag -s v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

---

## Comparison Matrix

| Feature | Maven App | NPM App | Gradle App | Monorepo |
|---------|-----------|---------|------------|----------|
| **Project Types** | Maven | NPM | Gradle | Mixed |
| **Containers** | ✅ Single | ✅ Single | Optional | ✅ Multiple |
| **Publishing** | GitHub | GitHub | GitHub | GitHub + Maven Central |
| **Complexity** | Low | Low | Medium | High |
| **Best For** | Java APIs | Node services | Android apps | Microservices |

---

## Common Modifications

### Add Maven Central Publishing

In any Maven example:
```yaml
artifacts:
  - name: my-lib
    build-type: library  # Required
    require-authorization: true  # Recommended
    publish-to:
      - github-packages
      - maven-central  # Add this
```

**Requirements:**
- Sonatype account
- MAVENCENTRAL_USERNAME secret
- MAVENCENTRAL_PASSWORD secret

See [Publishing Guide](../docs/publishing.md#maven-central) for setup.

---

### Add npmjs.org Publishing

In NPM example:
```yaml
artifacts:
  - name: my-package
    publish-to:
      - github-packages
      - npmjs  # Add this
```

**Requirements:**
- npmjs.org account
- NPM_TOKEN secret
- Scoped package name: `@org/package`

See [Publishing Guide](../docs/publishing.md#npm-registry-npmjsorg) for setup.

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
  - platforms: linux/amd64,linux/arm64  # Multi-platform (slower)
  # or
  - platforms: linux/amd64  # Single platform (faster)
```

---

### Disable Security Features

```yaml
containers:
  - enable-slsa: false  # Disable SLSA provenance
  - enable-sbom: false  # Disable SBOM generation
  - enable-scan: false  # Disable Trivy scanning
```

**Note:** Not recommended for production.

---

## Testing Your Configuration

### 1. Validate YAML Syntax

```bash
# Install yamllint
pip install yamllint

# Check syntax
yamllint .github/artifacts.yml
```

### 2. Test with Dev Workflow

Create `.github/workflows/release-dev-workflow.yml`:
```yaml
on:
  push:
    branches: [feat/test-config]

jobs:
  dev-release:
    uses: diggsweden/reusable-ci/.github/workflows/release-dev-orchestrator.yml@v2-dev
    with:
      artifacts-config: .github/artifacts.yml
    permissions:
      contents: write
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
  artifacts-config: .github/artifacts.yml  # Must match actual path
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

**Solution:** Add `publish-to`:
```yaml
publish-to:
  - github-packages  # Explicit publishing target
```


---

## See Also

- [Artifacts Reference](../docs/artifacts-reference.md) - Complete field documentation
- [Publishing Guide](../docs/publishing.md) - Registry setup instructions
