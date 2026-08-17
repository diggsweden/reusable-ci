# Publishing Guide

Configure and verify the supported publishing targets — Maven Central,
GitHub Packages, container registries, App Store Connect, and Google
Play. Per-target sections cover prerequisites, the `artifacts.yml`
fields and secrets each one needs, and the consumer-side fetch
commands.

## Targets at a glance

| Target                    | Artifact types     | Authentication              |
|---------------------------|--------------------|-----------------------------|
| Maven Central             | Maven libraries    | Sonatype credentials        |
| GitHub Packages (Maven)   | Maven artifacts    | `GITHUB_TOKEN` (automatic)  |
| GitHub Packages (NPM)     | NPM packages       | `GITHUB_TOKEN` (automatic)  |
| Container registries      | Container images   | `GITHUB_TOKEN` or registry-password |
| Apple App Store           | iOS / macOS apps   | App Store Connect API v2    |
| Google Play Store         | Android apps       | Service Account JSON        |

---

### NPM

```bash
# Configure npm to use GitHub Packages
npm config set @your-github-org:registry https://npm.pkg.github.com
npm config set //npm.pkg.github.com/:_authToken YOUR_GITHUB_TOKEN
```

#### Containers

```bash
# Pull image
podman pull ghcr.io/<owner>/<repo>:v1.0.0
```

---

## Maven Central

### Prerequisites

1. **GPG Key Setup** — configure `RELEASE_GPG_PRIVATE_KEY`,
   `RELEASE_GPG_PASSPHRASE`, and `RELEASE_GPG_PUBLIC_KEY` as repository
   or organization secrets.

2. **Maven Central Credentials** — configure `MAVEN_CENTRAL_USERNAME`
   and `MAVEN_CENTRAL_PASSWORD` the same way.

### Configuration

```yaml
# .reusable-ci/artifacts.yml
artifacts:
  - name: my-library
    project-type: maven
    working-directory: .
    build-type: library              # Required for Maven Central
    require-authorization: true      # Recommended for production libraries
    publish-to:
      - forge-packages              # Also publish to GitHub
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
  <groupId>com.example</groupId>
  <artifactId>my-library</artifactId>
  <version>1.0.0</version>
  <packaging>jar</packaging>

  <name>My Library</name>
  <description>A brief description</description>
  <url>https://github.com/<owner>/my-library</url>

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
      <email>your.email@example.com</email>
      <organization>Your Organization</organization>
      <organizationUrl>https://example.com</organizationUrl>
    </developer>
  </developers>

  <!-- Required SCM info -->
  <scm>
    <connection>scm:git:git://github.com/<owner>/my-library.git</connection>
    <developerConnection>scm:git:ssh://github.com:<owner>/my-library.git</developerConnection>
    <url>https://github.com/<owner>/my-library/tree/main</url>
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
      <username>${env.MAVEN_CENTRAL_USERNAME}</username>
      <password>${env.MAVEN_CENTRAL_PASSWORD}</password>
    </server>
  </servers>
</settings>
```

Reference it in artifacts.yml:

```yaml
config:
  settings-path: .mvn/settings.xml
```

### Release Process

1. **Request a release** — push a SIGNED request tag:

   ```bash
   git tag -s release-request/v1.0.0 -m "Release v1.0.0"
   git push origin release-request/v1.0.0
   ```

   reusable-ci verifies your signature (against the committed allowlist, when
   enabled), bumps the version + changelog, then **creates the immutable
   `v1.0.0` release tag once** at the bump commit — no tag is force-pushed or
   mutated. Your signed `release-request/v1.0.0` tag remains as the
   authorisation anchor, and the bot's release commit records you as the
   original tagger (`Release-Authorized-By` / `Co-authored-by` trailers).

   The reusable-ci example release workflows trigger on `release-request/v*`.
   (SNAPSHOT/dev builds are a separate `workflow_dispatch` flow — see the
   snapshot orchestrator — and do not use release-request tags.)

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
  <groupId>com.example</groupId>
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
          <url>https://central.sonatype.com/repository/maven-snapshots/</url>
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
  <groupId>com.example</groupId>
  <artifactId>my-library</artifactId>
  <version>1.0.0-SNAPSHOT</version>
