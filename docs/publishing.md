<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Publishing Guide

Complete guide to publishing artifacts to different registries.

## Overview

The reusable workflows support multiple publishing targets:

| Target | Artifact Types | Authentication |
|--------|---------------|----------------|
| **Maven Central** | Maven libraries | Sonatype credentials |
| **npmjs.org** | NPM packages | NPM token |
| **Container Registries** | Container images | Token/credentials |
| **Apple App Store** | iOS/macOS apps | App Store Connect API |
| **Google Play Store** | Android apps | Service Account JSON |

---

### NPM

```bash
# Configure npm to use GitHub Packages
npm config set @diggsweden:registry https://npm.pkg.github.com
npm config set //npm.pkg.github.com/:_authToken YOUR_GITHUB_TOKEN
```

#### Containers

```bash
# Pull image
podman pull ghcr.io/diggsweden/repo-name:v1.0.0
```

---

## Maven Central

### Maven Central Prerequisites

1. **GPG Key Setup**
   - Already configured at DiggSweden org level

2. **Maven Central Credentials**
   - Already configured at DiggSweden org level

### Configuration

```yaml
# .github/artifacts.yml
artifacts:
  - name: my-library
    project-type: maven
    working-directory: .
    build-type: library              # Required for Maven Central
    require-authorization: true      # Recommended for production libraries
    publish-to:
      - github-packages              # Also publish to GitHub
      - maven-central                # Publish to Maven Central
    config:
      java-version: 25
      settings-path: .mvn/settings.xml  # Optional: custom settings
```

### Project Requirements

Your `pom.xml` must include:

```xml
<project>
  <!-- Required metadata -->
  <groupId>se.digg</groupId>
  <artifactId>my-library</artifactId>
  <version>1.0.0</version>
  <packaging>jar</packaging>

  <name>My Library</name>
  <description>A brief description</description>
  <url>https://github.com/diggsweden/my-library</url>

  <!-- Required license -->
  <licenses>
    <license>
      <name>MIT License</name>
      <url>https://opensource.org/licenses/MIT</url>
    </license>
  </licenses>

  <!-- Required developer info -->
  <developers>
    <developer>
      <name>Your Name</name>
      <email>your.email@digg.se</email>
      <organization>Digg</organization>
      <organizationUrl>https://www.digg.se</organizationUrl>
    </developer>
  </developers>

  <!-- Required SCM info -->
  <scm>
    <connection>scm:git:git://github.com/diggsweden/my-library.git</connection>
    <developerConnection>scm:git:ssh://github.com:diggsweden/my-library.git</developerConnection>
    <url>https://github.com/diggsweden/my-library/tree/main</url>
  </scm>

  <build>
    <plugins>
      <!-- Maven Central Publishing (modern approach) -->
      <plugin>
        <groupId>org.sonatype.central</groupId>
        <artifactId>central-publishing-maven-plugin</artifactId>
        <version>0.8.0</version>
        <extensions>true</extensions>
        <configuration>
          <checksums>all</checksums>
          <skipPublishing>false</skipPublishing>
          <publishingServerId>central</publishingServerId>
        </configuration>
      </plugin>
    </plugins>
  </build>

  <profiles>
    <profile>
      <id>central-release</id>
      <build>
        <plugins>
          <!-- GPG Signing -->
          <plugin>
            <groupId>org.apache.maven.plugins</groupId>
            <artifactId>maven-gpg-plugin</artifactId>
            <version>3.2.8</version>
            <executions>
              <execution>
                <id>sign-artifacts</id>
                <phase>verify</phase>
                <goals>
                  <goal>sign</goal>
                </goals>
              </execution>
            </executions>
          </plugin>

          <!-- Sources JAR -->
          <plugin>
            <groupId>org.apache.maven.plugins</groupId>
            <artifactId>maven-source-plugin</artifactId>
            <version>3.3.1</version>
            <executions>
              <execution>
                <id>attach-sources</id>
                <goals>
                  <goal>jar-no-fork</goal>
                </goals>
              </execution>
            </executions>
          </plugin>

          <!-- Javadoc JAR -->
          <plugin>
            <groupId>org.apache.maven.plugins</groupId>
            <artifactId>maven-javadoc-plugin</artifactId>
            <version>3.11.3</version>
            <executions>
              <execution>
                <id>attach-javadocs</id>
                <goals>
                  <goal>jar</goal>
                </goals>
              </execution>
            </executions>
          </plugin>
        </plugins>
      </build>
    </profile>
  </profiles>
</project>
```

