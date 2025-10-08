<!--
SPDX-FileCopyrightText: 2025 The Reusable CI Authors

SPDX-License-Identifier: CC0-1.0
-->

# Gradle Application Example

Gradle/Android application with container build.

## Project Structure

```text
my-gradle-app/
├── app/
│   ├── src/
│   └── build.gradle
├── build.gradle
├── gradle.properties
├── settings.gradle
├── Containerfile
└── .github/
    ├── artifacts.yml
    └── workflows/
        └── release-workflow.yml
```

## Configuration Files

### `.github/artifacts.yml`

See [artifacts.yml](artifacts.yml) in this directory.

Key points:
- Single Gradle artifact
- Android-specific configuration
- Publishes to GitHub Packages
- Builds multi-platform container

### `.github/workflows/release-workflow.yml`

See [release-workflow.yml](release-workflow.yml) in this directory.

## How to Use

1. **Copy files to your repository:**
   ```bash
   mkdir -p .github/workflows
   cp examples/gradle-app/artifacts.yml .github/
   cp examples/gradle-app/release-workflow.yml .github/workflows/
   ```

2. **Customize for your project:**
   - Update `name` in artifacts.yml
   - Adjust `gradle-tasks` if needed
   - Configure version file path
   - Verify `Containerfile` path

3. **Prepare gradle.properties:**
   ```properties
   # gradle.properties
   versionName=1.0.0
   versionCode=1
   ```

4. **Create first release:**
   ```bash
   git tag -s v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

## What Gets Built

- Gradle artifacts → Built with configured tasks
- Published to → GitHub Packages
- Container image → `ghcr.io/org/repo:v1.0.0` (if configured)
- Platforms → `linux/amd64`, `linux/arm64`

## Android-Specific Configuration

For Android apps, use specific Gradle tasks:

```yaml
config:
  gradle-tasks: build assembleDemoRelease bundleDemoRelease
  gradle-version-file: gradle.properties
```

**What happens:**
- `build` - Standard build
- `assembleDemoRelease` - Creates APK
- `bundleDemoRelease` - Creates AAB (Android App Bundle)

**Artifacts created:**
- `app/build/outputs/apk/**/*.apk`
- `app/build/outputs/bundle/**/*.aab`

## Version Management

The workflow auto-increments `versionCode` in `gradle.properties`:

```properties
# Before release:
versionName=0.9.0
versionCode=42

# After v1.0.0 release:
versionName=1.0.0
versionCode=43  # Auto-incremented
```

## See Also

- [Configuration Reference](../../docs/configuration.md)
- [Publishing Guide](../../docs/publishing.md)
- [Troubleshooting](../../docs/troubleshooting.md)
