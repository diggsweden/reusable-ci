<!--
SPDX-FileCopyrightText: 2025 The Reusable CI Authors

SPDX-License-Identifier: CC0-1.0
-->

# Android App Example

Example configuration for Android applications with multiple product flavors and build types.

## Use Case

Android applications that need to:
- Build multiple variants (debug, release)
- Generate APK and AAB files
- Use custom artifact naming
- Support product flavors (demo, prod, staging, etc.)
- Sign release builds

## Files

### `build-workflow.yml`
Development build workflow that creates Android artifacts on push to main.

### `artifacts.yml`
Artifact configuration for release builds (used with release-workflow.yml).

### `release-workflow.yml`
Production release workflow triggered by version tags.

## Features

### Multiple Build Types
- **Debug APK**: Development builds with debugging enabled
- **Release APK**: Optimized production builds
- **AAB (Android App Bundle)**: For Google Play Store distribution

### Custom Artifact Naming
Artifacts are named with date stamps and custom prefixes:
```text
2025-01-15 - testing_store - my-app - demo - APK debug
2025-01-15 - testing_store - my-app - demo - APK release
2025-01-15 - testing_store - my-app - demo - AAB release
```

### Product Flavors
Support for multiple flavors:
- `demo` - Demo/testing version
- `prod` - Production version
- `staging` - Staging environment
- Custom flavors as needed

### JDK Distribution Options
Choose your preferred JDK:
- `temurin` (default)
- `zulu`
- `adopt`
- `corretto`

## Configuration

### Basic Setup

```yaml
jobs:
  build:
    uses: diggsweden/reusable-ci/.github/workflows/build-gradle-android.yml@main
    with:
      java-version: "25"
      build-module: "app"
      product-flavor: "demo"
```

### With Signing

```yaml
jobs:
  build:
    uses: diggsweden/reusable-ci/.github/workflows/build-gradle-android.yml@main
    secrets: inherit  # Required for signing secrets
    with:
      java-version: "25"
      build-module: "app"
      product-flavor: "prod"
      enable-signing: true
```

**Required Secrets:**
- `ANDROID_KEYSTORE` - Base64-encoded keystore file
- `ANDROID_KEYSTORE_PASSWORD` - Keystore password
- `ANDROID_KEY_ALIAS` - Key alias
- `ANDROID_KEY_PASSWORD` - Key password

### Release Only (No Debug)

```yaml
jobs:
  build:
    uses: diggsweden/reusable-ci/.github/workflows/build-gradle-android.yml@main
    with:
      java-version: "25"
      build-module: "app"
      build-types: "release"  # Only release builds
      include-aab: true
      enable-signing: true
```

### Custom Naming

```yaml
jobs:
  build:
    uses: diggsweden/reusable-ci/.github/workflows/build-gradle-android.yml@main
    with:
      java-version: "25"
      build-module: "app"
      artifact-name-prefix: "production_store"  # Custom prefix
      include-date-stamp: true  # Include date in name
```

## Multi-Flavor Builds

Build multiple flavors in parallel:

```yaml
jobs:
  build-demo:
    uses: diggsweden/reusable-ci/.github/workflows/build-gradle-android.yml@main
    with:
      product-flavor: "demo"
      artifact-name-prefix: "demo_store"

  build-prod:
    uses: diggsweden/reusable-ci/.github/workflows/build-gradle-android.yml@main
    with:
      product-flavor: "prod"
      artifact-name-prefix: "production_store"
      enable-signing: true
```

## Gradle Configuration

Your Android project needs standard Gradle configuration:

### `gradle.properties`
```properties
versionName=1.0.0
versionCode=1
```

### `build.gradle.kts` (app module)
```kotlin
android {
    defaultConfig {
        versionCode = project.property("versionCode").toString().toInt()
        versionName = project.property("versionName").toString()
    }

    flavorDimensions += "version"
    productFlavors {
        create("demo") {
            dimension = "version"
            applicationIdSuffix = ".demo"
        }
        create("prod") {
            dimension = "version"
        }
    }

    signingConfigs {
        create("release") {
            storeFile = file("release.keystore")
            storePassword = System.getenv("ANDROID_KEYSTORE_PASSWORD")
            keyAlias = System.getenv("ANDROID_KEY_ALIAS")
            keyPassword = System.getenv("ANDROID_KEY_PASSWORD")
        }
    }

    buildTypes {
        release {
            signingConfig = signingConfigs.getByName("release")
        }
    }
}
```

## Input Parameters

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `java-version` | Yes | - | Java version (e.g., 17, 25) |
| `build-module` | Yes | - | Gradle module to build |
| `product-flavor` | No | "" | Product flavor (demo, prod, etc.) |
| `build-types` | No | "debug,release" | Comma-separated build types |
| `include-aab` | No | true | Generate AAB files |
| `artifact-name-prefix` | No | "" | Custom artifact name prefix |
| `include-date-stamp` | No | true | Include date in artifact names |
| `jdk-distribution` | No | "temurin" | JDK distribution |
| `enable-signing` | No | false | Enable Android app signing |
| `working-directory` | No | "." | Working directory |
| `skip-tests` | No | false | Skip tests during build |

## Artifacts

The workflow generates separate artifacts for each variant:

**Debug APK:**
- Path: `app/build/outputs/apk/**/debug/*.apk`
- Name: `{date} - {prefix} - {repo} - {flavor} - APK debug`

**Release APK:**
- Path: `app/build/outputs/apk/**/release/*.apk`
- Name: `{date} - {prefix} - {repo} - {flavor} - APK release`

**Release AAB:**
- Path: `app/build/outputs/bundle/**/*.aab`
- Name: `{date} - {prefix} - {repo} - {flavor} - AAB release`

## See Also

- [Android Gradle Plugin Documentation](https://developer.android.com/build)
- [Product Flavors Guide](https://developer.android.com/build/build-variants)
- [App Signing Documentation](https://developer.android.com/studio/publish/app-signing)
