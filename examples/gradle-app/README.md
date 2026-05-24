<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Gradle JVM Build Example

Gradle JVM build example. For Android applications, see [examples/android-app](../android-app/) — that path uses `project-type: gradle-android` and routes to `build-gradle-android.yml`, which understands product flavors, AABs, and Google Play publishing.

## Project Structure

```text
my-gradle-lib/
├── src/
├── build.gradle(.kts)
├── gradle.properties
├── settings.gradle(.kts)
└── .github/
    ├── artifacts.yml
    └── workflows/
        ├── pullrequest-workflow.yml
        └── release-workflow.yml
```

## Configuration Files

### `.reusable-ci/artifacts.yml`

See [artifacts.yml](artifacts.yml) in this directory.

Key points:
- `project-type: gradle` → routes to `build-gradle-app.yml` (JVM-only; no Android SDK, no APK/AAB).
- `config.gradle-tasks: build` → runs the project's normal Gradle build.
- Gradle publishing is not wired to reusable-ci's Maven Central publisher today. If you need Gradle publishing, run your own Gradle publishing task in a project-owned workflow until reusable-ci has a Gradle-aware publisher.

### `.github/workflows/release-workflow.yml`

See [release-workflow.yml](release-workflow.yml) in this directory. Uses the release orchestrator with `artifacts-config`.

## How to Use

1. **Copy files to your repository:**
   ```bash
   mkdir -p .github/workflows
   cp examples/gradle-app/artifacts.yml .github/
   cp examples/gradle-app/release-workflow.yml .github/workflows/
   cp examples/gradle-app/pullrequest-workflow.yml .github/workflows/
   ```

2. **Customize for your project:**
   - Update `name` in `artifacts.yml`.
   - Adjust `gradle-tasks` if your build task differs.
   - Set `version=` in `gradle.properties` (the workflow reads this for the build summary).
   - **Set `preserveFileTimestamps = false` AND `reproducibleFileOrder = true` on every `AbstractArchiveTask`** — required for reproducible archives; `validate jvm-reproducibility` fails the release if either is missing. See [Reproducible Builds](../../docs/verification.md#reproducible-builds) for the exact snippet.

3. **Create first release:**
   ```bash
   git tag -s v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

## What Gets Built

- JAR artifacts from `build/libs/` (root and any subprojects) uploaded as `gradle-build-artifacts`.
- Build SBOM (CycloneDX) uploaded as `gradle-build-sbom`, produced by the `cyclonedx-gradle-plugin` via a throwaway init-script (no changes required to the consumer's `build.gradle`).

## Version Management

The workflow reads `version=` from `gradle.properties` for the build summary:

```properties
# gradle.properties
version=1.0.0
```

Version bump on release is handled by the release orchestrator's prepare stage, independent of this workflow.

## See Also

- [Artifacts Reference](../../docs/artifacts-reference.md)
- [Publishing Guide](../../docs/publishing.md)
- [SBOM Generation](../../docs/sbom.md)
