## Available Components

This document describes reusable workflow components and how they relate to the supported orchestrator entrypoints.

> For a per-ecosystem capability matrix and the artefact-first vs container-first framing (when does a language ship via `build-<lang>.yml` vs `sbom-<lang>.yml` + Containerfile compile?), see **[docs/ecosystems.md](ecosystems.md)**.

**Recommended stable GitHub entrypoints:**
- `pullrequest-orchestrator.yml`
- `release-orchestrator.yml`
- `release-dev-orchestrator.yml`

Leaf helper workflows such as `build-*`, `publish-*`, `lint-*`, `security-*`, `validate-*`, and selected release helpers can still be used directly by advanced consumers and are suitable for custom orchestration. The examples below use external consumer refs; inside this repository's own workflows, the same components are called with local `./.github/workflows/...` paths.

> **Trigger constraint for direct callers of publish / release / signing leaves.**
> Every reusable workflow that handles signing, package-registry, or
> platform-API secrets refuses to run on `pull_request`,
> `pull_request_target`, `pull_request_review`,
> `pull_request_review_comment`, or `issue_comment` triggers. The guard
> step (`reusable-ci validate event-context`) runs as the first
> secret-touching step of every privileged leaf. Direct callers must
> wire their caller workflow under `push`, `workflow_dispatch`,
> `release`, `schedule`, `workflow_run`, or `merge_group`; the
> [threat model](threat-model.md#what-reusable-ci-defends-against)
> describes the property in full. Adopters with a legitimate
> PR-context publish need (preview deploys) override per-call-site
> via the `--allowed-events` flag on the guard step — never as an
> env-var bypass.

The YAML blocks in this page are **job-level snippets**. Place them under
`jobs.<job-id>` in your workflow and add the permissions/secrets required by the
component you call.

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
| **publish-maven-github** | Publishes Maven libraries/NPM to GitHub Packages | Artifacts in GitHub Packages | GITHUB_TOKEN | When supported artifacts include `publish-to: [github-packages]` |
| **publish-maven-central** | Publishes Maven libraries to Maven Central | Public Maven artifacts | MAVEN_CENTRAL_USERNAME, MAVEN_CENTRAL_PASSWORD | Public libraries (requires build-type: library) |

#### Container Builders

| Component | Purpose | Features | Build Time | Use When |
|-----------|---------|----------|------------|----------|
| **publish-container** | Production multi-platform container builds | SLSA attestation, SBOM, vulnerability scanning, native split-runner multi-arch (no QEMU) | ~5-10 min | Production releases |
| **publish-dev-container** | Fast dev container builds, single- or multi-platform | Dev tags, no SLSA or vulnerability scan, optional analyzed-container SBOM, native split-runner when multi-arch | ~3-5 min | Development/testing |

#### Release Tools

| Component | Purpose | Creates/Updates | Required Secrets | Use When |
|-----------|---------|----------------|------------------|----------|
| **release-create-github** | GitHub release creation | GitHub release, changelog, signatures | RELEASE_TOKEN, GPG keys | Any production release |
| **version-bump** | Version management | Updated version files | GITHUB_TOKEN, RELEASE_TOKEN | Before releases |
| **generate-changelog** | Changelog generation | Formatted changelog | GITHUB_TOKEN | Before releases |

#### Validators

| Component | Purpose | Validates | Blocks On | Use When |
|-----------|---------|-----------|-----------|----------|
| **validate-release-prerequisites** | Pre-release checks | Version match, permissions, secrets | Any validation failure | Before any release |

> **Note:** To request a new component or publisher, open an issue in the reusable-ci repository.

### Build Workflows

#### `build-maven.yml`
Builds Maven projects (apps or libraries).
```yaml
uses: diggsweden/reusable-ci/.github/workflows/build-maven.yml@v3.0.0
with:
  build-type: app           # "app" or "lib"
  working-directory: "."    # Path to pom.xml
```

Direct callers use `app`/`lib`. In `artifacts.yml`, use
`application`/`library`; the orchestrator maps those values to this workflow's
input.

#### `build-npm.yml`
Builds NPM projects.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/build-npm.yml@v3.0.0
with:
  working-directory: "."    # Path to package.json
```

#### `build-go.yml`
Builds artifact-first Go binaries, generates a native CycloneDX Build SBOM with
`cyclonedx-gomod`, and uploads `dist/<goos>-<goarch>/<binary>-<goos>-<goarch>`.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/build-go.yml@v3.0.0
with:
  working-directory: "."
  main-package: ./cmd/my-cli
  binary-name: my-cli
  platforms: linux/amd64,linux/arm64,darwin/amd64,darwin/arm64
```

#### `sbom-go.yml`
Generates the native CycloneDX Build SBOM for container-first Go projects with
`cyclonedx-gomod`. It does not compile; the project's Containerfile owns
`go build`.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/sbom-go.yml@v3.0.0
with:
  working-directory: "."
```

#### `build-cargo.yml`
Builds artefact-first Rust binaries via `cargo build --release --target …`,
cross-compiles per platform into `dist/<goos>-<goarch>/<binary>-<goos>-<goarch>`
(matching Go's shape), and emits an inline CycloneDX Build SBOM with
`cargo-cyclonedx`. Used for Cargo artefacts with `config.build-mode:
artifact-first`. Container-first cargo continues to use `sbom-cargo.yml`.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/build-cargo.yml@v3.0.0
with:
  working-directory: "."
  binary-name: my-cli
  platforms: linux/amd64,linux/arm64
```

#### `sbom-cargo.yml`
Generates the lockfile-derived CycloneDX Build SBOM for container-first Rust
projects with `cargo-cyclonedx`. It does not compile; the project's
Containerfile owns `cargo build`.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/sbom-cargo.yml@v3.0.0
with:
  working-directory: "."
```

#### `build-gradle-app.yml`
Builds Gradle JVM projects — libraries, applications, plugins — and uploads their JARs. Android is out of scope; use `build-gradle-android.yml` for APKs/AABs with flavors and Google Play publishing.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/build-gradle-app.yml@v3.0.0
with:
  working-directory: "."       # Path to build.gradle
  gradle-tasks: "build"        # Gradle tasks to run
  skip-tests: false            # Skip `test` task
  artifact-name: ""            # Custom artifact name (optional)
```

#### `build-gradle-android.yml`
Builds Android applications with multiple product flavors and build types. Sole Android path: sets up the Android SDK, handles keystore decoding for signing, and produces split APK/AAB artifacts with Google Play-friendly naming.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/build-gradle-android.yml@v3.0.0
with:
  build-module: "app"             # Gradle module
  product-flavor: "demo"          # Product flavor (demo, prod, etc.)
  build-types: "debug,release"    # Build types to create
  include-aab: true               # Build AAB for Play Store
  enable-signing: true            # Enable Android signing
  artifact-name: ""               # Override the derived upload name (orchestrator passes artifacts.yml `name:`)
  artifact-name-prefix: ""        # Prefix for derived names (ignored when artifact-name is set)
  include-date-stamp: true        # Include date in derived names (ignored when artifact-name is set)
  # gradle-tasks: ""              # Optional: explicit task list, overrides product-flavor/build-types/include-aab
```

**Task derivation.** By default the workflow derives gradle tasks from `product-flavor` + `build-types` + `include-aab` — e.g., `product-flavor: demo`, `build-types: release`, `include-aab: true` → `assembleDemoRelease app:bundleDemoRelease`. No surrounding `build` task is run by default; only the targeted variants/bundles. Set `gradle-tasks` to override the derived list verbatim — useful when you need custom task names or to add lint/test alongside.

**Artifact naming.** Two modes:

- **Override (orchestrator path).** `release-build-stage.yml` forwards each artifact's `name:` from artifacts.yml as `artifact-name`. The AAB uploads under that exact identifier; APK debug/release and the build SBOM get `<name>-debug`, `<name>-release`, `<name>-sbom`. `release-publish-stage.yml` then hands the same `name:` to `publish-google-play.yml` as `aab-artifact-name`, so the download matches what build uploaded. `artifact-name-prefix` and `include-date-stamp` are ignored in this mode.
- **Derived (direct callers).** When `artifact-name` is empty, upload names are composed as `[<date> - ][<prefix> - ]<repo>[ - <flavor>] - {APK debug|APK release|AAB release|build SBOM}` from `include-date-stamp`, `artifact-name-prefix`, `${{ github.event.repository.name }}`, and `product-flavor`. Direct callers should wire the publish step's `aab-artifact-name` to `${{ needs.build.outputs.aab-name }}` instead of duplicating the format string.

### Publish Workflows

#### `publish-maven-github.yml`
Publishes Maven libraries and NPM artifacts to GitHub Packages. Maven apps and
Gradle publishing are not wired today; use a project-owned publishing workflow
until reusable-ci adds those publishers.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/publish-maven-github.yml@v3.0.0
with:
  package-type: maven          # maven or npm
  artifact-source: maven-build-artifacts  # Name of workflow artifact
  working-directory: "."
```

#### `publish-maven-central.yml`
Publishes Maven libraries to Maven Central.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/publish-maven-central.yml@v3.0.0
with:
  artifact-source: maven-build-artifacts  # Name of workflow artifact
  working-directory: "."
  settings-path: ".mvn/settings.xml"
```

### Container Workflows

#### `publish-container.yml`
Production container builds with full security features. Supports multiple registries.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/publish-container.yml@v3.0.0
with:
  reusable-ci-binary-ref: v3.0.0
  container-file: "Containerfile"
  context: "."
  artifact-types: maven
  platforms: "linux/amd64,linux/arm64"
  enable-slsa: true
  enable-analyzed-container-sbom: true   # was `enable-sbom: true` in v2; rename
  enable-scan: true
  registry: "ghcr.io"
```

#### `publish-dev-container.yml`
Fast development container builds. Supports multiple registries.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/publish-dev-container.yml@v3.0.0
with:
  reusable-ci-binary-ref: v3.0.0
  container-file: "Containerfile"  # or "Dockerfile"
  registry: "ghcr.io"
  artifact-types: maven
  working-directory: "."
```

### Other Components

#### `version-bump.yml`
Handles version bumping and updates version files.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/version-bump.yml@v3.0.0
with:
  project-type: maven      # Determines version file (pom.xml vs package.json)
  branch: main             # Base branch for comparison
  working-directory: "."   # Path to project root
```

#### `generate-changelog.yml`
Generates changelog from git commits.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/generate-changelog.yml@v3.0.0
with:
  branch: main             # Base branch for changelog comparison
  changelog-config: ""     # Optional: custom git-cliff config
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
uses: diggsweden/reusable-ci/.github/workflows/pullrequest-orchestrator.yml@v3.0.0
with:
  project-type: maven              # Required: maven, npm, gradle, gradle-android, xcode-ios, cargo, go (python reserved)
  base-branch: ""                  # Optional: auto-detects PR target
  linters.nanolinter: true         # Default — runs just lint via mise-installed nanolinter
  linters.devbasecheck: false      # Legacy alternative — runs just lint via a cloned devbase-check
  linters.dependencyreview: true   # Dependency vulnerability review
  security.sast-opengrep: true     # OpenGrep SAST (default; set false to opt out)
  security.sast-opengrep-rules: p/default
  security.sast-opengrep-fail-on-severity: high
  linters.publiccodelint: false    # Publiccode.yml validation
  linters.swiftformat: false       # Swift format for iOS/macOS
  linters.swiftlint: false         # SwiftLint for iOS/macOS
  reusable-ci-binary-ref: v3.0.0   # Match the pinned workflow release
```

**Behavior:** The orchestrator remains the supported entrypoint. Internally it delegates to the quality stage, which writes a normalized manifest consumed by the top-level PR summary. See [Stage Result Contract](workflows.md#stage-result-contract) for the internal schema.

### Lint Workflows

These workflows are automatically called by `pullrequest-orchestrator.yml`.

#### `lint-misc.yml`
Performs miscellaneous validation checks.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/lint-misc.yml@v3.0.0
```

#### `lint-publiccode.yml`
Validates publiccode.yml file format.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/lint-publiccode.yml@v3.0.0
```

#### `lint-nanolinter.yml`
Default lint surface — runs `nanolinter`, covering the consumer's `just lint`
plan (commit messages, SPDX/license headers, and filesystem-level
multi-language checks). nanolinter and every check tool are pinned in the
consumer's `.mise.toml` and installed via `mise install`. Client `justfile`
overrides work both locally and in CI.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/lint-nanolinter.yml@v3.0.0
```

Runs the consumer repository's aggregate `just lint-all` (or `just lint`);
client justfile overrides like `lint-yaml: @echo "Skipping"` work in CI the
same way they work locally. nanolinter is pinned in `.mise.toml` and tracked
by Renovate.

#### `lint-devbase.yml`
Legacy lint surface — runs `devbase-check` instead of nanolinter, covering the
same `just lint` plan via a cloned devtools repo. Enable with
`linters.devbasecheck: true` (and `linters.nanolinter: false`).
```yaml
uses: diggsweden/reusable-ci/.github/workflows/lint-devbase.yml@v3.0.0
with:
  devbase-check-version: ""  # Optional: override pinned version
```

Like `lint-nanolinter.yml`, it runs the consumer repository's aggregate `just
lint-all` (or `just lint`); client justfile overrides work in CI the same way
they work locally. The `devbase-check` version is pinned and tracked by
Renovate.

### Security Workflows

These workflows are automatically called by `pullrequest-orchestrator.yml`.

#### `security-dependency-review.yml`
Reviews dependencies for known vulnerabilities.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/security-dependency-review.yml@v3.0.0
```

#### `security-opengrep.yml`
Runs OpenGrep SAST and emits portable outputs for GitHub and GitLab-style integrations.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/security-opengrep.yml@v3.0.0
with:
  opengrep-rules: p/default
  fail-on-severity: high
```

The workflow runs inside the reusable-ci runtime image and emits SARIF plus portable report artifacts.

SARIF is always generated and saved as a workflow artifact. To publish results into GitHub Security / Code Scanning, configure the org or repo secret `CODE_SCANNING_TOKEN` and pass secrets with `secrets: inherit`.

#### `security-openssf-scorecard.yml`
Generates OpenSSF security scorecard for the repository.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/security-openssf-scorecard.yml@v3.0.0
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
| Container image | ✓ with dev tag; optional analyzed-container SBOM | ✓ + SLSA + SBOM + vulnerability scan |
| Build artifacts | ✓ for artifact-first ecosystems | ✓ |
| SBOMs | Default `none`; opt in with `sboms` | Default `all` |
| NPM publish | ✓ (dev tag) | ✓ |
| Maven publish | — | ✓ (libraries only) |
| GitHub Release | — | ✓ |
