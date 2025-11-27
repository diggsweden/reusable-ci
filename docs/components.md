## Available Components

This document describes individual workflow components that can be used standalone or combined with orchestrators.

**When to use components:**
- You need fine-grained control over the release process
- You want to create custom workflows
- You're integrating with existing CI/CD pipelines

**When to use orchestrators:**
- You want a complete, ready-to-use release workflow
- You prefer convention over configuration
- You're starting a new project (recommended)

See [Workflow Guide](workflows.md) for orchestrator documentation and [Artifacts Reference](artifacts-reference.md) for configuration.

### Component Overview Matrix

#### Artifact Publishers

| Component | Purpose | Output | Required Secrets | Use When |
|-----------|---------|--------|------------------|----------|
| **publish-github** | Publishes Maven/NPM/Gradle to GitHub Packages | Artifacts in GitHub Packages | GITHUB_TOKEN | Default publishing target |
| **publish-maven-central** | Publishes Maven libraries to Maven Central | Public Maven artifacts | MAVENCENTRAL_USERNAME, MAVENCENTRAL_PASSWORD | Public libraries (requires build-type: library) |

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
  build-type: application   # "application" or "library"
  java-version: "25"        # JDK version
  working-directory: "."    # Path to pom.xml
```

#### `build-npm.yml`
Builds NPM projects.
```yaml
uses: ./.github/workflows/build-npm.yml
with:
  node-version: "24"        # Node.js version
  working-directory: "."    # Path to package.json
```

#### `build-gradle.yml`
Builds Gradle projects.
```yaml
uses: ./.github/workflows/build-gradle.yml
with:
  java-version: "25"        # JDK version
  working-directory: "."    # Path to build.gradle
  gradle-tasks: "build"     # Gradle tasks to run
```

### Publish Workflows

#### `publish-github.yml`
Publishes artifacts to GitHub Packages (Maven/NPM/Gradle).
```yaml
uses: ./.github/workflows/publish-maven-github.yml
with:
  package-type: maven          # maven, npm, or gradle
  artifact-source: maven-build-artifacts  # Name of workflow artifact
  working-directory: "."
```

#### `publish-maven-central.yml`
Publishes Maven libraries to Maven Central.
```yaml
uses: ./.github/workflows/publish-maven-central.yml
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
  container-file: "Containerfile"  # or "Dockerfile"
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

### Lint Workflows

These workflows are automatically called by `pullrequest-orchestrator.yml`.

#### `lint-commit.yml`
Validates commit messages follow conventional commit format.
```yaml
uses: ./.github/workflows/lint-commit.yml
```

#### `lint-license.yml`
Checks license compliance using REUSE specifications.
```yaml
uses: ./.github/workflows/lint-license.yml
```

#### `lint-mega.yml`
Runs MegaLinter for multi-language code quality checks.
```yaml
uses: ./.github/workflows/lint-mega.yml
```

#### `lint-misc.yml`
Performs miscellaneous validation checks.
```yaml
uses: ./.github/workflows/lint-misc.yml
```

#### `lint-publiccode.yml`
Validates publiccode.yml file format.
```yaml
uses: ./.github/workflows/lint-publiccode.yml
```

#### `lint-just-mise.yml`
Runs just+mise-based linting using mise-managed tools (lightweight alternative to MegaLinter).
```yaml
uses: ./.github/workflows/lint-just-mise.yml
```

### Security Workflows

These workflows are automatically called by `pullrequest-orchestrator.yml`.

#### `security-dependency-review.yml`
Reviews dependencies for known vulnerabilities.
```yaml
uses: ./.github/workflows/security-dependency-review.yml
```

#### `security-openssf-scorecard.yml`
Generates OpenSSF security scorecard for the repository.
```yaml
uses: ./.github/workflows/security-openssf-scorecard.yml
```

---

## Workflow Reference

### Orchestrator Workflows

| Workflow | Purpose | When to Use |
|----------|---------|-------------|
| `pullrequest-orchestrator.yml` | Run CI checks on PRs | Every repository |
| `release-orchestrator.yml` | Full release process | Production releases |
| `release-dev-orchestrator.yml` | Dev container builds | Development branches |
