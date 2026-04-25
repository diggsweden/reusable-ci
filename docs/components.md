## Available Components

This document describes reusable workflow components and how they relate to the supported orchestrator entrypoints.

**Recommended stable GitHub entrypoints:**
- `pullrequest-orchestrator.yml`
- `release-orchestrator.yml`
- `release-dev-orchestrator.yml`

Leaf helper workflows such as `build-*`, `publish-*`, `lint-*`, `security-*`, `validate-*`, and selected release helpers can still be used directly by advanced consumers and are suitable for repo-local custom orchestration.

Stage workflows such as `pullrequest-quality-stage.yml`, `release-prepare-stage.yml`, `release-build-stage.yml`, `release-publish-stage.yml`, `release-dev-build-stage.yml`, and `release-dev-publish-stage.yml` are internal composition helpers. Advanced consumers may still use them, but they should be treated as less stable direct-use contracts than the orchestrators and leaf helpers.

**When to use components:**
- You need fine-grained control over builds, publishing, validation, or security checks
- You want to compose custom workflows around leaf helpers
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
| **publish-dev-container** | Fast single-platform dev builds | Basic image only, SHA-based tags | ~2-3 min | Development/testing |

#### Release Tools

| Component | Purpose | Creates/Updates | Required Secrets | Use When |
|-----------|---------|----------------|------------------|----------|
| **release-github** | GitHub release creation | GitHub release, changelog, signatures | RELEASE_BOT_TOKEN, GPG keys | Any production release |
| **version-bump** | Version management | Updated version files | GITHUB_TOKEN, RELEASE_BOT_TOKEN | Before releases |
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

#### `build-gradle-app.yml`
Builds Gradle JVM projects — libraries, applications, plugins — and uploads their JARs. Android is out of scope; use `build-gradle-android.yml` for APKs/AABs with flavors and Google Play publishing.
```yaml
uses: ./.github/workflows/build-gradle-app.yml
with:
  java-version: "25"           # JDK version
  working-directory: "."       # Path to build.gradle
  gradle-tasks: "build"        # Gradle tasks to run
  skip-tests: false            # Skip `test` task
  artifact-name: ""            # Custom artifact name (optional)
```

#### `build-gradle-android.yml`
Builds Android applications with multiple product flavors and build types. Sole Android path: sets up the Android SDK, handles keystore decoding for signing, and produces split APK/AAB artifacts with Google Play-friendly naming.
```yaml
uses: ./.github/workflows/build-gradle-android.yml
with:
  java-version: "25"              # JDK version
  build-module: "app"             # Gradle module
  product-flavor: "demo"          # Product flavor (demo, prod, etc.)
  build-types: "debug,release"    # Build types to create
  include-aab: true               # Build AAB for Play Store
  enable-signing: true            # Enable Android signing
  artifact-name-prefix: ""        # Artifact name prefix
  include-date-stamp: true        # Include date in artifact names
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
  enable-analyzed-container-sbom: true   # was `enable-sbom: true` in v2; rename
  enable-scan: true
  registry: "ghcr.io"
```

#### `publish-dev-container.yml`
Fast development container builds. Supports multiple registries.
```yaml
uses: ./.github/workflows/publish-dev-container.yml
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

#### `release-create-github.yml`
Creates GitHub releases with assets.

Usually called by `release-orchestrator.yml`, but can also be used directly by advanced consumers that want lower-level release composition.

#### `validate-release-prerequisites.yml`
Validates release requirements (called automatically by orchestrator).

Usually called by `release-orchestrator.yml`, but can also be used directly by advanced consumers that want explicit prerequisite validation.

### PR Orchestrator

#### `pullrequest-orchestrator.yml`
Orchestrates all quality checks for pull requests. Composes a control-plane interface, delegates to the quality stage, and produces a top-level summary.

```yaml
uses: diggsweden/reusable-ci/.github/workflows/pullrequest-orchestrator.yml@72b9c326139080c9a9c91999ada2d62d19e7ee54 # v2.7.0
with:
  project-type: maven              # Required: maven, npm, python
  base-branch: ""                  # Optional: auto-detects PR target
  linters.commitlint: true         # Deprecated v3.0: migrate to devbasecheck
  linters.licenselint: true        # Deprecated v3.0: migrate to devbasecheck
  linters.dependencyreview: true   # Dependency vulnerability review
  security.sast-opengrep: true     # OpenGrep SAST (default; set false to opt out)
  security.sast-opengrep-rules: p/default
  security.sast-opengrep-fail-on-severity: high
  linters.megalint: true           # Deprecated v3.0: migrate to devbasecheck
  linters.publiccodelint: false    # Publiccode.yml validation
  linters.devbasecheck: false      # Recommended: replaces deprecated linters
  linters.swiftformat: false       # Swift format for iOS/macOS
  linters.swiftlint: false         # SwiftLint for iOS/macOS
  reusable-ci-ref: v2.7.0           # Match the pinned workflow release
```

**Behavior:** The orchestrator remains the supported entrypoint. Internally it delegates to the quality stage, which writes a normalized manifest consumed by the top-level PR summary. See [PR Quality Stage Result Contract](workflows.md#pr-quality-stage-result-contract) for the internal schema.

### Lint Workflows

These workflows are automatically called by `pullrequest-orchestrator.yml`.

#### `lint-commit.yml`
Validates commit messages follow conventional commit format using [gommitlint](https://codeberg.org/itiquette/gommitlint).
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

#### `lint-devbase.yml`
Runs quality checks using devbase-check. Client justfile overrides work both locally and in CI.
```yaml
uses: ./.github/workflows/lint-devbase.yml
with:
  devbase-check-version: ""  # Optional: override pinned version
```

**Features:**
- Same `verify.sh` script runs locally and in CI
- Client justfile overrides (e.g., `lint-license: @echo "Skipping"`) work in CI
- Generates GitHub Actions summary with pass/fail per linter
- Version-pinned devbase-check with Renovate auto-updates

### Security Workflows

These workflows are automatically called by `pullrequest-orchestrator.yml`.

#### `security-dependency-review.yml`
Reviews dependencies for known vulnerabilities.
```yaml
uses: ./.github/workflows/security-dependency-review.yml
```

#### `security-opengrep.yml`
Runs OpenGrep SAST and emits portable outputs for GitHub and GitLab-style integrations.
```yaml
uses: ./.github/workflows/security-opengrep.yml
with:
  opengrep-rules: p/default
  fail-on-severity: high
```

The workflow runs directly on the GitHub runner in this branch. The runtime container path is introduced later on the GitLab prep branch.

SARIF is always generated and saved as a workflow artifact. To publish results into GitHub Security / Code Scanning, configure the org or repo secret `SARIF_UPLOAD_TOKEN` and pass secrets with `secrets: inherit`.

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
| `pullrequest-orchestrator.yml` | Pull request quality control plane | Every repository |
| `release-orchestrator.yml` | Production release control plane | Production releases |
| `release-dev-orchestrator.yml` | Lightweight dev release control plane | Development branches |

### Dev vs Production Release

| Aspect | Dev | Production |
|--------|-----|------------|
| Build time | ~3-5 min | ~12-15 min |
| Container image | ✓ | ✓ + SLSA + SBOM |
| Build artifacts | ✓ (JARs/tarballs) | ✓ |
| NPM publish | ✓ (dev tag) | ✓ |
| Maven publish | — | ✓ (libraries only) |
| GitHub Release | — | ✓ |