### Maven Settings (Optional)

If you need custom repository configuration:

```xml
<!-- .mvn/settings.xml -->
<settings>
  <servers>
    <server>
      <id>central</id>
      <username>${env.MAVENCENTRAL_USERNAME}</username>
      <password>${env.MAVENCENTRAL_PASSWORD}</password>
    </server>
  </servers>
</settings>
```

Reference it in artifacts.yml:

```yaml
config:
  settings-path: .mvn/settings.xml
```

### Maven Central Release Process

1a. **Tag your release:**

   ```bash
   git tag -s v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

1b. **or, Tag your SNAPSHOT release:**

   ```bash
   git tag -s v1.0.0-SNAPSHOT -m "v1.0.0-SNAPSHOT"
   git push origin v1.0.0
   ```

2. **Workflow automatically:**
   - Builds library with sources and javadoc
   - GPG signs all artifacts
   - Publishes to Maven Central staging
   - Auto-releases after validation

3. **Availability:**
   - Appears on Maven Central within ~10-30 minutes
   - Searchable at <https://central.sonatype.com/>

### Consuming Published Library

#### Released Versions

Users add to their `pom.xml`:

```xml
<dependency>
  <groupId>se.digg</groupId>
  <artifactId>my-library</artifactId>
  <version>1.0.0</version>
</dependency>
```

No additional configuration needed - Maven Central is included by default.

#### Snapshot Versions

To consume `-SNAPSHOT` versions, add snapshot repository to `~/.m2/settings.xml` or project `pom.xml`:

```xml
<!-- ~/.m2/settings.xml -->
<settings>
  <profiles>
    <profile>
      <id>snapshots</id>
      <repositories>
        <repository>
          <id>maven-snapshots</id>
          <url>https://s01.oss.sonatype.org/content/repositories/snapshots/</url>
          <releases>
            <enabled>false</enabled>
          </releases>
          <snapshots>
            <enabled>true</enabled>
            <updatePolicy>always</updatePolicy>
          </snapshots>
        </repository>
      </repositories>
    </profile>
  </profiles>

  <activeProfiles>
    <activeProfile>snapshots</activeProfile>
  </activeProfiles>
</settings>
```

Then use snapshot version in your project:

```xml
<dependency>
  <groupId>se.digg</groupId>
  <artifactId>my-library</artifactId>
  <version>1.0.0-SNAPSHOT</version>
</dependency>
```

**Note:** Snapshots are development versions and may change frequently. Use `updatePolicy>always</updatePolicy>` to always check for latest snapshot.

---

## NPM Registry (npmjs.org)

### NPM Registry Overview

- **Public distribution** - Available to all Node.js developers
- **Scoped packages** - Use `@diggsweden/` prefix
- **No approval needed** - Publish immediately

### NPM Configuration

```yaml
# .github/artifacts.yml
artifacts:
  - name: my-package
    project-type: npm
    working-directory: packages/my-package
    publish-to:
      - github-packages  # Also publish to GitHub
      - npmjs            # Publish to npmjs.org
    config:
      node-version: 24
      npm-tag: latest    # or 'next', 'beta'
```

### Package Requirements

Your `package.json` must include:

```json
{
  "name": "@diggsweden/my-package",
  "version": "1.0.0",
  "description": "A brief description",
  "main": "dist/index.js",
  "types": "dist/index.d.ts",
  "license": "MIT",
  "repository": {
    "type": "git",
    "url": "https://github.com/diggsweden/my-package.git"
  },
  "publishConfig": {
    "access": "public"
  },
  "files": [
    "dist",
    "README.md",
    "LICENSE"
  ]
}
```

### Release Process

1. **Tag your release:**

   ```bash
   git tag -s v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

