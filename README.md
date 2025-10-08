# Reusable CI/CD Workflows

Reusable CI/CD workflows and scripts.
Implements best Open Source workflows, compliance, security best practices, automated releases, and quality checks.

**Current version:** `@v2-dev`

## Documentation

- **[Configuration Reference](docs/configuration.md)** - Complete `artifacts.yml` field documentation
- **[Publishing Guide](docs/publishing.md)** - Maven Central, NPM, and registry setup
- **[Troubleshooting](docs/troubleshooting.md)** - Common errors and solutions
- [Workflow Guide](docs/workflows.md) - Workflow configuration details
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

All components are configurable:
- **Use complete chains** - Full functionality with minimal configuration
- **Disable specific features** - Skip linters, disable signing, remove SBOM generation
- **Use individual components** - Build custom workflows using specific components
- **Combine approaches** - Use specific builders with custom release processes
- **Custom implementation** - Requires: security scanning, license compliance, SBOM generation, artifact signing, SLSA attestation

---

### Getting Started

Most projects require two or three files:

1. `.github/workflows/pullrequest-workflow.yml` - For PR checks
2. `.github/workflows/release-workflow.yml` - For production releases
3. `.github/workflows/release-workflow-dev.yml` - (Optional) For dev/feature branch releases

### Prerequisites

Some features require GitHub secrets:
- **GPG signing** needs GPG keys
- **Maven Central** needs Sonatype credentials  
- **Container registries** use GITHUB_TOKEN (automatic)

All required GitHub secrets are configured at the DiggSweden organization level. Request access from DiggSweden GitHub administrators to enable secrets for the repository. You can of course also set up your own secrets.

### How It Works

1. Push code → PR workflow runs checks
2. Create version tag → Release workflow builds and publishes
3. Workflow failures → Detailed error messages

### Customization Levels

#### Option 1: Use Everything (v2-dev - requires artifacts.yml)
```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2-dev
with:
  artifacts-config: .github/artifacts.yml
  release-publisher: github-cli
```

#### Option 2: Build Your Own Flow
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

#### Option 3: Complete Custom Implementation
```yaml
jobs:
  custom-everything:
    # Your own implementation
    # Required: security scanning, license compliance, SBOM generation, 
    # artifact signing, SLSA attestation
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

2. **Create release workflow** - Trigger builds on tags:
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

3. **Create your first release**:
   ```bash
   git tag -s v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

