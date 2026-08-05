<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Artifacts Reference

Complete reference for the artifacts config format.

> For per-ecosystem capability matrices and the artifact-first vs container-first framing, see **[docs/ecosystems.md](ecosystems.md)**.

## Location

The canonical path is **`.reusable-ci/artifacts.yml`**. The release-orchestrator reads it by default — operators no longer need to pass `artifacts-config:` explicitly. To use a different path, set `artifacts-config: <path>` on the workflow_call.

Co-located with the other reusable-ci files:

```text
.reusable-ci/
├── artifacts.yml                       ← the release plan
├── artifacts.schema.json               ← JSON Schema for editor autocomplete
├── allowed_signers                     ← release-authorisation (SSH)
└── allowed_gpg_keys.asc                ← release-authorisation (GPG)
```

### JSON Schema

[`.reusable-ci/artifacts.schema.json`](../.reusable-ci/artifacts.schema.json) is a Draft 2020-12 schema covering every field of the file. Most editors honour the `# yaml-language-server: $schema=…` directive at the top of a YAML file:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/diggsweden/reusable-ci/main/.reusable-ci/artifacts.schema.json
artifacts:
  - name: my-artifact
    project-type: maven
```

The schema is contract-tested against every shipped `examples/*/artifacts.yml`, so editor warnings line up with what `config parse-artifacts` actually rejects.

## Auto-derive: skip the file for simple repos

When `.reusable-ci/artifacts.yml` is absent AND the repo has **exactly one** ecosystem manifest at root, reusable-ci synthesises the equivalent plan in-memory:

| Manifest at root | Synthesised |
|---|---|
| `pom.xml` | one Maven artifact, name = `artifactId`, working-directory = `.` |
| `package.json` | one NPM artifact, name = unscoped package name |
| `Cargo.toml` | one Cargo artifact, name = `package.name` |
| `go.mod` | one Go artifact, name = basename of `module` path |
| `build.gradle` or `build.gradle.kts` | one Gradle artifact, name = `rootProject.name` (or repo dir name as fallback) |

This collapses the common "single-library repo" case to zero configuration. **Polyglot repos** (more than one root manifest) refuse to auto-derive and demand an explicit `artifacts.yml` — the error names every manifest it found.

## Overview

`artifacts.yml` has two main sections:

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
- **Valid values:** `maven`, `npm`, `gradle`, `gradle-android`, `xcode-ios`, `cargo`, `meta`, `python`, `go`
- **Note:** for `go` and `cargo`, `config.build-mode` selects the pipeline shape; omitted defaults to `artifact-first`. `python` is reserved in the schema but has no workflows yet.
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
- **Description:** Maven build type used by the orchestrator when calling `build-maven.yml`
- **Valid values:** `application` (default), `library`
- **Default:** `application`
- **Applies to:** Maven only. Gradle workflows ignore this field today.
- **Example:** `build-type: library`
- **Behavior:**
  - `application`: Builds with `mvn package`
  - `library`: Builds with `mvn package` via the library path, generating sources/javadoc according to the configured Maven profile/project setup

#### `require-authorization`

- **Type:** `boolean`
- **Description:** Require the release tag to be signed by a key listed in the project's committed allowlist files (`.reusable-ci/allowed_signers` for SSH-signed tags, `.reusable-ci/allowed_gpg_keys.asc` for GPG-signed tags).
- **Default:** `false`
- **Use case:** Production libraries that need cryptographic proof of release authorship.
- **Example:** `require-authorization: true`
- **Effect:** When true, a missing or empty allowlist file fails the release closed with `EX_NOPERM` (exit 77). When the file is present, the signing-key fingerprint must match an entry. See [verification.md → Release Authorisation](verification.md#release-authorisation) for the file format and a worked example.

#### `publish-to`

- **Type:** `array of strings`
- **Description:** Publishing targets for built artifacts
- **Default:** `[]` (no package-registry publisher; container publishing is configured separately under `containers[]`)
- **Supported values:** `forge-packages`, `maven-central`, `google-play`
- **Reserved value:** `npmjs` is schema-recognized for future use, but current
  config validation rejects it because production npmjs.org publishing is not
  implemented yet. Use `forge-packages` for current NPM package publishing.
- **Example:**

  ```yaml
  publish-to:
    - forge-packages
    - maven-central
  ```

- **Example (Android):**

  ```yaml
  publish-to:
    - google-play
  ```

- **Behavior:** Workflows only run if target is listed
- **Note:** iOS apps use `publish-to: []` as signed Xcode artifacts publish via App Store Connect automatically; unsigned archive-only builds skip App Store upload

#### `sboms`

- **Type:** `string` (enum / comma-list)
- **Description:** Which CISA SBOM types to generate for this artifact. See [docs/sbom.md](sbom.md) for the full taxonomy.
- **Accepted values:**
  - `all` — Build + Analyzed-artifact + Analyzed-container (default)
  - `none` — skip SBOM generation entirely
  - `build` — CISA Build SBOM only (cyclonedx plugin during build)
  - `analyzed-artifact` — Syft scan of the built binary only
  - `analyzed-container` — Syft scan of the published container only
  - Any comma-list of the three layer names, e.g. `build,analyzed-artifact`
- **Default:** Automatic based on project type:
  - `all` for: `maven`, `npm`, `gradle`, `gradle-android`, `cargo`, `go`, `python` (`python` is reserved but has no workflow yet)
  - `none` for: `xcode-ios`, `meta`
- **Examples:**
  ```yaml
  # (default — same as omitting the field for a supported project type)
  sboms: all

  # Compliance minimum
  sboms: build

  # Turn off SBOMs for this artifact
  sboms: none
  ```
- **Formats produced:** Build layer: CycloneDX 1.6. Analyzed-artifact and analyzed-container layers: SPDX 2.3 and CycloneDX 1.6.
- **Pipeline cap:** The release orchestrator `release.sboms` input (default `all`) and release-snapshot orchestrator `sboms` input (default `none`) cap aggregate release/snapshot SBOM generation against the pipeline-wide SBOM union. They do not rewrite each artifact's parsed `effective-sboms`.
- **What it controls:** Effective SBOM layers for this artifact. On the orchestrator path it controls build-time Build SBOM execution and container SBOM generation. Direct build workflow calls still default their Build SBOM input to `true` unless explicitly disabled. See [docs/sbom.md](sbom.md) for the full semantics.
- **Note:** `analyzed-*` scans use [Syft](https://github.com/anchore/syft); ecosystem coverage varies. Gradle Android currently produces Build SBOMs and analyzed-container SBOMs, but not APK/AAB analyzed-artifact SBOMs. The `build` layer uses the language-native cyclonedx plugin and is the highest-fidelity type.

---

### Configuration Fields (Maven/Gradle)

#### `config.java-version`

- **Type:** `string` or `number`
- **Description:** JDK version forwarded to publishing/setup steps. The build runtime is selected by the workflow `runtime-image` input.
- **Default:** `25`
- **Valid values:** `8`, `11`, `17`, `21`, `25`
- **Example:** `java-version: 25`

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
- **Description:** Node.js version forwarded to publishing/setup steps. The build runtime is selected by the workflow `runtime-image` input.
- **Default:** `24`
- **Valid values:** `18`, `20`, `22`, `24`
- **Example:** `node-version: 24`

---

### Configuration Fields (Go)

#### `config.build-mode`

- **Type:** `string`
- **Required:** No — omitted defaults to `artifact-first`
- **Valid values:** `artifact-first` (default), `container-first`
- **Description:** Selects whether reusable-ci compiles Go binaries in `build-go.yml` (artifact-first) or lets the project's Containerfile compile them (container-first).

Use `artifact-first` for CLI/tools released as standalone binaries:

```yaml
artifacts:
  - name: my-cli
    project-type: go
    config:
      build-mode: artifact-first
      main-package: ./cmd/my-cli
      binary-name: my-cli
      platforms: linux/amd64,linux/arm64,darwin/amd64,darwin/arm64
```

Use `container-first` for services where the container image is the primary deliverable:

```yaml
artifacts:
  - name: my-service
    project-type: go
    config:
      build-mode: container-first
containers:
  - name: my-service
    from: [my-service]
    container-file: Containerfile
    context: .
    target: runtime
```

When an artifact-first Go binary is copied into a container, that container may
reference one artifact-first Go artifact. Multiple Go binaries should either be
built in the Containerfile (`container-first`) or split across containers.
The downloaded layout inside the container build context is
`dist/<goos>-<goarch>/<binary>-<goos>-<goarch>`, so multi-arch Containerfiles
can copy `dist/${TARGETOS}-${TARGETARCH}/...` using BuildKit's target
arguments.

#### `config.main-package`

- **Type:** `string`
- **Applies to:** Go `artifact-first`
- **Default:** `.`
- **Description:** Go package passed to `go build`.
- **Example:** `main-package: ./cmd/my-cli`

#### `config.binary-name`

- **Type:** `string`
- **Applies to:** Go `artifact-first`
- **Default:** artifact `name`, then Go module basename
- **Description:** Output binary basename.

#### `config.platforms`

- **Type:** comma-separated `GOOS/GOARCH` list
- **Applies to:** Go `artifact-first`
- **Default:** `linux/amd64,linux/arm64,darwin/amd64,darwin/arm64`
- **Description:** Cross-compile targets written as `dist/<goos>-<goarch>/<binary>-<goos>-<goarch>` so release asset basenames are unique.

#### `config.build-tags`, `config.ldflags`, `config.skip-tests`

- **Applies to:** Go `artifact-first`
- **Description:** Optional build tags, additional ldflags, and release-build test suppression. `govulncheck` remains caller-owned in PR/test workflows.

### Configuration Fields (Cargo)

#### `config.build-mode`

- **Type:** `string`
- **Required:** No — omitted defaults to `artifact-first`
- **Valid values:** `artifact-first` (default), `container-first`
- **Description:** Selects whether reusable-ci cross-compiles Rust binaries in `build-cargo.yml` (artifact-first) or lets the project's Containerfile compile them (container-first). Symmetric with Go's `build-mode`.

Use `artifact-first` for CLI/tools released as standalone binaries:

```yaml
artifacts:
  - name: my-cli
    project-type: cargo
    config:
      build-mode: artifact-first
      binary-name: my-cli
      platforms: linux/amd64,linux/arm64
```

Use `container-first` for services where the container image is the primary deliverable. This is also the right choice when your Containerfile already runs `cargo build`:

```yaml
artifacts:
  - name: my-service
    project-type: cargo
    config:
      build-mode: container-first
containers:
  - name: my-service
    from: [my-service]
    container-file: Containerfile
    context: .
    target: runtime
```

When an artifact-first Cargo binary feeds a container, the downloaded layout
inside the build context matches Go's: `dist/<goos>-<goarch>/<binary>-<goos>-<goarch>`.
Multi-arch Containerfiles can copy `dist/${TARGETOS}-${TARGETARCH}/...` the same
way Go containers do.

#### `config.binary-name`

- **Type:** `string`
- **Applies to:** Cargo `artifact-first`
- **Default:** Cargo.toml `[[bin]]` target name, then `[package].name`
- **Description:** Output binary basename. The default is what cargo itself produces under `target/<triple>/release/`.

#### `config.platforms`

- **Type:** comma-separated `GOOS/GOARCH` list
- **Applies to:** Cargo `artifact-first`
- **Default:** `linux/amd64`
- **Description:** Cross-compile targets. Each value is mapped to a Rust target triple at build time (`linux/amd64 → x86_64-unknown-linux-gnu`, `linux/arm64 → aarch64-unknown-linux-gnu`, etc.). The runtime image installs the targets and cross-linkers it supports; `linux/amd64` + `linux/arm64` are validated for the default `runtime-rust-stable` image. Other targets require either a runtime image extension or a host with the matching cross-linker.

#### `config.skip-tests`

- **Applies to:** Cargo `artifact-first`
- **Description:** Skip `cargo test --locked --all-targets` during the release build. PR/test workflows remain the canonical home for test execution.

### Configuration Fields (Gradle)

#### `config.gradle-tasks`

- **Type:** `string`
- **Description:** Gradle tasks to execute.
  - **`gradle` (JVM):** default is `assemble`; set to override.
  - **`gradle-android`:** **not honored** on the orchestrator path. The orchestrator (`release-build-stage.yml`) derives tasks from `product-flavor` + `build-types` + `include-aab` and ignores this field. Configure those instead. The override input still exists on `build-gradle-android.yml` for direct callers (e.g., a hand-rolled `release-snapshot-workflow.yml`).
- **Default:** `assemble` (JVM); derived (Android, ignored)
- **Example (JVM):** `gradle-tasks: build test`

#### `config.gradle-version-file`

- **Type:** `string`
- **Description:** File containing version properties
- **Default:** `gradle.properties`
- **Example:** `gradle-version-file: gradle.properties`

---

### Configuration Fields (Gradle Android)

#### `config.build-module`

- **Type:** `string`
- **Description:** Gradle module to build (the application module).
- **Required:** Yes (for `project-type: gradle-android`)
- **Example:** `build-module: app`

#### `config.product-flavor`

- **Type:** `string`
- **Description:** Product flavor for the build. Combined with `build-types` and `include-aab` to derive the gradle task list (e.g. `demo` + `release` + AAB → `assembleDemoRelease app:bundleDemoRelease`).
- **Default:** `""` (no flavor)
- **Example:** `product-flavor: demo`

#### `config.build-types`

- **Type:** `string`
- **Description:** Comma-separated build types to produce.
- **Default:** `debug,release`
- **Example:** `build-types: release`

#### `config.include-aab`

- **Type:** `boolean`
- **Description:** Also build the Android App Bundle (AAB) for the release build type. Required for Google Play publishing.
- **Default:** `true`
- **Example:** `include-aab: true`

#### `config.artifact-name-prefix`

- **Type:** `string`
- **Description:** Prefix for derived artifact names when calling `build-gradle-android.yml` directly. Ignored on the orchestrator path — the orchestrator forwards the artifact's `name:` as the upload identifier so it lines up with what `release-publish-stage.yml` hands to `publish-google-play.yml`.
- **Default:** `""`
- **Example:** `artifact-name-prefix: dev`

---

### Configuration Fields (Xcode iOS/macOS)

#### `config.xcode-version`

- **Type:** `string`
- **Description:** Xcode version to use for building
- **Required:** Yes
- **Valid values:** `15.4`, `16.0`, `16.1`, etc.
- **Example:** `xcode-version: "16.1"`

#### `config.scheme`

- **Type:** `string`
- **Description:** Xcode scheme to build
- **Required:** Yes
- **Example:** `scheme: "Wallet Demo"`

#### `config.workspace`

- **Type:** `string`
- **Description:** Xcode workspace file (mutually exclusive with `project`)
- **Required:** One of `workspace` or `project`
- **Example:** `workspace: "MyApp.xcworkspace"`

#### `config.project`

- **Type:** `string`
- **Description:** Xcode project file (mutually exclusive with `workspace`)
- **Required:** One of `workspace` or `project`
- **Example:** `project: "MyApp.xcodeproj"`

#### `config.configuration`

- **Type:** `string`
- **Description:** Build configuration
- **Default:** `Release`
- **Valid values:** `Debug`, `Release`, or custom configurations
- **Example:** `configuration: Release`

#### `config.use-xcodegen`

- **Type:** `boolean`
- **Description:** Run XcodeGen before version extraction and archive
- **Default:** `false`
- **Example:** `use-xcodegen: true`

#### `config.xcodegen-spec`

- **Type:** `string`
- **Description:** Path to the XcodeGen spec file, relative to `working-directory`
- **Default:** `project.yml`
- **Example:** `xcodegen-spec: project.yml`
- **Note:** Keep `project` or `workspace` set so later steps use a deterministic build target after generation

#### `config.enable-code-signing`

- **Type:** `boolean`
- **Description:** Enable iOS/macOS code signing and IPA export. When false, the build uploads an archive and release publishing skips App Store Connect upload.
- **Default:** `true`
- **Example:** `enable-code-signing: true`
- **Requires secrets:**
  - `IOS_SIGNING_CERTIFICATE_BASE64` - Base64-encoded .p12 certificate
  - `IOS_SIGNING_CERTIFICATE_PASSPHRASE` - Certificate password
  - `PROVISIONING_PROFILE_BASE64` - Base64-encoded provisioning profile
  - `KEYCHAIN_PASSWORD` - Temporary keychain password

#### `config.export-options-var`

- **Type:** `string`
- **Description:** Name of GitHub variable containing base64-encoded exportOptions.plist
- **Default:** `EXPORT_OPTIONS_BASE64`
- **Example:** `export-options-var: EXPORT_OPTIONS_BASE64`
- **Note:** Variable should contain base64-encoded exportOptions.plist for IPA export

#### `config.macos-version`

- **Type:** `string`
- **Description:** macOS runner version
- **Default:** `macos-26`
- **Valid values:** `macos-15`, `macos-26`
- **Example:** `macos-version: macos-26`

#### `config.destination`

- **Type:** `string`
- **Description:** Build destination for xcodebuild
- **Default:** `generic/platform=iOS`
- **Example:** `destination: generic/platform=macOS` (for macOS apps)

#### `config.submit-for-review`

- **Type:** `boolean`
- **Description:** Records App Store review intent in the upload summary. Current workflows upload to App Store Connect/TestFlight; review submission is manual in App Store Connect.
- **Default:** `false`
- **Example:** `submit-for-review: false` (upload only)
- **Example:** `submit-for-review: true` (record that review submission is intended)
- **Note:** Automatic App Store review submission is not implemented today

#### `config.skip-validation`

- **Type:** `boolean`
- **Description:** Skip IPA validation before upload to App Store Connect
- **Default:** `false`
- **Example:** `skip-validation: false` (Recommended - validates before upload)
- **Note:** Only set to `true` if validation fails incorrectly

---

### Configuration Fields (Gradle Android - Google Play)

#### `config.enable-android-signing`

- **Type:** `boolean`
- **Description:** Enable Android app signing for release builds
- **Default:** `false`
- **Example:** `enable-android-signing: true`
- **Requires secrets:**
  - `ANDROID_KEYSTORE` - Base64-encoded keystore file
  - `ANDROID_KEYSTORE_PASSWORD` - Keystore password
  - `ANDROID_KEY_ALIAS` - Key alias
  - `ANDROID_KEY_PASSWORD` - Key password
- **Note:** The workflow maps `ANDROID_KEYSTORE` to the internal
  `ANDROID_KEYSTORE_BASE64` environment variable used by the `reusable-ci` CLI.

#### `config.package-name`

- **Type:** `string`
- **Description:** Android package name (application ID)
- **Required:** Yes (for Google Play publishing)
- **Example:** `package-name: com.example.myapp`

#### `config.google-play-track`

- **Type:** `string`
- **Description:** Google Play release track
- **Default:** `internal`
- **Valid values:** `internal`, `alpha`, `beta`, `production`
- **Example:** `google-play-track: internal`

#### `config.google-play-status`

- **Type:** `string`
- **Description:** Release status on Google Play
- **Default:** `completed`
- **Valid values:** `completed`, `inProgress`, `halted`, `draft`
- **Example:** `google-play-status: completed`
- **Note:** Use `inProgress` with `user-fraction` for staged rollouts

#### `config.google-play-user-fraction`

- **Type:** `string`
- **Description:** Staged rollout percentage (0.0-1.0)
- **Default:** Empty (full rollout)
- **Example:** `google-play-user-fraction: "0.1"` (10% rollout)
- **Note:** Only applies when `google-play-status: inProgress`

#### `config.google-play-update-priority`

- **Type:** `string`
- **Description:** In-app update priority level
- **Default:** `"0"`
- **Valid values:** `"0"` to `"5"` (5 is highest)
- **Example:** `google-play-update-priority: "3"`

#### `config.google-play-release-name`

- **Type:** `string`
- **Description:** Custom release name (defaults to version from AAB)
- **Default:** Empty (auto-generated)
- **Example:** `google-play-release-name: "Summer Update"`

#### `config.google-play-changes-not-sent-for-review`

- **Type:** `boolean`
- **Description:** Hold changes for manual review in Play Console
- **Default:** `false`
- **Example:** `google-play-changes-not-sent-for-review: true`

#### `config.whats-new-directory`

- **Type:** `string`
- **Description:** Directory containing localized release notes
- **Default:** Empty (no release notes)
- **Example:** `whats-new-directory: distribution/whatsnew`
- **Format:** Files named `whatsnew-<LOCALE>` (e.g., `whatsnew-en-US`, `whatsnew-sv-SE`)

#### `config.mapping-file`

- **Type:** `string`
- **Description:** Path to ProGuard/R8 mapping.txt file
- **Default:** Empty
- **Example:** `mapping-file: app/build/outputs/mapping/release/mapping.txt`
- **Use case:** De-obfuscate crash reports in Play Console

#### `config.debug-symbols`

- **Type:** `string`
- **Description:** Path to native debug symbols (zip or folder)
- **Default:** Empty
- **Example:** `debug-symbols: app/build/intermediates/merged_native_libs/release/out/lib`
- **Use case:** Symbolicate native crashes in Play Console

---

## Containers Section

Containers reference artifacts via the `from:` field and are built after all artifacts complete.

### Container Required Fields

#### Container `name`

- **Type:** `string`
- **Description:** Container image name (becomes part of image tag)
- **Example:** `backend-api`, `frontend-ui`
- **Resulting image:** `ghcr.io/org/repo/backend-api:v1.0.0`
- **Single-container collapse:** When `name` equals the repo's short name (the common single-container pattern), the redundant `<repo>/<repo>` subpath is collapsed to `ghcr.io/org/repo:<tag>`. Multi-container layouts where each `name` is distinct from the repo are unaffected.

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
- **Description:** Target CPU architectures. Each platform builds natively on a runner of that architecture (`linux/amd64` → `ubuntu-24.04`, `linux/arm64` → `ubuntu-24.04-arm`). No QEMU. Multi-platform inputs split across runners and merge into a single manifest list.
- **Default:** `linux/amd64`
- **Example:** `platforms: linux/amd64,linux/arm64`
- **Performance:** Multi-platform runs in parallel; wall-clock is dominated by the slowest arch leg (≈1.1× single-arch, plus the merge job).

#### `enable-slsa`

- **Type:** `boolean`
- **Description:** Generate SLSA build-provenance attestation for this container. The per-container value propagates through the typed plan to `publish-container.yml`; set to `false` to opt this specific image out of provenance.
- **Default:** `true`
- **Requires:** release artifact signing enabled and `sign.method: sigstore` or `sign.method: kms`; production planning rejects enabled pushed provenance when signing is disabled or uses GPG. Keyless Sigstore also requires `id-token: write`; the orchestrator supplies workflow permissions.
- **Example:** `enable-slsa: true`

#### Container `enable-sbom` (removed in v3)

The v2.x `enable-sbom: bool` field on the container block is removed in v3 and strict parsing rejects it. Container scanning is now derived from each source artifact's `sboms` field — the container is scanned if any source artifact has `analyzed-container` in its effective sboms (the default for buildable types). To skip the scan, exclude `analyzed-container` from the source artifact's `sboms` (e.g. `sboms: build,analyzed-artifact`).

#### `enable-scan`

- **Type:** `boolean`
- **Description:** Run Trivy vulnerability scan AND gate the publish on its findings. When `true` (default), any finding at the configured `scan-severity` or above fails the publish — the SARIF + GitLab reports are still produced so the rejection is visible in Code Scanning. Set to `false` to skip both the scan and the gate.
- **Default:** `true`
- **Requires:** `CODE_SCANNING_TOKEN` org secret for results to appear in Code Scanning
- **Example:** `enable-scan: true`

#### `scan-severity`

- **Type:** `string` (comma-separated trivy severities)
- **Description:** Trivy `--severity` filter AND fail-on threshold for `enable-scan`. Any finding at this severity or above fails the publish. Narrow the filter to relax the gate (e.g. `CRITICAL` only). To skip the gate entirely use `enable-scan: false` rather than emptying this field.
- **Default:** `"CRITICAL,HIGH"`
- **Example:** `scan-severity: "CRITICAL"`

#### `target`

- **Type:** `string`
- **Description:** Containerfile stage to build for the runtime image. Useful for multi-stage Containerfiles where the deployable image is not the last stage.
- **Default:** empty (builds the last stage; current `docker build` behavior)
- **Example:** `target: runtime`
- **Used by:** container-first ecosystems primarily, but the field is generic — any multi-stage Containerfile may set it.
- **See also:** [artifact-first vs container-first framing](ecosystems.md)

#### `extract.binary`

Opt-in extraction of compiled binaries as CI artifacts. Used by container-first ecosystems (`cargo`, `go`) where the binary is a byproduct of the container build. The same Containerfile is built a second time with `--target` set to the extraction stage; each platform leg uploads `${container.name}-binaries-${arch}`.

The extraction shares cache with the runtime image build (same buildah layer cache, same `--mount=type=cache` IDs), so it does not double-compile.

##### `extract.binary.target`

- **Type:** `string`
- **Description:** Containerfile stage that exposes the binaries. Typically `FROM scratch AS export-binary` with `COPY --from=builder ...` lines.
- **Required when** `extract.binary` is set.
- **Example:** `target: export-binary`

##### `extract.binary.names`

- **Type:** list of `string`
- **Description:** Expected binary file basenames in the extracted output. Informational — surfaced in the GitHub Actions step summary and used downstream for naming. Not enforced; if the names don't match the export-binary stage's COPYs, no error is raised.
- **Example:** `names: [my-service, my-service-cli]`

##### Full example

```yaml
containers:
  - name: my-service
    from: [my-service]
    container-file: my-service/Containerfile
    context: .
    target: runtime
    platforms: linux/amd64,linux/arm64
    extract:
      binary:
        target: export-binary
        names: [my-service, my-service-cli]
```

##### Output

- GHA artifacts `${container.name}-binaries-${arch}` for each platform leg (for example, `my-service-binaries-amd64` and `my-service-binaries-arm64`).
- Automatically included in the GitHub Release when extraction produced files; callers can still add their own `release.attachartifacts` globs for other assets.

#### `build-args`

- **Type:** key/value object
- **Description:** Build arguments for the Containerfile (e.g., `RUST_VERSION`, `DEBIAN_VARIANT`). The parser converts the object to `KEY=VALUE` lines for `reusable-ci container build` (buildah `--build-arg`), so callers can use the more readable object form in `artifacts.yml`.
- **Example (in artifacts.yml):**

  ```yaml
  containers:
    - name: my-svc
      from: [my-svc]
      container-file: Containerfile
      context: .
      build-args:
        RUST_VERSION: "1.94"
        DEBIAN_VARIANT: bookworm-slim
  ```

- **Direct caller equivalent** (when invoking `publish-container.yml` directly, bypassing the orchestrator):

  ```yaml
  uses: diggsweden/reusable-ci/.github/workflows/publish-container.yml@v3.0.0
  with:
    reusable-ci-binary-ref: v3.0.0
    artifact-types: cargo
    build-args: |
      RUST_VERSION=1.94
      DEBIAN_VARIANT=bookworm-slim
  ```

#### `build-secrets`

- **Type:** array of strings (each matches `^[A-Z_][A-Z0-9_]*$`)
- **Description:** Names of GitHub Actions secrets to forward into the Containerfile build as `RUN --mount=type=secret` mounts. Use this for any secret needed at build time (private registry tokens, build-time API keys). Unlike `build-args` — whose values surface verbatim in the published image's config / build records (`docker history`, any registry inspect) — build-secret values are mounted on tmpfs into the consuming `RUN` step and **never recorded in the image, workflow logs, or step inputs**.
- **How it works:** Each name listed here must appear as a key in
  the `REUSABLE_CI_BUILD_SECRETS_JSON` envelope secret (see the
  recipe below). At build time, `reusable-ci container
  materialize-build-secrets` unpacks the envelope into mode-0600
  tmpfiles under `$RUNNER_TEMP/build-secrets/` and emits the
  `id=NAME,src=PATH` lines `reusable-ci container build` passes to
  buildah's `--secret`.
  Reserved names — those reusable-ci already uses for its own
  publish flows (`RELEASE_GPG_PRIVATE_KEY`, `MAVEN_CENTRAL_PASSWORD`,
  etc.) — are rejected at config-parse time to prevent accidental
  shadowing.
- **artifacts.yml:**

  ```yaml
  containers:
    - name: my-svc
      from: [my-svc]
      container-file: Containerfile
      context: .
      build-secrets:
        - PRIVATE_REGISTRY_TOKEN
        - BUILD_TIME_API_KEY
  ```

- **Containerfile:** declare each secret on the `RUN` line that consumes it. The `id=` value is the **lowercased** name; `target=` is the file path inside the mount namespace (a conventional value, the file disappears once the `RUN` step exits).

  ```dockerfile
  # syntax=docker/dockerfile:1
  RUN --mount=type=secret,id=private_registry_token,target=/run/secrets/token \
      curl -H "Authorization: Bearer $(cat /run/secrets/token)" \
           https://internal.example/private/dep.tar.gz -o /tmp/dep.tar.gz
  ```

- **Caller workflow:** the adopter's top-level caller forwards the envelope as the `REUSABLE_CI_BUILD_SECRETS_JSON` secret. The value must stay inside the `secrets:` context end-to-end — step outputs, job outputs, and `with:` inputs are **not** secret-scoped (their values are queryable through the workflow-run API even when GHA masks them in log text). Two correct shapes, picked by how much scoping you want:

  **Option A — single pre-packed org secret (recommended).** Create one GitHub secret named `REUSABLE_CI_BUILD_SECRETS_JSON` at the repo or org level whose value is the compact JSON object you'd otherwise build at runtime, e.g.:

  ```json
  {"PRIVATE_REGISTRY_TOKEN":"<value>","BUILD_TIME_API_KEY":"<value>"}
  ```

  Then forward it untouched:

  ```yaml
  jobs:
    release:
      uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v3.0.0
      secrets:
        # ... your usual secret forwards ...
        REUSABLE_CI_BUILD_SECRETS_JSON: ${{ secrets.REUSABLE_CI_BUILD_SECRETS_JSON }}
  ```

  Pros: the value never enters a non-secret channel; rotation is one secret update; the envelope ships exactly the keys you declared.

  **Option B — `toJSON(secrets)` shortcut.** If every name in `containers[].build-secrets` matches a top-level secret name exactly, forward the whole context:

  ```yaml
  secrets:
    REUSABLE_CI_BUILD_SECRETS_JSON: ${{ toJSON(secrets) }}
  ```

  The materialize step ignores envelope keys it didn't declare, so unrelated org secrets riding along are decoded into memory and dropped without touching disk. The value still stays in the secrets channel. Trade-off: the envelope JSON is larger than it needs to be, and the auto-`GITHUB_TOKEN` rides along.

  Avoid: packing the envelope at runtime via a `jq` step into `$GITHUB_OUTPUT`, then forwarding `needs.pack.outputs.envelope`. Step / job outputs are not a secrets channel, and the value leaves the masked path the moment you write it there.

---

## Publishing Targets

### `forge-packages`

- **Description:** GitHub Packages registry
- **Requirements:** `GITHUB_TOKEN` (automatic)
- **Applies to:** Maven libraries and NPM packages. Maven apps and Gradle publishing are not wired today; use a project-owned publishing workflow until reusable-ci adds those publishers.
- **Registry:** `ghcr.io` (containers), `npm.pkg.github.com` (NPM)

### `maven-central`

- **Description:** Maven Central (Sonatype Central Portal)
- **Requirements:**
  - `MAVEN_CENTRAL_USERNAME` secret
  - `MAVEN_CENTRAL_PASSWORD` secret
  - `build-type: library` (required)
- **Applies to:** Maven only
- **Note:** Requires Sonatype account and approved groupId

### `npmjs`

- **Description:** Public npmjs.org registry (reserved for future production support)
- **Requirements:** Not currently wired into production release publishing; current config validation rejects this target
- **Applies to:** NPM only
- **Note:** Current NPM package publishing uses `forge-packages` and
  `npm.pkg.github.com`.

### `google-play`

- **Description:** Google Play Store
- **Requirements:**
  - `GOOGLE_PLAY_SERVICE_ACCOUNT_JSON` secret (service account JSON key)
  - `ANDROID_KEYSTORE`, `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD` secrets (for signing)
  - Signed release AAB setup. `config.enable-android-signing: true` is normally required by Google Play, but reusable-ci does not currently validate this coupling.
  - `config.package-name` specified
- **Applies to:** Gradle Android only
- **Note:** App must already exist in Google Play Console (upload first AAB manually)

---

## Quick Start Examples

### Single Artifact (Maven)

**`.reusable-ci/artifacts.yml`**
```yaml
artifacts:
  - name: my-app
    project-type: maven
    working-directory: .
    build-type: application
    config:
      java-version: 25
```

**`.github/workflows/release-workflow.yml`**
```yaml
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v3.0.0
    with:
      reusable-ci-binary-ref: v3.0.0
      artifacts-config: .reusable-ci/artifacts.yml
      release-publisher: github-cli
```

### Single Artifact with Container (Maven)

**`.reusable-ci/artifacts.yml`**
```yaml
artifacts:
  - name: my-app
    project-type: maven
    working-directory: .
    build-type: application
    config:
      java-version: 25

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
      - forge-packages
      - maven-central
    config:
      java-version: 25
      settings-path: .mvn/settings.xml
```

### NPM Application

```yaml
artifacts:
  - name: my-ui
    project-type: npm
    working-directory: frontend
    config:
      node-version: 24
```

### Gradle Android App

```yaml
artifacts:
  - name: my-android-app
    project-type: gradle-android
    working-directory: .
    config:
      build-module: app
      product-flavor: demo
      build-types: release
      gradle-version-file: gradle.properties
```

### Gradle Android App with Google Play Publishing

```yaml
artifacts:
  - name: my-android-app
    project-type: gradle-android
    working-directory: .
    publish-to:
      - google-play
    config:
      build-module: app
      product-flavor: demo
      build-types: release
      gradle-version-file: gradle.properties
      enable-android-signing: true
      # Google Play configuration
      package-name: com.example.myapp
      google-play-track: internal
      google-play-status: completed
```

**Required Secrets:**
```text
ANDROID_KEYSTORE
ANDROID_KEYSTORE_PASSWORD
ANDROID_KEY_ALIAS
ANDROID_KEY_PASSWORD
GOOGLE_PLAY_SERVICE_ACCOUNT_JSON
```

### iOS/macOS App (Xcode)

```yaml
artifacts:
  - name: my-ios-app
    project-type: xcode-ios
    working-directory: .
    publish-to: []  # iOS apps don't publish to package registries
    config:
      xcode-version: "16.1"
      scheme: "MyApp"
      use-xcodegen: true
      xcodegen-spec: "project.yml"
      project: "MyApp.xcodeproj"
      configuration: Release
      enable-code-signing: true
      export-options-var: EXPORT_OPTIONS_BASE64
      macos-version: macos-26
```

**Required Secrets:**
```text
IOS_SIGNING_CERTIFICATE_BASE64
IOS_SIGNING_CERTIFICATE_PASSPHRASE
PROVISIONING_PROFILE_BASE64
KEYCHAIN_PASSWORD
APP_STORE_CONNECT_ISSUER_ID
APP_STORE_CONNECT_API_KEY_ID
APP_STORE_CONNECT_API_PRIVATE_KEY_BASE64
```

**Required Variables:**
```text
EXPORT_OPTIONS_BASE64
```

**Encoding certificates/profiles to base64:**
```bash
# Certificate
base64 -i certificate.p12 -o certificate.txt

# Provisioning Profile
base64 -i profile.mobileprovision -o profile.txt

# Export Options
base64 -i exportOptions.plist -o exportOptions.txt
```

### iOS/macOS App With XcodeGen

```yaml
artifacts:
  - name: my-ios-app
    project-type: xcode-ios
    working-directory: .
    publish-to: []
    config:
      xcode-version: "16.1"
      scheme: "MyApp"
      use-xcodegen: true
      xcodegen-spec: "project.yml"
      project: "MyApp.xcodeproj"
      configuration: Release
      enable-code-signing: true
      export-options-var: EXPORT_OPTIONS_BASE64
      macos-version: macos-26
```

Use `project` or `workspace` alongside XcodeGen so version detection and archive steps target the generated Xcode project explicitly.

### Multiple iOS Schemes (Demo, Production)

```yaml
artifacts:
  - name: wallet-ios-demo
    project-type: xcode-ios
    working-directory: .
    config:
      xcode-version: "16.1"
      scheme: "Wallet Demo"
      project: "Wallet.xcodeproj"
      configuration: Release
      submit-for-review: false  # Upload only for demo builds

  - name: wallet-ios-production
    project-type: xcode-ios
    working-directory: .
    config:
      xcode-version: "16.1"
      scheme: "Wallet Production"
      project: "Wallet.xcodeproj"
      configuration: Release
      submit-for-review: true  # Record production review intent in the summary
```

### iOS App Store Submission Options

```yaml
artifacts:
  - name: my-ios-app
    project-type: xcode-ios
    working-directory: .
    config:
      xcode-version: "16.1"
      scheme: "MyApp"
      project: "MyApp.xcodeproj"

      # App Store Connect upload options
      submit-for-review: true   # Summary intent only; submit review manually in App Store Connect
      skip-validation: false    # Validate IPA before upload (recommended)
```

---

## Monorepo Configuration

Build multiple artifacts from a single repository.

### Separate Containers (One Artifact → One Container)

**`.reusable-ci/artifacts.yml`**
```yaml
artifacts:
  - name: backend
    project-type: maven
    working-directory: java-backend
    build-type: application
    config:
      java-version: 25

  - name: frontend
    project-type: npm
    working-directory: frontend
    config:
      node-version: 24

containers:
  - name: backend
    from: [backend]
    container-file: java-backend/Containerfile
    context: .

  - name: frontend
    from: [frontend]
    container-file: frontend/Containerfile
    context: .
```

### Combined Container (Multiple Artifacts → One Container)

**`.reusable-ci/artifacts.yml`**
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
COPY target/*.jar app.jar
COPY dist/ /app/static/
CMD ["java", "-jar", "app.jar"]
```

### Workflow Configuration

**`.github/workflows/release-workflow.yml`**
```yaml
name: Release Workflow

on:
  push:
    tags:
      - "release-request/v*"

permissions:
  contents: read

jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v3.0.0
    permissions:
      contents: write
      packages: write
      id-token: write
      actions: read
      attestations: write
    secrets: inherit
    with:
      reusable-ci-binary-ref: v3.0.0
      artifacts-config: .reusable-ci/artifacts.yml
      changelog-creator: git-cliff
      release-publisher: github-cli
```

### Monorepo Limitations

- **Unified versioning**: All artifacts share the same version (from git tag)
- **Single changelog**: One changelog for the entire repository
- **No change detection**: All artifacts build on every release
- **Serialized release preparation**: Multi-artifact version bumps run one at a
  time against the release branch. After every bump succeeds, one separate job
  creates the final release tag once at the resulting branch HEAD without force.

## Complete Working Examples

For complete working examples, see the [`examples/`](../examples/) directory:

- **Maven Application**: [`examples/maven-app/`](../examples/maven-app/)
- **NPM Application**: [`examples/npm-app/`](../examples/npm-app/)
- **Gradle JVM Library**: [`examples/gradle-app/`](../examples/gradle-app/)
- **Android Application**: [`examples/android-app/`](../examples/android-app/)
- **Monorepo**: [`examples/monorepo/`](../examples/monorepo/)

---