2. **Workflow automatically:**
   - Builds package (`npm run build`)
   - Publishes to npmjs.org
   - Publishes to GitHub Packages

3. **Availability:**
   - Appears on npmjs.org within ~1 minute
   - Searchable at <https://www.npmjs.com/>

### Consuming Published Package

Users install with:

```bash
npm install @diggsweden/my-package
```

---

## Container Registries

### Supported Registries

| Registry | Default | Authentication |
|----------|---------|----------------|
| `ghcr.io` | ✅ Yes | `GITHUB_TOKEN` (automatic) |
| Custom | No | Custom credentials |

### GitHub Container Registry (ghcr.io)

**Default** - No setup required:

```yaml
containers:
  - name: my-app
    from: [my-app]
    container-file: Containerfile
    # registry defaults to ghcr.io
```

**Image naming:**

```text
ghcr.io/diggsweden/repo-name/container-name:v1.0.0
```

**Namespace security:**
- Images must follow pattern: `ghcr.io/OWNER/REPO_NAME` or `ghcr.io/OWNER/REPO_NAME-*`
- Default owner: `github.repository_owner` (e.g., `diggsweden`)
- Configurable via `enforce-namespace` input
- Prevents pushing to unauthorized namespaces
- Enforced automatically during container build

**Custom namespace (optional):**

```yaml
containers:
  - name: my-app
    from: [my-app]
    container-file: Containerfile
    enforce-namespace: my-custom-org  # Override default
```

**Pull image:**

```bash
podman pull ghcr.io/diggsweden/repo-name/my-app:v1.0.0
```

### Docker Hub

**Requires credentials:**

1. Create Docker Hub account
2. Generate access token
3. Request secrets: `DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`

```yaml
# .github/workflows/release-workflow.yml
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@main
    with:
      artifacts-config: .github/artifacts.yml
      container.registry: docker.io
      container.registry-username: ${{ secrets.DOCKERHUB_USERNAME }}
      container.use-github-token: false
    secrets: inherit
```

**Image naming:**

```text
docker.io/diggsweden/my-app:v1.0.0
```

### Custom Registry

```yaml
# .github/workflows/release-workflow.yml
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@main
    with:
      artifacts-config: .github/artifacts.yml
      container.registry: registry.example.com
      container.registry-username: ${{ secrets.REGISTRY_USERNAME }}
      container.use-github-token: false
    secrets: inherit
```

---

## Apple App Store (TestFlight)

### Apple App Store Overview

- **TestFlight distribution** - Automated beta testing
- **App Store submission** - Optional automatic submission for review
- **API-based uploads** - Uses App Store Connect API v2

### Apple App Store Prerequisites

1. **Apple Developer Account**
   - Enrolled in Apple Developer Program
   - App created in App Store Connect

