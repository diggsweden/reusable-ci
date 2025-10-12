# Reusable CI/CD Workflows

Reusable CI/CD workflows and scripts.
Implements best Open Source workflows, compliance, security best practices, automated releases, and quality checks.

**Current version:** `@v2-dev`

## Documentation

**Getting Started:**
- [Workflow Guide](docs/workflows.md) - Workflow architecture and patterns
- [Artifacts Reference](docs/artifacts-reference.md) - `artifacts.yml` documentation with examples

**Customizing:**
- [Publishing Guide](docs/publishing.md) - Maven Central, NPM, and registry setup
- [Components Reference](docs/components.md) - Individual workflow components

**Advanced:**
- [Reference Guide](docs/reference.md) - Permissions, secrets, and validation matrices
- [Artifact Verification](docs/verification.md) - Security and verification
- [Scripts Reference](docs/scripts.md) - Validation scripts

---

## Introduction

There are two main workflow chains:

1. **Pull Request Chain** - Runs on every PR and push
   - Linting and code quality checks
   - Security scanning
   - License compliance
   - Build verification
   - Optional testing (project-specific)

2. **Release Chain** - Runs when you create and push version tag
   - Version validation (including tag requirements)
   - Artifact building and publishing
   - Container image creation
   - Security features (SBOM, signing, attestation)
   - Changelog generation
   - GitHub release creation
   - Dependency caching between jobs
   - Enhanced build summaries

The workflows handle multi-platform container builds, security scanning and attestation, artifact signing and checksums, version management, and changelog generation.

Most components are configurable.

---

### Getting Started

Most projects require two or three files:

1. `.github/workflows/pullrequest-workflow.yml` - For PR checks
2. `.github/workflows/release-workflow.yml` - For production releases
3. `.github/workflows/release-workflow-dev.yml` - (Optional) For dev/feature branch releases


### How It Works

1. Push code → PR workflow runs checks
2. Create version tag → Release workflow builds and publishes
3. Workflow failures → Detailed error messages

### Customization Levels

#### Example 1: Just Use Flows As Is 
```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2-dev
with:
  artifacts-config: .github/artifacts.yml
  release-publisher: github-cli
```

#### Example 2: Build Your Own Flow From The Components
```yaml
jobs:
  build-maven:
    uses: diggsweden/reusable-ci/.github/workflows/build-maven.yml@v2-dev
    with:
      build-type: app
      java-version: "21"

  publish-github:
    needs: build-maven
    uses: diggsweden/reusable-ci/.github/workflows/publish-github.yml@v2-dev
    with:
      package-type: maven
      artifact-source: maven-build-artifacts

  build-container:
    needs: build-maven
    uses: diggsweden/reusable-ci/.github/workflows/publish-container.yml@v2-dev
    with:
      container-file: Containerfile
      artifact-source: maven-build-artifacts
```

## Quick Start

### For New Projects

1. **Create artifacts configuration** - Define what to build:
   ```yaml
   # .github/artifacts.yml
   artifacts:
     - name: my-app
       project-type: maven  # or npm, gradle
       working-directory: .
       config:
         java-version: 21  # or node-version for npm
   ```

2. **Create pull request workflow** - Run checks on PRs:
   ```yaml
   # .github/workflows/pullrequest-workflow.yml
   name: Pull Request Checks
   on:
     pull_request:
       branches: [main, master, develop]
   permissions:
     contents: read
   jobs:
     pr-checks:
       uses: diggsweden/reusable-ci/.github/workflows/pullrequest-orchestrator.yml@v2-dev
       permissions:
         contents: read
         packages: read
         security-events: write
       secrets: inherit
       with:
         project-type: maven  # or npm, gradle
   ```

3. **Create release workflow** - Trigger builds on tags:
   ```yaml
   # .github/workflows/release-workflow.yml
   name: Release
   on:
     push:
       tags: ["v[0-9]+.[0-9]+.[0-9]+"]
   permissions:
     contents: read
   jobs:
     release:
       uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2-dev
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
   ```

4. **(Optional) Create dev release workflow** - Fast dev builds:
   ```yaml
   # .github/workflows/release-dev-workflow.yml
   name: Dev Release
   on:
     push:
       branches: ['dev/**', 'feat/**']
   permissions:
     contents: read
   jobs:
     dev-release:
       uses: diggsweden/reusable-ci/.github/workflows/release-dev-orchestrator.yml@v2-dev
       permissions:
         contents: write
         packages: write
       secrets: inherit
       with:
         project-type: maven  # or npm, gradle
   ```

5. **Create your first release**:
   ```bash
   git tag -s v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```



---

## Conceptual View

### Pull Request Flow Diagram

```text
┌─────────────────────────────────────────────────────────────────────┐
│                    Pull Request Created/Updated                     │
└────────────────────────────────┬────────────────────────────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │   Commit Lint Check     │
                    │   (conventional commits)│
                    └────────────┬────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │   License Lint Check    │
                    │   (SPDX headers)        │
                    └────────────┬────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │  Dependency Review      │
                    │  (vulnerability scan)   │
                    └────────────┬────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │      MegaLint           │
                    │  (50+ code linters)     │
                    └────────────┬────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │    Build & Verify       │
                    │  (Maven/NPM/Gradle)     │
                    └────────────┬────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │   PR Checks Complete    │
                    │   ✓ Ready to merge      │
                    └─────────────────────────┘
```

### Release Flow Diagram

```text
┌─────────────────────────────────────────────────────────────────────┐
│                        Tag Push (v1.0.0)                            │
└────────────────────────────────┬────────────────────────────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │  Parse artifacts.yml    │
                    │  Validate configuration │
                    └────────────┬────────────┘
                                 │
           ┌─────────────────────┼─────────────────────┐
           │                     │                     │
   ┌───────▼────────┐   ┌───────▼────────┐   ┌───────▼────────┐
   │  Build Maven   │   │   Build NPM    │   │  Build Gradle  │
   │  Artifact 1    │   │   Artifact 2   │   │   Artifact 3   │
   └───────┬────────┘   └───────┬────────┘   └───────┬────────┘
           │                     │                     │
           └─────────────────────┼─────────────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │  Publish to Registries  │
                    │  - GitHub Packages      │
                    │  - Maven Central        │
                    │  - npmjs.org            │
                    └────────────┬────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │   Build Containers      │
                    │   (from artifacts)      │
                    │   - Multi-platform      │
                    │   - SLSA + SBOM + Scan  │
                    └────────────┬────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │  Create GitHub Release  │
                    │  - Changelog            │
                    │  - Checksums            │
                    │  - Signatures           │
                    └─────────────────────────┘
```

### Core Concepts

**Build Stage** - Language-specific builders create artifacts:
- `build-maven` - Builds Maven projects (apps or libs)
- `build-npm` - Builds NPM projects
- `build-gradle` - Builds Gradle projects

**Publish Stage** - Target-specific workflows publish artifacts:
- `publish-github` - Publishes Maven/NPM/Gradle → GitHub Packages
- `publish-mavencentral` - Publishes Maven libs → Maven Central

**Container Stage** - Separate containers section references artifacts:
- Containers defined in `containers[]` section
- Reference artifacts by name via `from: [artifact-name]`
- Built after all artifact builds complete
- Support multi-artifact containers (combine multiple artifacts into one image)


**Development builds (NOT from tags):**
- Branch pushes create branch-aware tags: `0.5.9-dev-feat-feature-abc1234`
- Tags like `v1.0.0-dev` are explicitly excluded from releases

---