**See [Complete Examples](#single-artifact-example) below for Maven, NPM, and container configurations.**

---

## Architecture (v2)

**Decoupled build/publish architecture** with separate artifact and container handling for monorepo support.

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

**Benefits:**
- **DRY** - Build logic written once per language
- **Flexible** - Easy to add new publish targets (GitLab, Artifactory, etc.)
- **Composable** - Can publish same build to multiple registries
- **Testable** - Can build without publishing
- **Monorepo-friendly** - Multiple artifacts + containers in one repo
- **Multi-artifact containers** - Combine multiple builds into one container

### Single Artifact Example

**Note:** v2-dev requires an `artifacts.yml` configuration file. Inline configuration is no longer supported.

**`.github/artifacts.yml`** (Maven app only):
```yaml
artifacts:
  - name: my-app
    project-type: maven
    working-directory: .
    build-type: application
    config:
      java-version: 21
```

**`.github/artifacts.yml`** (Maven app + Container):
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

**`.github/workflows/release-workflow.yml`**:
```yaml
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2-dev
    with:
      artifacts-config: .github/artifacts.yml
      release-publisher: github-cli
```

## Monorepo Support (v2-dev)

Build multiple artifacts from a single repository with separate container handling.

### Basic Monorepo Example

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
    platforms: linux/amd64,linux/arm64

  - name: frontend
    from: [frontend]
    container-file: frontend/Containerfile
    context: frontend
    platforms: linux/amd64,linux/arm64
```

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
      # Point to artifacts config file
      artifacts-config: .github/artifacts.yml

      # Shared configuration
      changelog-creator: git-cliff
      release-publisher: github-cli
```

### Real-World Example: Wallet Issuer POC

Repository structure:
```text
wallet-issuer-poc/
├── java-backend/          # Spring Boot backend
│   ├── src/
│   └── pom.xml
├── frontend/              # React frontend
│   ├── src/
│   └── package.json
├── Containerfile          # Combines both
└── .github/
    ├── artifacts.yml
    └── workflows/
        └── release-workflow.yml
```

**`.github/artifacts.yml`**
```yaml
artifacts:
  - name: issuer-backend
    project-type: maven
    working-directory: java-backend
    build-type: application
    config:
      java-version: 21

  - name: issuer-frontend
    project-type: npm
    working-directory: frontend
    config:
      node-version: 22

containers:
  - name: issuer-backend
    from: [issuer-backend]
    container-file: Containerfile
    context: .
    platforms: linux/amd64,linux/arm64
```

**`.github/workflows/release-workflow.yml`**
```yaml
name: Release Workflow

on:
  push:
    tags:
      - "v[0-9]+.[0-9]+.[0-9]+"
      - "v[0-9]+.[0-9]+.[0-9]+-alpha*"
      - "v[0-9]+.[0-9]+.[0-9]+-beta*"
      - "v[0-9]+.[0-9]+.[0-9]+-rc*"

concurrency:
  group: release-${{ github.ref }}
  cancel-in-progress: false

permissions:
  contents: read

jobs:
  release:
    permissions:
      contents: write
      packages: write
      id-token: write
      actions: read
      security-events: write
      attestations: write
    secrets: inherit
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2-dev
    with:
      artifacts-config: .github/artifacts.yml
      changelog-creator: git-cliff
      release-publisher: github-cli
```

### Artifact Configuration Reference

> **Naming Convention:** All fields in `artifacts.yml` now use **`kebab-case`** consistently:
> - **Top-level fields:** `project-type`, `working-directory`
> - **Config fields:** `java-version`, `node-version`, `settings-path`
> - **Container fields:** `container-file`, `enable-slsa`, `enable-sbom`, `enable-scan`
> 
> This matches the workflow input parameter naming convention for consistency.

Each artifact in `artifacts.yml` supports these fields:

**Artifacts section:**

| Field | Required | Description | Example |
|-------|----------|-------------|---------|
| `name` | Yes | Artifact identifier | `backend-api` |
| `project-type` | Yes | Build system type | `maven`, `npm`, `gradle` |
| `working-directory` | Yes | Path to artifact source | `services/backend` |
| `build-type` | No | Build type (Maven/Gradle only) | `application` (default), `library` |
| `require-authorization` | No | Require user authorization | `false` (default), `true` |
| `publish-to` | No | Publishing targets | `[github-packages]` (default) |
| `config.java-version` | No | Java version (Maven/Gradle) | `21` (default) |
| `config.node-version` | No | Node version (NPM) | `22` (default) |
| `config.npm-tag` | No | NPM dist tag | `latest` |
| `config.settings-path` | No | Maven settings path | `.mvn/settings.xml` |

**Containers section:**

| Field | Required | Description | Example |
|-------|----------|-------------|---------|
| `name` | Yes | Container identifier | `backend-api` |
| `from` | Yes | Array of artifact names | `[backend-api]` |
| `container-file` | Yes | Path to Containerfile | `Containerfile` |
| `context` | No | Docker build context | `.` (default) |
| `platforms` | No | Target platforms | `linux/amd64,linux/arm64` |
| `enable-slsa` | No | Enable SLSA provenance | `true` (default) |
| `enable-sbom` | No | Generate SBOM | `true` (default) |
| `enable-scan` | No | Trivy vulnerability scan | `true` (default) |

### Publishing Targets

**Available Publishing Targets** (for `publish-to` field):

| Target | Description | Requirements |
|--------|-------------|--------------|
| `github-packages` | GitHub Packages registry | GITHUB_TOKEN (automatic) |
| `maven-central` | Maven Central (Sonatype OSSRH) | MAVENCENTRAL_USERNAME, MAVENCENTRAL_PASSWORD, build-type: library |
| `npm-registry` | Public npmjs.org registry | NPM_TOKEN |

> **Default Behavior:** If `publish-to` is omitted, artifacts are published to `github-packages` only.

**Container Configuration** (in separate `containers[]` section):
- Containers reference artifacts via `from: [artifact-name]`
- Built automatically after artifact builds complete
- Support multi-artifact containers (e.g., `from: [api, worker, web]`)

### How Monorepo Builds Work (v2-dev)

1. **Parse Config**: Reads `artifacts[]` and `containers[]` from `artifacts.yml`
2. **Validate**: Checks container dependencies reference valid artifact names
3. **Version Bump**: Each artifact's version file updated (pom.xml, package.json)
4. **Build Stage**: Each artifact built in parallel (build-maven, build-npm, build-gradle)
5. **Publish Stage**: Built artifacts published to registries (GitHub Packages, Maven Central)
6. **Container Stage**: Containers built from artifacts (references via `from:` field)
7. **Release**: Single GitHub release created with changelog and all artifacts

### Limitations

- **Unified versioning**: All artifacts share the same version (from git tag)
- **Single changelog**: One changelog for the entire repository
- **No change detection**: All artifacts build on every release (smart builds coming in future)
- **Sequential version bumps**: Artifacts bump versions one at a time (parallel coming in future)

### Example Configurations

See the `examples/` directory for complete working examples:
- `examples/monorepo-artifacts.yml` - Multiple artifacts with containers
- `examples/multi-artifact-container.yml` - Combining multiple builds into one container

---

## Pull Request Workflow

Quality checks executed on pull requests and pushes.

### Workflow Steps
1. **Linting** - Code style and quality validation
2. **License Scanning** - License compliance verification
3. **Security Scanning** - SAST and dependency analysis
4. **Build** - Project compilation
5. **Tests** - Unit test execution (when test job is chained)

### Full Configuration Example (Maven Application)
```yaml
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
#
# SPDX-License-Identifier: CC0-1.0
---

name: Pull Request Workflow

on:
  pull_request:
    branches:
      - main
      - master
      - develop
      - 'release/**'  # Matches release/1.0, release/2.0, etc.
      - 'feature/**'  # Matches feature/new-api, feature/fix-bug, etc.

permissions:
  contents: read  # Best Security practice. Jobs only get read as base, and then permissions are added as needed

jobs:
  pr-checks:
    uses: diggsweden/reusable-ci/.github/workflows/pullrequest-orchestrator.yml@v2-dev

    # Pass organization-level secrets to the workflow
    # Required for access to private GitHub Packages
    # Without this, workflow cannot fetch private @diggsweden/* packages
    secrets: inherit

    permissions:
      contents: read         # Required: Clone and read repository source code
      packages: read         # Required: Download private dependencies from GitHub Packages
      security-events: write # Required: Upload vulnerability scan results to GitHub Security tab

    with:
      project-type: maven  # Determines build commands and dependency management (maven/npm)

  test:
    needs: [pr-checks]

    # Always run tests regardless of linting results
    # Shows both linting issues and test failures
    # Without this, test failures hidden if linting fails first
    if: always()

    permissions:
      contents: read  # Required: Access repository source code for test execution
      packages: read  # Required: Fetch test dependencies from GitHub Packages

    # Uses local test workflow (must exist in repository)
    # Separation allows custom test configurations per repository
    uses: ./.github/workflows/test.yml
```

### Full Configuration Example (All Options)
```yaml
name: Pull Request Workflow

on:
  pull_request:
    branches:
      - main
      - master
      - develop
      - 'release/**'  # Matches release/1.0, release/2.0, etc.
      - 'feature/**'  # Matches feature/new-api, feature/fix-bug, etc.

permissions:
  contents: read  # Best Security practice. Jobs only get read as base, and then permissions are added as needed

jobs:
  pr-checks:
    uses: diggsweden/reusable-ci/.github/workflows/pullrequest-orchestrator.yml@v2-dev

    # Pass organization-level secrets to the workflow
    # Required for accessing private @diggsweden/* packages in GitHub Packages
    # Without this, builds fail if you depend on internal private libraries
    secrets: inherit

    permissions:
      contents: read          # Required: Clone repository and read source code
      packages: read          # Required: Access private packages from GitHub Packages registry
      security-events: write  # Required: Upload security findings to GitHub's Security tab

    with:
      # REQUIRED PARAMETERS
      project-type: maven              # Required. Valid: maven, npm

      # OPTIONAL PARAMETERS (shown with defaults)
      base-branch: main               # Default: main. Base branch for commit linting

      # LINTER CONTROLS (all default to true except publiccodelint)
      linters.commitlint: true       # Default: true. Validates commit messages follow conventions
      linters.licenselint: true      # Default: true. Validates SPDX license headers
      linters.dependencyreview: true # Default: true. Checks for vulnerable dependencies
      linters.megalint: true         # Default: true. Runs 50+ code quality linters
      linters.publiccodelint: false  # Default: false. Validates publiccode.yml (for open source)

  test:
    needs: [pr-checks]

    # Always run tests regardless of linting results
    # CI feedback in one run
    if: always()

    permissions:
      contents: read  # Required: Read source code to run tests
      packages: read  # Required: Download test dependencies from GitHub Packages

    # Your custom test workflow - keeps test logic separate and maintainable
    uses: ./.github/workflows/test.yml
```

### Supported Project Types
- `maven` - Java projects with pom.xml
- `npm` - Node.js projects with package.json

---

## Release Workflow

Complete release process triggered by version tags.

### Release Steps
1. **Version Validation** - Tag and project version matching
2. **Build Artifacts** - JAR and NPM package creation
3. **Container Images** - Docker image build and registry push
4. **Security** - SBOM generation, artifact signing, SLSA attestation
5. **Changelog** - Release notes via git-cliff
6. **GitHub Release** - Release creation with assets
7. **Publishing** - Deployment to Maven Central, NPM, GitHub Packages

### Basic Release Workflow (v2-dev)
```yaml
# .github/workflows/release-workflow.yml
name: Release Workflow

on:
  push:
    tags:
      - "v[0-9]+.[0-9]+.[0-9]+"              # Stable: v1.0.0
#      - "v[0-9]+.[0-9]+.[0-9]+-alpha*"       # Alpha: v1.0.0-alpha.1
#      - "v[0-9]+.[0-9]+.[0-9]+-beta*"        # Beta: v1.0.0-beta.1
#      - "v[0-9]+.[0-9]+.[0-9]+-rc*"          # RC: v1.0.0-rc.1
      - "v[0-9]+.[0-9]+.[0-9]+-snapshot*"    # Snapshot: v1.0.0-snapshot
      - "v[0-9]+.[0-9]+.[0-9]+-SNAPSHOT*"    # Snapshot: v1.0.0-SNAPSHOT

concurrency:
  group: release-${{ github.ref }}
  cancel-in-progress: false  # Queue releases, don't cancel partial releases

permissions:
  contents: read  # Best Security practice. Jobs only get read as base, and then permissions are added as needed

jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2-dev
    secrets: inherit  # Use org-level GPG keys and publishing credentials
    permissions:
      contents: write         # Create GitHub releases and tags
      packages: write         # Publish artifacts and containers to GitHub
      id-token: write        # Generate OIDC token for attestations
      actions: read          # Read workflow for SLSA provenance
      security-events: write # Upload container scan results
      attestations: write    # Attach SBOM to container images
    with:
      artifacts-config: .github/artifacts.yml  # Path to artifacts configuration
      release-publisher: github-cli  # Creates GitHub release with changelog
```

### Configuration Options (v2-dev)

All configuration is now defined in `artifacts.yml`. See [Artifact Configuration Reference](#artifact-configuration-reference) for complete field documentation.

**Shared workflow inputs:**
```yaml
with:
  artifacts-config: .github/artifacts.yml      # Required: Path to artifacts config
  release-publisher: github-cli                # Optional: Create GitHub release
  changelog-creator: git-cliff                 # Optional: Generate changelog
  changelog.config: path/to/config.toml       # Optional: Custom changelog config
  changelog.skipversionbump: false            # Optional: Skip version bump
  release.generatesbom: true                  # Optional: Generate SBOM for release
  release.signartifacts: true                 # Optional: GPG sign artifacts
  release.checkauthorization: false           # Optional: Check user authorization
  release.draft: false                        # Optional: Create draft release
  branch: main                                # Optional: Base branch for changelog
```

### Release Types

#### Production Release
Triggered by Semver version tags without suffix:
- `v1.0.0` → Production release

#### Pre-release Types
Triggered by version tags with specific suffixes:
- `v1.0.0-alpha.1` → Alpha pre-release
- `v1.0.0-beta.1` → Beta pre-release  
- `v1.0.0-rc.1` → Release candidate
- `v1.0.0-SNAPSHOT` → Snapshot release (Maven style)

#### Excluded Tags
The following tags will NOT trigger releases:
- `v1.0.0-dev` → Development builds (use branch triggers instead)

---

## Development Container Workflow

Container builds for development environments.

### What It Does
- **Version bump** - Adds `-dev` suffix (e.g., `1.2.4-dev.1`)
- **Builds project** - Maven/NPM build with dependency caching
- **Creates container** - Multi-platform (linux/amd64, linux/arm64)
- **Pushes to registry** - With branch-aware tags (e.g., `0.5.9-dev-feat-awesome-cdb5e47`)
- **Publishes packages** - Maven/NPM artifacts with dev version
- **Generates summary** - Shows all created artifacts
- **Fast execution** - Completes in 1 minutes with multi-arch
- ❌ **Skips production features** - No SLSA,CHANGELOG, SBOM, signing, or GitHub release

### Configuration
```yaml
# .github/workflows/release-workflow-dev.yml
name: Dev Release

on:
  push:
    branches:
      - 'dev/**'   # Matches dev/feature-x, dev/fix-y
      - 'feat/**'  # Matches feat/new-api, feat/cool-stuff

permissions:
  contents: read

jobs:
  dev-release:
    uses: diggsweden/reusable-ci/.github/workflows/release-dev-orchestrator.yml@v2-dev
    permissions:
      contents: write   # Version bump commits
      packages: write   # Push dev containers to ghcr.io
    with:
      project-type: maven  # or npm
    secrets: inherit
```

### Triggering Dev Releases

Use The GitHub Workflow UI

OR

Push to a dev or feat branch
```bash
git push origin feat/my-feature
```

### Output
Creates containers with branch-aware dev tags:
- `ghcr.io/diggsweden/your-repo:0.5.9-dev-feat-awesome-abc1234`

Pull the image:
```bash
podman pull ghcr.io/diggsweden/your-repo:0.5.9-dev-feat-awesome-abc1234
```
---

### Tag Requirements

Tags must be:
1. Semantic versioned - Format: `vMAJOR.MINOR.PATCH[-PRERELEASE]`
2. Annotated - Created with `git tag -a` (not lightweight tags)
3. Signed - Created with `git tag -s` (GPG) or SSH signed

Examples:
```bash
git tag -s v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

Accepted tag patterns:
- `v1.0.0` - Stable release
- `v1.0.0-alpha.1` - Alpha pre-release
- `v1.0.0-beta.1` - Beta pre-release  
- `v1.0.0-rc.1` - Release candidate

For Maven based releases:
- `v1.0.0-snapshot` - Snapshot build
- `v1.0.0-SNAPSHOT` - Snapshot build (uppercase)

Tags ending with `-dev` are excluded from release workflows.

---

## Permissions in Reusable Workflows

You must explicitly declare permissions in your workflow file when calling reusable workflows. The orchestrator cannot automatically grant permissions to its nested workflow calls.

GitHub doesn't support dynamic permission inheritance across nested reusable workflow calls. Since orchestrators call multiple sub-workflows (version-bump, publish, container-build, release), each requiring different permissions, there's no way to make this automatic.

Copy the exact permissions shown in the examples. These are the minimum required.

### Required Permissions by Workflow Type

#### Pull Request Workflows
```yaml
permissions:
  contents: read         # Clone and read repository code
  packages: read         # Download packages from GitHub Packages
  security-events: write # Upload security scan results to GitHub
```

#### Release Workflows
```yaml
permissions:
  contents: write         # Create GitHub releases and tags
  packages: write         # Publish artifacts and containers to GitHub
  id-token: write        # Generate OIDC token for attestations
  actions: read          # Read workflow for SLSA provenance
  security-events: write # Upload container scan results
  attestations: write    # Attach SBOM to container images
```

#### Dev Release Workflows
```yaml
permissions:
  contents: write   # Version bump commits
  packages: write   # Push dev containers to ghcr.io
```

---

## Available Components

You can use individual components instead of the full orchestrators.

### Component Overview Matrix

#### Artifact Publishers

| Component | Purpose | Output | Required Secrets | Use When |
|-----------|---------|--------|------------------|----------|
| **publish-github** | Publishes Maven/NPM/Gradle to GitHub Packages | Artifacts in GitHub Packages | GITHUB_TOKEN | Default publishing target |
| **publish-mavencentral** | Publishes Maven libraries to Maven Central | Public Maven artifacts | MAVENCENTRAL_USERNAME, MAVENCENTRAL_PASSWORD | Public libraries (requires build-type: library) |

#### Container Builders

| Component | Purpose | Features | Build Time | Use When |
|-----------|---------|----------|------------|----------|
| **publish-container** | Production multi-platform container builds | SLSA attestation, SBOM, vulnerability scanning, multi-arch | ~10-15 min | Production releases |
| **publish-container-dev** | Fast single-platform dev builds | Basic image only, SHA-based tags | ~2-3 min | Development/testing |

#### Release Tools

| Component | Purpose | Creates/Updates | Required Secrets | Use When |
|-----------|---------|----------------|------------------|----------|
| **release-github** | GitHub release creation | GitHub release, changelog, signatures | RELEASE_TOKEN, GPG keys | Any production release |
| **version-bump** | Version management | Updated version files | GITHUB_TOKEN, OSPO_BOT_GHTOKEN | Before releases |
| **generate-changelog** | Changelog generation | Formatted changelog | GITHUB_TOKEN | Before releases |

#### Validators

| Component | Purpose | Validates | Blocks On | Use When |
|-----------|---------|-----------|-----------|----------|
| **release-prerequisites** | Pre-release checks | Version match, permissions, secrets | Any validation failure | Before any release |

> **Note:** To request a new component or publisher, open an issue in the reusable-ci repository.

### Build Workflows

#### `build-maven.yml`
Builds Maven projects (apps or libraries).
```yaml
uses: ./.github/workflows/build-maven.yml
with:
  build-type: app           # "app" or "lib"
  java-version: "21"        # JDK version
  working-directory: "."    # Path to pom.xml
```

#### `build-npm.yml`
Builds NPM projects.
```yaml
uses: ./.github/workflows/build-npm.yml
with:
  node-version: "22"        # Node.js version
  working-directory: "."    # Path to package.json
```

#### `build-gradle.yml`
Builds Gradle projects.
```yaml
uses: ./.github/workflows/build-gradle.yml
with:
  java-version: "21"        # JDK version
  working-directory: "."    # Path to build.gradle
  gradle-tasks: "build"     # Gradle tasks to run
```

### Publish Workflows

#### `publish-github.yml`
Publishes artifacts to GitHub Packages (Maven/NPM/Gradle).
```yaml
uses: ./.github/workflows/publish-github.yml
with:
  package-type: maven          # maven, npm, or gradle
  artifact-source: maven-build-artifacts  # Name of workflow artifact
  working-directory: "."
```

#### `publish-mavencentral.yml`
Publishes Maven libraries to Maven Central.
```yaml
uses: ./.github/workflows/publish-mavencentral.yml
with:
  artifact-source: maven-build-artifacts  # Name of workflow artifact
  working-directory: "."
  settings-path: ".mvn/settings.xml"
```

### Container Workflows

#### `publish-container.yml`
Production container builds with full security features. Supports multiple registries.
```yaml
uses: ./.github/workflows/publish-container.yml
with:
  container-file: "Containerfile"
  context: "."
  platforms: "linux/amd64,linux/arm64"
  enable-slsa: true
  enable-sbom: true
  enable-scan: true
  registry: "ghcr.io"
```

#### `publish-container-dev.yml`
Fast development container builds. Supports multiple registries.
```yaml
uses: ./.github/workflows/publish-container-dev.yml
with:
  container-file: "Dockerfile"
  registry: "ghcr.io"
  project-type: maven
  working-directory: "."
```

### Other Components

#### `version-bump.yml`
Handles version bumping and updates version files.
```yaml
uses: ./.github/workflows/version-bump.yml
with:
  project-type: maven      # Determines version file (pom.xml vs package.json)
  branch: main             # Base branch for comparison
  working-directory: "."   # Path to project root
```

#### `generate-changelog.yml`
Generates changelog from git commits.
```yaml
uses: ./.github/workflows/generate-changelog.yml
with:
  branch: main             # Base branch for changelog comparison
  config-file: ""          # Optional: Custom changelog config
```

#### `release-github.yml`
Creates GitHub releases with assets.
```yaml
uses: ./.github/workflows/release-github.yml
with:
  attach-artifacts: "target/*.jar"  # Files to upload as release assets
  generate-sbom: true               # Include CycloneDX/SPDX SBOM files
  sign-artifacts: true              # GPG sign all release artifacts
```

#### `release-prerequisites.yml`
Validates release requirements (called automatically by orchestrator).
```yaml
uses: ./.github/workflows/release-prerequisites.yml
with:
  project-type: maven
  build-type: application
  check-authorization: true  # Verify user has permission to release
```

---

## Workflow Reference

### Workflow Files

| Workflow | Purpose | When to Use |
|----------|---------|-------------|
| `pullrequest-orchestrator.yml` | Run CI checks on PRs | Every repository |
| `release-orchestrator.yml` | Full release process | Production releases |
| `release-dev-orchestrator.yml` | Dev container builds | Development branches |

### Required Secrets and Environment Variables

## Environment Variables Matrix

| Variable/Secret | Required For | When Checked | Expected Value | Notes |
|-----------------|--------------|--------------|----------------|--------|
| **GITHUB_TOKEN** | All workflows | Always | Valid GitHub token | Provided by GitHub Actions |
| **OSPO_BOT_GHTOKEN** | Release workflows | During release | GitHub PAT with repo scope | Bot token for releases |
| **OSPO_BOT_GPG_PUB** | GPG signing | During signing | GPG public key | Public key for verification |
| **OSPO_BOT_GPG_PRIV** | GPG signing | During signing | Base64 GPG private key | Private key for signing |
| **OSPO_BOT_GPG_PASS** | GPG signing | During signing | GPG key passphrase | Passphrase for GPG key |
| **MAVENCENTRAL_USERNAME** | Maven Central publishing | During publish | Sonatype username | Maven Central auth |
| **MAVENCENTRAL_PASSWORD** | Maven Central publishing | During publish | Sonatype password | Maven Central auth |
| **NPM_TOKEN** | NPM publishing to npmjs.org | During publish | npmjs.org auth token | NPM public registry auth (not GitHub Packages) |
| **RELEASE_TOKEN** | GitHub CLI | During release | GitHub PAT | GitHub release operations |
| **AUTHORIZED_RELEASE_DEVELOPERS** | Production releases | Pre-release check | Comma-separated usernames | Who can release |

## Prerequisites Check Matrix

| Check | When Performed | What It Validates | Fails If | How to Fix |
|-------|----------------|-------------------|----------|------------|
| **Version Match** | Release workflow | Tag matches project version | `v1.0.0` tag but pom.xml has `1.0.1` | Ensure tag matches version exactly |
| **GPG Key** | When `signatures: true` | GPG key is valid and accessible | Key expired or malformed | Generate new GPG key, export as base64 |
| **Maven Central Creds** | Maven Central publishing | Can authenticate to Sonatype | Invalid username/password | Verify Sonatype account credentials |
| **NPM Registry** | NPM publishing to npmjs.org | Can authenticate to registry | Token expired or invalid scope | Generate new NPM token with publish scope |
| **Container Registry** | Container in `containers[]` | Can push to registry | No write permission | Ensure `packages: write` permission |
| **GitHub Release** | Release creation | Can create releases | No `contents: write` | Add permission to workflow |
| **Protected Branch** | On push to main | User has bypass rights | Actor lacks permission | Add user to bypass list |
| **Artifact Existence** | During upload | Build artifacts exist | `target/*.jar` not found | Ensure build succeeds first |
| **Container/Containerfile** | Container build | Containerfile exists | No Containerfile at specified path | Create Containerfile or specify correct path |
| **License Compliance** | PR checks | Dependencies have compatible licenses | GPL in proprietary project | Review and replace dependencies |

## Permission Requirements Matrix

| Workflow | Permission | Why Needed | If Missing |
|----------|------------|------------|------------|
| **PR Workflow** | `contents: read` | Read code | Cannot checkout |
| | `packages: read` | Read private packages | Cannot fetch dependencies |
| | `security-events: write` | Upload scan results | Security tab won't show results |
| **Release Workflow** | `contents: write` | Create tags/releases | Cannot create release |
| | `packages: write` | Push packages | Cannot publish artifacts |
| | `id-token: write` | OIDC for SLSA | No attestation |
| | `attestations: write` | Attach SBOMs | No SBOM attachment |
| | `actions: read` | Read workflow | SLSA generation fails |
| | `issues: write` | Update issues | Cannot add labels/comments |
| **Dev Workflow** | `contents: read` | Read code | Cannot checkout |
| | `packages: write` | Push images | Cannot push to ghcr.io |

## Getting Access to Secrets

### How Secrets Work

**All secrets are managed centrally at the DiggSweden organization level.** As a developer in a DiggSweden project, you:

1. **Don't need to create secrets** - They already exist at DiggSweden org level
2. **Request access** - Contact your DiggSweden GitHub org owner/admin
3. **Specify which ones** - Tell them which secrets your repo needs:
   - GPG signing → Request `OSPO_BOT_GPG_PRIV`, `OSPO_BOT_GPG_PASS`, and `OSPO_BOT_GPG_PUB`
   - Bot token → Request `OSPO_BOT_GHTOKEN` and `RELEASE_TOKEN`
   - Maven Central → Request `MAVENCENTRAL_USERNAME` and `MAVENCENTRAL_PASSWORD`
   - NPM public registry → Request `NPM_TOKEN` (only if publishing to npmjs.org)
4. **Get enabled** - DiggSweden admin grants your repository access to the secrets

- **No manual configuration** - Developers never touch secret values

---

## Version Tag Format

**Allowed tags for releases:**
- Production: `v1.0.0`, `v2.3.4`
- Alpha: `v1.0.0-alpha`, `v1.0.0-alpha.1`
- Beta: `v1.0.0-beta`, `v1.0.0-beta.1`
- Release Candidate: `v1.0.0-rc`, `v1.0.0-rc.1`
- Snapshot: `v1.0.0-snapshot`, `v1.0.0-SNAPSHOT`

**Development builds (NOT from tags):**
- Branch pushes create SHA-based tags: `abc1234-dev`
- Tags like `v1.0.0-dev` are explicitly excluded from releases

---

## Local Testing

The release workflow includes several validation scripts that you can run locally before creating a tag:

See `scripts/README.md` for detailed documentation on validation scripts.

---