</dependency>
```

**Note:** Snapshots are development versions and may change frequently. Use `updatePolicy>always</updatePolicy>` to always check for latest snapshot.

---

## NPM Packages (GitHub Packages)

NPM publishing goes to `npm.pkg.github.com` and authenticates with the
auto-provided `GITHUB_TOKEN`; no separate npm token is needed. The
package name must use the `@<owner>/` scope, with `<owner>` matching
the lowercased GitHub repository owner.

Public-registry publishing to `npmjs.org` is not implemented today.
The `npmjs` `publish-to` value is reserved in the schema; current
config validation rejects it.

### Configuration

```yaml
# .reusable-ci/artifacts.yml
artifacts:
  - name: my-package
    project-type: npm
    working-directory: packages/my-package
    publish-to:
      - forge-packages  # Publish to GitHub Packages
    config:
      node-version: 24
```

### Package Requirements

Your `package.json` must include:

```json
{
  "name": "@your-github-org/my-package",
  "version": "1.0.0",
  "description": "A brief description",
  "main": "dist/index.js",
  "types": "dist/index.d.ts",
  "license": "MIT",
  "repository": {
    "type": "git",
    "url": "https://github.com/your-github-org/my-package.git"
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
   git tag -s release-request/v1.0.0 -m "Release v1.0.0"
   git push origin release-request/v1.0.0
   ```

2. **Workflow automatically:**
    - Runs `npm run build` when a `build` script exists
    - Packs the package tarball
    - Publishes the tarball to GitHub Packages

3. **Availability:**
    - Appears under the repository/organization packages in GitHub
    - Installable through `npm.pkg.github.com` after registry authentication

### Consuming Published Package

Users install with:

```bash
npm install @your-github-org/my-package
```

---

## Container Registries

`ghcr.io` is the default — `GITHUB_TOKEN` covers auth, no setup
beyond declaring the container in `artifacts.yml`. Other registries
work via the `registry-password` secret on
`publish-container.yml`'s call site.

### GitHub Container Registry (ghcr.io)

No setup required — declare the container and push:

```yaml
containers:
  - name: my-app
    from: [my-app]
    container-file: Containerfile
    # registry defaults to ghcr.io
```

**Image naming:**

```text
ghcr.io/OWNER/REPO_NAME/container-name:v1.0.0
```

**Namespace security:**
- Images must follow pattern: `ghcr.io/<owner>/<repo>` or `ghcr.io/<owner>/<repo>-*`
- Default owner: `github.repository_owner` (lowercased)
- Configurable via direct `publish-container.yml` `enforce-namespace` input
- Prevents pushing to unauthorized namespaces
- Enforced automatically during container build

**Custom namespace (direct component use only):**

```yaml
jobs:
  publish-container:
    uses: diggsweden/reusable-ci/.github/workflows/publish-container.yml@v3.0.0
    permissions:
      contents: read
      packages: write
      id-token: write
      attestations: write
      actions: read
    secrets: inherit
    with:
      reusable-ci-binary-ref: v3.0.0
      container-file: Containerfile
      artifact-types: maven
      registry: ghcr.io
      enforce-namespace: my-custom-org  # Override default
```

**Pull image:**

```bash
podman pull ghcr.io/<owner>/<repo>/my-app:v1.0.0
```

### Docker Hub

Requires a Docker Hub access token. Store the username and token as
secrets (`DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`).

The release orchestrator publishes containers to GHCR. For Docker
Hub, call `publish-container.yml` directly after the build job, set
the Docker Hub image name explicitly, and pass registry credentials
through the `registry-username` input and the `registry-password`
secret:

```yaml
jobs:
  publish-container:
    needs: build-maven
    uses: diggsweden/reusable-ci/.github/workflows/publish-container.yml@v3.0.0
    permissions:
      contents: read
      packages: write
      id-token: write
      attestations: write
      actions: read
    with:
      reusable-ci-binary-ref: v3.0.0
      container-file: Containerfile
      artifact-types: maven
      registry: docker.io
      image-name: docker.io/DOCKERHUB_ORG/my-app
      registry-username: ${{ vars.DOCKERHUB_USERNAME }}
      use-ci-token: false
      enable-slsa: false
    secrets:
      registry-password: ${{ secrets.DOCKERHUB_TOKEN }}
```

**Image naming:**

```text
docker.io/DOCKERHUB_ORG/my-app:v1.0.0
```

### Custom Registry

```yaml
jobs:
  publish-container:
    needs: build-maven
    uses: diggsweden/reusable-ci/.github/workflows/publish-container.yml@v3.0.0
    permissions:
      contents: read
      packages: write
      id-token: write
      attestations: write
      actions: read
    with:
      reusable-ci-binary-ref: v3.0.0
      container-file: Containerfile
      artifact-types: maven
      registry: registry.example.com
      registry-username: ${{ vars.REGISTRY_USERNAME }}
      use-ci-token: false
      enable-slsa: false
    secrets:
      registry-password: ${{ secrets.REGISTRY_TOKEN }}
```

---

## Apple App Store (TestFlight)

iOS builds upload to App Store Connect via API v2 (no Fastlane).
Review submission stays a manual step in App Store Connect; reusable-ci
gets the artifact uploaded and stops there.

### Prerequisites

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

**To build without App Store publishing**, set `enable-code-signing: false`:

```yaml
config:
  enable-code-signing: false  # Build archive only; release publish skips App Store upload
```

**Note:** iOS apps use `publish-to: []` because they don't publish to package
registries like Maven Central or npm. The App Store upload happens automatically
for signed Xcode artifacts and is skipped for unsigned archive-only builds.

### Configuration

```yaml
# .reusable-ci/artifacts.yml
artifacts:
  - name: my-ios-app
    project-type: xcode-ios
    working-directory: .
    publish-to: []  # iOS apps publish via App Store Connect, not package registries
    config:
      xcode-version: "16.1"
      scheme: "MyApp"
      use-xcodegen: true
      xcodegen-spec: "project.yml"
      project: "MyApp.xcodeproj"
      configuration: Release
      enable-code-signing: true   # Enables IPA export and App Store Connect upload
      export-options-var: EXPORT_OPTIONS_BASE64
      macos-version: macos-26
      # App Store Connect upload options
      submit-for-review: false  # Summary intent only; submit review manually in App Store Connect
      skip-validation: false    # Validate IPA before upload (recommended)
```

If your app uses XcodeGen, keep `project` or `workspace` configured as well so the generated build target is explicit in later steps.

### Required Secrets

```text
# Code Signing
IOS_SIGNING_CERTIFICATE_BASE64              # Base64-encoded .p12 distribution certificate
IOS_SIGNING_CERTIFICATE_PASSPHRASE          # Certificate password
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

### Release Process

1. **Tag your release:**

   ```bash
   git tag -s release-request/v1.0.0 -m "Release v1.0.0"
   git push origin release-request/v1.0.0
   ```

2. **Workflow automatically:**
   - Builds iOS app with Xcode
   - Signs with distribution certificate
   - Exports IPA file
   - Validates IPA with Apple
   - Uploads to App Store Connect

3. **Availability:**
   - TestFlight: ~10-15 minutes after upload processing
   - App Store: After manual review submission in App Store Connect

---

## Google Play Store

Android builds upload to Google Play via the Developer API v3. The
workflow supports the four standard tracks (internal, alpha, beta,
production) and staged rollouts to a fraction of users.

### Prerequisites

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

### Configuration

```yaml
# .reusable-ci/artifacts.yml
artifacts:
  - name: my-android-app
    project-type: gradle-android
    working-directory: .
    publish-to:
      - google-play    # <-- This enables Google Play publishing
    config:
      build-module: app
      product-flavor: demo
      build-types: release
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

The workflow maps the `ANDROID_KEYSTORE` secret to the internal
`ANDROID_KEYSTORE_BASE64` environment variable used by the `reusable-ci` CLI.
Repository users should create the secret as `ANDROID_KEYSTORE`.

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

### Release Process

1. **Tag your release:**

   ```bash
   git tag -s release-request/v1.0.0 -m "Release v1.0.0"
   git push origin release-request/v1.0.0
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

Published artifacts can include these security features, depending on artifact
type and enabled workflow inputs:

- **GPG Signing** - Release checksums and package artifacts when signing is enabled
- **SBOM Generation** - CycloneDX/SPDX outputs for supported `sboms` layers
- **SLSA Provenance** - portable, signed SLSA v1.0 build provenance (cosign attest, ~Build L2) on any registry/forge when enabled; GHCR adds an opt-in GitHub attestation-store record
- **Namespace Validation** - Enforces correct registry namespaces to prevent unauthorized publishing

For verification instructions, see [Artifact Verification Guide](verification.md).