2. **App Store Connect API Key**
   - Navigate to [App Store Connect > Users and Access > Integrations > App Store Connect API](https://appstoreconnect.apple.com/access/integrations/api)
   - Create a new API key with "App Manager" role
   - Download the `.p8` private key file (only available once!)
   - Note the Key ID and Issuer ID

3. **Code Signing**
   - Distribution certificate (`.p12` file)
   - Provisioning profile for App Store distribution
   - Export options plist configured for App Store

### Enabling/Disabling App Store Publishing

iOS apps with `project-type: xcode-ios` **automatically publish to App Store Connect** when:
1. `enable-code-signing: true` is set
2. The required secrets are configured

**To disable App Store publishing**, set `enable-code-signing: false`:

```yaml
config:
  enable-code-signing: false  # Build only, no IPA export or upload
```

**Note:** iOS apps use `publish-to: []` because they don't publish to package registries like Maven Central or npm. The App Store upload happens automatically based on `enable-code-signing`.

### Apple App Store Configuration

```yaml
# .github/artifacts.yml
artifacts:
  - name: my-ios-app
    project-type: xcode-ios
    working-directory: .
    build-type: application
    publish-to: []  # iOS apps publish via App Store Connect, not package registries
    config:
      xcode-version: "16.1"
      scheme: "MyApp"
      project: "MyApp.xcodeproj"
      configuration: Release
      enable-code-signing: true   # <-- This enables App Store publishing
      export-options-var: EXPORT_OPTIONS_BASE64
      macos-version: macos-26
      # App Store submission options
      submit-for-review: false  # true = submit to App Store, false = TestFlight only
      skip-validation: false    # Validate IPA before upload (recommended)
```

### Required Secrets

```text
# Code Signing
CERTIFICATE_BASE64              # Base64-encoded .p12 distribution certificate
CERTIFICATE_PASSPHRASE          # Certificate password
PROVISIONING_PROFILE_BASE64     # Base64-encoded provisioning profile
KEYCHAIN_PASSWORD               # Temporary keychain password (any value)

# App Store Connect API
APP_STORE_CONNECT_ISSUER_ID           # From App Store Connect API keys page
APP_STORE_CONNECT_API_KEY_ID          # Key ID from App Store Connect
APP_STORE_CONNECT_API_PRIVATE_KEY_BASE64  # Base64-encoded .p8 private key
```

### Required Variables

```text
EXPORT_OPTIONS_BASE64           # Base64-encoded exportOptions.plist
```

### Encoding Files to Base64

```bash
# Certificate (.p12)
base64 -i Certificates.p12 -o certificate.txt

# Provisioning Profile
base64 -i MyApp_Distribution.mobileprovision -o profile.txt

# App Store Connect API Key (.p8)
base64 -i AuthKey_XXXXXXXXXX.p8 -o apikey.txt

# Export Options
base64 -i exportOptions.plist -o exportOptions.txt
```

### Export Options Example

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>method</key>
    <string>app-store-connect</string>
    <key>teamID</key>
    <string>YOUR_TEAM_ID</string>
    <key>uploadSymbols</key>
    <true/>
    <key>destination</key>
    <string>upload</string>
</dict>
</plist>
```

### Apple App Store Release Process

1. **Tag your release:**

   ```bash
   git tag -s v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

2. **Workflow automatically:**
   - Builds iOS app with Xcode
   - Signs with distribution certificate
   - Exports IPA file
   - Validates IPA with Apple
   - Uploads to App Store Connect

3. **Availability:**
   - TestFlight: ~10-15 minutes after upload processing
   - App Store: After manual or automatic review submission

---

## Google Play Store

### Google Play Store Overview

- **Multiple tracks** - internal, alpha, beta, production
- **Staged rollouts** - Gradual release to percentage of users
- **API-based uploads** - Uses Google Play Developer API v3

### Google Play Store Prerequisites

1. **Google Play Developer Account**
   - Enrolled in Google Play Developer Program
   - App created in Google Play Console (must upload first APK/AAB manually)

2. **Service Account Setup**
   - Enable Google Play Android Developer API in [Google Cloud Console](https://console.cloud.google.com/apis/library/androidpublisher.googleapis.com)
   - Create service account in [IAM & Admin > Service accounts](https://console.cloud.google.com/iam-admin/serviceaccounts)
   - Create and download JSON key for the service account
   - In [Google Play Console > Users and permissions](https://play.google.com/console), invite the service account email
   - Grant "Release manager" or appropriate permissions for your app

3. **App Signing**
   - Keystore file for signing release builds
   - Key alias and passwords

### Enabling/Disabling Google Play Publishing

**To enable Google Play publishing**, add `google-play` to the `publish-to` array:

```yaml
publish-to:
  - google-play    # Enable Google Play publishing
```

**To disable Google Play publishing**, remove `google-play` from the array or use an empty array:

```yaml
publish-to: []     # No publishing - only build and attach to GitHub Release
```

### Google Play Store Configuration

```yaml
# .github/artifacts.yml
artifacts:
  - name: my-android-app
    project-type: gradle-android
    working-directory: .
    build-type: application
    publish-to:
      - google-play    # <-- This enables Google Play publishing
    config:
      java-version: 21
      gradle-tasks: build assembleDemoRelease bundleDemoRelease
      build-module: app
      gradle-version-file: gradle.properties
      enable-android-signing: true
      # Google Play configuration (required when publish-to includes google-play)
      package-name: com.example.myapp
      google-play-track: internal          # internal, alpha, beta, production
      google-play-status: completed        # completed, inProgress, halted, draft
      # Optional settings
      google-play-user-fraction: ""        # 0.1 = 10% rollout (only for inProgress)
      google-play-update-priority: "0"     # 0-5 (5 = highest priority)
      google-play-release-name: ""         # Custom release name
      whats-new-directory: ""              # Path to localized release notes
      mapping-file: ""                     # ProGuard mapping.txt path
      debug-symbols: ""                    # Native debug symbols path
```

### Required Secrets

```text
# App Signing
ANDROID_KEYSTORE                # Base64-encoded keystore file
ANDROID_KEYSTORE_PASSWORD       # Keystore password
ANDROID_KEY_ALIAS               # Key alias name
ANDROID_KEY_PASSWORD            # Key password

# Google Play API
GOOGLE_PLAY_SERVICE_ACCOUNT_JSON  # Service account JSON key (plain text, not base64)
```

### Encoding Keystore to Base64

```bash
base64 -i release-keystore.jks -o keystore.txt
```

### Gradle Signing Configuration

Your `app/build.gradle.kts` should read signing config from environment:

```kotlin
android {
    signingConfigs {
        create("release") {
            storeFile = file(System.getenv("ANDROID_KEYSTORE_PATH") ?: "release.keystore")
            storePassword = System.getenv("ANDROID_KEYSTORE_PASSWORD") ?: ""
            keyAlias = System.getenv("ANDROID_KEY_ALIAS") ?: ""
            keyPassword = System.getenv("ANDROID_KEY_PASSWORD") ?: ""
        }
    }

    buildTypes {
        release {
            signingConfig = signingConfigs.getByName("release")
            // ... other config
        }
    }
}
```

### Google Play Track Options

| Track | Description | Review Required |
|-------|-------------|-----------------|
| `internal` | Internal testing (up to 100 testers) | No |
| `alpha` | Closed testing | No |
| `beta` | Open testing | No |
| `production` | Full release | Yes (first time) |

### Staged Rollouts

For gradual releases, use `inProgress` status with `user-fraction`:

```yaml
config:
  google-play-track: production
  google-play-status: inProgress
  google-play-user-fraction: "0.1"  # 10% of users
```

### Localized Release Notes

Create a directory with `whatsnew-<LOCALE>` files:

```text
distribution/
└─ whatsnew/
  ├─ whatsnew-en-US
  ├─ whatsnew-sv-SE
  └─ whatsnew-de-DE
```

Reference in config:

```yaml
config:
  whats-new-directory: distribution/whatsnew
```

### Google Play Store Release Process

1. **Tag your release:**

   ```bash
   git tag -s v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

2. **Workflow automatically:**
   - Builds Android app with Gradle
   - Signs AAB with release keystore
   - Uploads to Google Play Console
   - Assigns to configured track

3. **Availability:**
   - Internal track: Immediately after upload
   - Alpha/Beta: After processing (~minutes)
   - Production: After review (first release) or immediately (updates)

---

## Security Features

All published artifacts include security features:

- **GPG Signing** - JAR files, POM files, release checksums, git tags
- **SBOM Generation** - SPDX and CycloneDX formats for all artifacts and containers
- **SLSA Provenance** - Level 3 attestations for containers
- **Namespace Validation** - Enforces correct registry namespaces to prevent unauthorized publishing

For verification instructions, see [Artifact Verification Guide](verification.md).
