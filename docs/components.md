# Components Reference

## Available Components

This document describes reusable workflow components and how they relate to the supported orchestrator entrypoints.

> For a per-ecosystem capability matrix and the artifact-first vs container-first framing (when does a language ship via `build-<lang>.yml` vs `sbom-<lang>.yml` + Containerfile compile?), see **[docs/ecosystems.md](ecosystems.md)**.

**Recommended stable GitHub entrypoints:**
- `pullrequest-orchestrator.yml`
- `release-orchestrator.yml`
- `release-snapshot-orchestrator.yml`

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

Stage workflows such as `pullrequest-quality-stage.yml`, `release-prepare-stage.yml`, `release-build-stage.yml`, `release-publish-stage.yml`, `release-snapshot-build-stage.yml`, and `release-snapshot-publish-stage.yml` are internal composition helpers. Advanced consumers may still use them, but they should be treated as less stable direct-use contracts than the orchestrators and leaf helpers.

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
| **publish-maven-github** | Publishes Maven libraries/NPM to GitHub Packages | Artifacts in GitHub Packages | GITHUB_TOKEN | When supported artifacts include `publish-to: [forge-packages]` |
| **publish-maven-central** | Publishes Maven libraries to Maven Central | Public Maven artifacts | MAVEN_CENTRAL_USERNAME, MAVEN_CENTRAL_PASSWORD | Public libraries (requires build-type: library) |
| **publish-gradle** | Publishes Gradle artifacts (incl. Android libraries) to Maven Central or the forge's package registry | Public/internal Gradle artifacts | MAVEN_CENTRAL_USERNAME, MAVEN_CENTRAL_PASSWORD, RELEASE_GPG_* (Central only) | Gradle or gradle-android artifacts with `publish-to`; Central requires build-type: library |

#### Container Builders

| Component | Purpose | Features | Build Time | Use When |
|-----------|---------|----------|------------|----------|
| **publish-container** | Production multi-platform container builds | SLSA attestation, SBOM, vulnerability scanning, native split-runner multi-arch (no QEMU) | ~5-10 min | Production releases |

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
Builds artifact-first Rust binaries via `cargo build --release --target …`,
cross-compiles per platform into `dist/<goos>-<goarch>/<binary>-<goos>-<goarch>`
(matching Go's shape), and emits an inline CycloneDX Build SBOM with
`cargo-cyclonedx`. Used for Cargo artifacts with `config.build-mode:
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

#### `publish-gradle.yml`
Publishes Gradle-toolchain artifacts from source, using the project's own
`maven-publish` configuration. Android libraries pass the Android runtime
image; there is no `setup-android` input.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/publish-gradle.yml@v3.0.0
with:
  target: maven-central          # or forge-packages
  working-directory: "."
  publish-tasks: ""              # empty → derived per target
  runtime-image: ghcr.io/diggsweden/reusable-ci-runtime-java-25:v3.0.0
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
  enable-analyzed-container-sbom: true   # CycloneDX SBOM of the built image
  enable-scan: true
  registry: "ghcr.io"
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
  lint-engine: nanolinter          # General lint engine: nanolinter (default) | megalinter | none — mutually exclusive
  required-lints: secrets,sast     # Mandated lint floor: forced to run + block via `nanolinter verify --require`, unskippable per project
  megalinter-image: oxsecurity/megalinter:v8  # MegaLinter flavour image when lint-engine=megalinter (pin by @sha256)
  linters.swiftformat: false       # Swift format for iOS/macOS (separate macOS job, orthogonal to the engine)
  linters.swiftlint: false         # SwiftLint for iOS/macOS
  reusable-ci-binary-ref: v3.0.0   # Match the pinned workflow release
```

**Behavior:** The orchestrator remains the supported entrypoint. Internally it delegates to the quality stage, which writes a normalized manifest consumed by the top-level PR summary. See [Stage Result Contract](workflows.md#stage-result-contract) for the internal schema.

### Lint Workflows

These workflows are automatically called by `pullrequest-orchestrator.yml`. The
`lint-engine` input picks the single general engine (mutually exclusive — two
would duplicate Code Scanning findings); Swift/iOS checks run separately in
`lint-swift.yml` regardless of the engine.

#### `lint-nanolinter.yml` (default)
Runs `nanolinter verify` against the consumer's `nanolinter.toml` verify plan,
inside the nanolinter flavour image (which bakes nanolinter and its check
toolchain — no `just`/`justfile` required). Fast, node-less. Security findings
upload to GitHub Code Scanning as SARIF.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/lint-nanolinter.yml@v3.0.0
```

**Mandated lint floor.** The job passes `nanolinter verify --require
"$REQUIRED_LINTS"` (default `secrets,sast`), which forces those checks to *run*
and *block* regardless of the project's `nanolinter.toml` — a project cannot
drop them from `[verify].checks`, exclude them, nor downgrade them to advisory
via `[policy].warn`/`warn_all`. So "every project runs SAST and secret
detection" is enforced, not merely recommended. Override the set with the
`required-lints` input (org-level; a project can only add via its own config —
by `extends`-ing a base with `[policy].require` — never remove). Set
`required-lints: ""` to disable the floor.

Swift/iOS linting runs separately in `lint-swift.yml` (macOS) — nanolinter has
no native macOS binary.

#### `lint-megalinter.yml`
Runs MegaLinter from the pinned `oxsecurity/megalinter` image — the heavier,
governance-recognised alternative for teams standardising on MegaLinter. Reads
the project's own `.mega-linter.yml`. Selected with `lint-engine: megalinter`;
its security findings reach Code Scanning as SARIF (category
`megalinter-security`) through the same enrich/upload path. Heavier and slower
than nanolinter, so nanolinter stays the default.
```yaml
uses: diggsweden/reusable-ci/.github/workflows/lint-megalinter.yml@v3.0.0
```

### Security Workflows

These workflows are automatically called by `pullrequest-orchestrator.yml`.

> **SAST + dependency scanning** are no longer standalone workflows. The
> selected lint engine runs them as part of its plan: `nanolinter` (default)
> runs OpenGrep SAST, OSV dependency scanning, secret detection, and trivy-fs
> (configure in `nanolinter.toml`); `megalinter` runs its own SAST/secrets
> linters (configure in `.mega-linter.yml`). Either uploads its findings to
> GitHub Code Scanning as SARIF via `lint-nanolinter.yml` / `lint-megalinter.yml`.

SARIF is uploaded to GitHub Code Scanning when the org or repo secret
`CODE_SCANNING_TOKEN` is configured and secrets are passed with `secrets:
inherit`; it is always also saved as a workflow artifact.

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
| `release-snapshot-orchestrator.yml` | Lightweight snapshot release control plane (npm/SBOM, plus opt-in Gradle SNAPSHOTs) | Development branches |

### Snapshot vs Production Release

| Aspect | Snapshot | Production |
|--------|----------|------------|
| Build time | ~3-5 min | ~12-15 min |
| Container image | — (builds none; a release-built image is promoted to `:dev` by the promotion ladder) | ✓ + SLSA + SBOM + vulnerability scan |
| Build artifacts | ✓ for artifact-first ecosystems | ✓ |
| SBOMs | Default `none`; opt in with `sboms` | Default `all` |
| NPM publish | ✓ (snapshot tag) | ✓ |
| Maven publish | — | ✓ (libraries only) |
| GitHub Release | — | ✓ |
