# Reusable CI/CD Workflows

Reusable CI/CD workflows and scripts for DiggSweden projects. Implements security best practices, automated releases, and quality checks.

**Current version:** `@v1`

## Documentation

- [Workflow Guide](docs/workflows.md)
- [Artifact Verification](docs/verification.md)
- [Scripts Reference](docs/scripts.md)

---

## Table of Contents
- [Introduction](#introduction)
- [Quick Start](#quick-start)
- [Pull Request Workflow](#pull-request-workflow)
- [Release Workflow](#release-workflow)
- [Development Container Workflow](#development-container-workflow)
- [Workflow Architecture](#workflow-architecture)
- [Available Components](#available-components)
- [Environment Variables Matrix](#environment-variables-matrix)
- [Prerequisites Check Matrix](#prerequisites-check-matrix)
- [Permission Requirements Matrix](#permission-requirements-matrix)
- [Getting Access to Secrets](#getting-access-to-secrets)
- [Workflow Reference](#workflow-reference)
- [Examples](#examples)
- [Version Tag Format](#version-tag-format)
- [Local Testing](#local-testing)

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

#### Option 1: Use Everything -
```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v1
with:
  projectType: maven
  artifactPublisher: maven-app-github
  containerBuilder: containerimage-ghcr
  releasePublisher: github-cli
```

#### Option 2: Configure What You Need
```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v1
with:
  projectType: npm
  containerBuilder: containerimage-ghcr  # Only build containers
```

#### Option 3: Build Your Own Flow
```yaml
jobs:
  my-build:
    # Your custom steps
    
  my-container:
    needs: my-build
    uses: diggsweden/reusable-ci/.github/workflows/build-container-ghcr.yml@v1
```

#### Option 4: Complete Custom Implementation
```yaml
jobs:
  custom-everything:
    # Your own implementation
    # Required: security scanning, license compliance, SBOM generation, 
    # artifact signing, SLSA attestation
```

## Quick Start


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
# SPDX-FileCopyrightText: 2024 Digg - Agency for Digital Government
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
    uses: diggsweden/reusable-ci/.github/workflows/pullrequest-orchestrator.yml@v1
    
    # Pass organization-level secrets to the workflow
    # Required for access to private GitHub Packages
    # Without this, workflow cannot fetch private @diggsweden/* packages
    secrets: inherit
    
    permissions:
      contents: read         # Required: Clone and read repository source code
      packages: read         # Required: Download private dependencies from GitHub Packages
      security-events: write # Required: Upload vulnerability scan results to GitHub Security tab
    
    with:
      projectType: maven  # Determines build commands and dependency management (maven/npm)
  
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
    uses: diggsweden/reusable-ci/.github/workflows/pullrequest-orchestrator.yml@v1
    
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
      projectType: maven              # Required. Valid: maven, npm
      
      # OPTIONAL PARAMETERS (shown with defaults)
      baseBranch: main               # Default: main. Base branch for commit linting
      
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

### Basic Release Workflow
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
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v1
    secrets: inherit  # Use org-level GPG keys and publishing credentials
    permissions:
      contents: write         # Create GitHub releases and tags
      packages: write         # Publish artifacts and containers to GitHub
      id-token: write        # Generate OIDC token for attestations
      actions: read          # Read workflow for SLSA provenance
      security-events: write # Upload container scan results
      attestations: write    # Attach SBOM to container images
    with:
      projectType: maven
      artifactPublisher: maven-app-github  # Publishes JAR to GitHub Packages
      containerBuilder: containerimage-ghcr  # Creates Docker image in ghcr.io
      releasePublisher: jreleaser  # Creates GitHub release with changelog
```

### Full Configuration Example (With Defaults)
```yaml
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

permissions:
  contents: read  # Best Security practice. Jobs only get read as base, and then permissions are added as needed

jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v1
    secrets: inherit
    permissions:
      contents: write       # Create GitHub releases and tags
      packages: write       # Push packages to GitHub Packages/ghcr.io
      id-token: write      # Generate OIDC token for SLSA provenance
      attestations: write  # Attach SBOM attestations to containers
      security-events: write
    with:
      projectType: maven
      
      # === PUBLISHERS & BUILDERS ===
      # Artifact Publisher - Choose based on your package type:
      artifactPublisher: maven-app-github        # Publishes JAR/WAR to GitHub Packages
      # artifactPublisher: maven-lib-mavencentral # For libraries going to Maven Central (needs credentials)
      # artifactPublisher: npm-app-github         # Publishes to GitHub NPM registry
      
      containerBuilder: containerimage-ghcr      # Creates multi-arch Docker images in ghcr.io
      
      changelogCreator: git-cliff                # Generates changelog from commit messages
      
      # Release Publisher - Platform-specific:
      releasePublisher: jreleaser               # Uses JReleaser (for Java projects)
      # releasePublisher: github-cli            # Uses GitHub CLI (for Node.js projects)
      
      # === ARTIFACT SETTINGS (showing defaults) ===
      artifact.javaversion: "21"                # Default: "21". JDK version for Maven builds
      artifact.nodeversion: "22"                # Default: "22". Node.js version for NPM builds  
      artifact.attachpattern: "target/*.jar"    # Default: "target/*.jar". Files to attach to release
      artifact.npmtag: "latest"                 # Default: "latest". NPM dist-tag
      # artifact.settingspath: ".mvn/settings.xml" # No default. Custom Maven settings path
      artifact.jreleaserenabled: false          # Default: false. Enable JReleaser Maven plugin
      
      # === CONTAINER SETTINGS (showing defaults) ===
      container.registry: "ghcr.io"             # Default: "ghcr.io". Container registry
      container.platforms: "linux/amd64,linux/arm64"  # Default: "linux/amd64,linux/arm64". Target platforms
      container.enableslsa: true                # Default: true. SLSA provenance attestation
      container.enablesbom: true                # Default: true. Generate SBOM
      container.enablescan: true                # Default: true. Trivy vulnerability scan
      container.containerfile: "Containerfile"  # Default: "Containerfile". Dockerfile path
      
      # === CHANGELOG SETTINGS (showing defaults) ===
      changelogCreator: "git-cliff"             # Default: "git-cliff". Changelog generator
      changelog.config: ".github-templates/gitcliff-templates/keepachangelog.toml" # Default template
      changelog.skipversionbump: false          # Default: false. Skip version bump
      
      # === RELEASE SETTINGS (showing defaults) ===
      # releasePublisher: jreleaser             # No default. Choose jreleaser or github-cli
      release.config: "jreleaser.yml"           # Default: "jreleaser.yml". JReleaser config
      release.generatesbom: true                # Default: true. Generate SBOM for release
      release.signartifacts: true               # Default: true. GPG sign artifacts
      release.checkauthorization: false         # Default: false. Check user authorization
      release.draft: false                      # Default: false. Create draft release
      # releaseType: stable                     # Auto-detected from tag (v1.0.0=stable, v1.0.0-beta=prerelease)
      branch: "main"                             # Default: "main". Base branch for changelog
      
      # === ADVANCED SETTINGS (showing defaults) ===
      workingDirectory: "."                     # Default: ".". Working directory
      # file_pattern: Auto-detected based on projectType (Maven: "CHANGELOG.md pom.xml", NPM: "CHANGELOG.md package.json package-lock.json")
      
      # Note: The following features are automatically handled by the workflows:
      # - File pattern detection based on project type (Maven, NPM, Gradle, Python)
      # - SHA-256 checksums in checksums.sha256 (generated by JReleaser or GitHub CLI)
      # - GPG signing (when OSPO_BOT_GPG_* secrets are configured)
      # - SLSA/SBOM catalogs (controlled by container.enableslsa and release.generatesbom)
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
    uses: diggsweden/reusable-ci/.github/workflows/release-dev-orchestrator.yml@v1
    permissions:
      contents: write   # Version bump commits
      packages: write   # Push dev containers to ghcr.io
    with:
      projectType: maven  # or npm
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
| **maven-app-github** | Publishes Maven JARs to GitHub Packages | JAR artifacts in GitHub Packages | GITHUB_TOKEN | Java apps for internal distribution |
| **maven-lib-mavencentral** | Publishes Maven libraries to Maven Central | Public Maven artifacts | MAVENCENTRAL_USERNAME, MAVENCENTRAL_PASSWORD | Open source Java libraries |
| **npm-app-github** | Publishes NPM packages to GitHub registry | NPM packages in GitHub Packages | NPM_TOKEN | Node.js apps/libs for internal use |

#### Container Builders
| Component | Purpose | Features | Build Time | Use When |
|-----------|---------|----------|------------|----------|
| **containerimage-ghcr** | Production multi-platform container builds | SLSA attestation, SBOM, vulnerability scanning, multi-arch | ~10-15 min | Production releases |
| **containerimage-ghcr-dev** | Fast single-platform dev builds | Basic image only, SHA-based tags | ~2-3 min | Development/testing |

#### Release Tools
| Component | Purpose | Creates/Updates | Required Secrets | Use When |
|-----------|---------|----------------|------------------|----------|
| **jreleaser** | Automated GitHub releases | GitHub release, changelog, signatures | RELEASE_TOKEN, GPG keys | Any production release |
| **github-release** | Simple GitHub release creation | GitHub release with assets | GITHUB_TOKEN | Basic releases without JReleaser |
| **version-bump-changelog** | Version management | Updated version files, changelog | GITHUB_TOKEN | Before releases |

#### Validators
| Component | Purpose | Validates | Blocks On | Use When |
|-----------|---------|-----------|-----------|----------|
| **validate-release-prerequisites** | Pre-release checks | Version match, permissions, secrets | Any validation failure | Before any release |

> **Note:** New components are created based on requests and needs. Currently planned for the near future:
> - **Monorepo support** - Build multiple services/packages from a single repository
> - **Gradle support** - For Gradle-based Java/Kotlin projects
> - **NPM library publisher** - For publishing NPM libraries (not just applications)
> 
> To request a new component, open an issue in the `.github` repository.

### Publishers

#### `maven-app-github`
Publishes Maven applications to GitHub Packages.
```yaml
uses: ./.github/workflows/publish-maven-app-github.yml
with:
  javaVersion: "21"        # JDK version (17, 21, etc.)
  workingDirectory: "."    # Subdirectory containing pom.xml
  branch: main             # Branch to checkout for build
  attachPattern: "target/*.jar"  # Files to attach to release (supports wildcards)
```

#### `maven-lib-mavencentral`
Publishes Maven libraries to Maven Central.
```yaml
uses: ./.github/workflows/publish-maven-lib-central.yml
with:
  javaVersion: "21"
  workingDirectory: "."
  settingsPath: ".mvn/settings.xml"  # Maven settings with Central credentials
  jreleaserEnabled: true              # Use JReleaser plugin from pom.xml
```

#### `npm-app-github`
Publishes NPM packages to GitHub Packages.
```yaml
uses: ./.github/workflows/publish-npm-app-github.yml
with:
  nodeVersion: "22"       # Node.js version (20, 22, lts/*, etc.)
  workingDirectory: "."
  npmTag: "latest"        # Distribution tag (latest, next, beta)
```

### Container Builders

#### `containerimage-ghcr`
Production container builds with full security features.
```yaml
uses: ./.github/workflows/build-container-ghcr.yml
with:
  containerfile: "Containerfile"        # Path to Container/Dockerfile
  context: "."                          # Docker build context directory
  platforms: "linux/amd64,linux/arm64"  # Multi-architecture support
  enableSLSA: true                      # Generate supply chain attestation
  enableSBOM: true                      # Embed Software Bill of Materials
  enableScan: true                      # Run vulnerability scanning with Trivy
  registry: "ghcr.io"                   # Container registry URL
```

#### `containerimage-ghcr-dev`
Fast development container builds.
```yaml
uses: ./.github/workflows/build-container-ghcr-dev.yml
with:
  containerfile: "Dockerfile"
  registry: "ghcr.io"
  projectType: maven       # Build system (determines build commands)
  workingDirectory: "."    # Where to run maven/npm build
```

### Other Components

#### `version-bump-changelog`
Handles version bumping and changelog generation.
```yaml
uses: ./.github/workflows/version-bump-changelog.yml
with:
  projectType: maven      # Determines version file (pom.xml vs package.json)
  branch: main            # Base branch for changelog comparison
  releaseType: stable     # stable or prerelease (affects version bump)
```

#### `github-release`
Creates GitHub releases with assets.
```yaml
uses: ./.github/workflows/github-release.yml
with:
  attachPattern: "target/*.jar"  # Files to upload as release assets
  generateSBOM: true              # Include CycloneDX/SPDX SBOM files
  signArtifacts: true             # GPG sign all release artifacts
```

#### `validate-release-prerequisites`
Validates release requirements.
```yaml
uses: ./.github/workflows/validate-release-prerequisites.yml
with:
  projectType: maven
  artifactPublisher: maven-app-github
  checkAuthorization: true  # Verify user has permission to release
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
| **MAVENCENTRAL_USERNAME** | `maven-lib-mavencentral` | During publish | Sonatype username | Maven Central auth |
| **MAVENCENTRAL_PASSWORD** | `maven-lib-mavencentral` | During publish | Sonatype password | Maven Central auth |
| **NPM_TOKEN** | `npm-app-github` | During publish | GitHub NPM token | NPM registry auth |
| **RELEASE_TOKEN** | JReleaser | During release | GitHub PAT | JReleaser operations |
| **AUTHORIZED_RELEASE_DEVELOPERS** | Production releases | Pre-release check | Comma-separated usernames | Who can release |

## Prerequisites Check Matrix

| Check | When Performed | What It Validates | Fails If | How to Fix |
|-------|----------------|-------------------|----------|------------|
| **Version Match** | Release workflow | Tag matches project version | `v1.0.0` tag but pom.xml has `1.0.1` | Ensure tag matches version exactly |
| **GPG Key** | When `signatures: true` | GPG key is valid and accessible | Key expired or malformed | Generate new GPG key, export as base64 |
| **Maven Central Creds** | `maven-lib-mavencentral` | Can authenticate to Sonatype | Invalid username/password | Verify Sonatype account credentials |
| **NPM Registry** | `npm-app-github` | Can authenticate to registry | Token expired or invalid scope | Generate new NPM token with publish scope |
| **Container Registry** | `containerBuilder` set | Can push to registry | No write permission | Ensure `packages: write` permission |
| **GitHub Release** | Release creation | Can create releases | No `contents: write` | Add permission to workflow |
| **Protected Branch** | On push to main | User has bypass rights | Actor lacks permission | Add user to bypass list |
| **Artifact Existence** | During upload | Build artifacts exist | `target/*.jar` not found | Ensure build succeeds first |
| **Container/Dockerfile** | Container build | Dockerfile exists | No Dockerfile in root | Create Dockerfile or specify path |
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
   - GPG signing → Request `GPG_SECRET_KEY` and `GPG_PASSPHRASE`
   - Maven Central → Request `MAVEN_CENTRAL_USERNAME` and `MAVEN_CENTRAL_PASSWORD`
   - NPM publishing → Request `NPM_TOKEN`
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



See `.github/scripts/README.md` for detailed documentation.

---
