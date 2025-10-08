<!--
SPDX-FileCopyrightText: 2025 The Reusable CI Authors

SPDX-License-Identifier: CC0-1.0
-->

# Workflow Architecture and Patterns

Advanced workflow documentation for DiggSweden reusable CI/CD workflows.

For basic usage, configuration, and getting started, see [README.md](../README.md).

## Table of Contents

- [Workflow Architecture](#workflow-architecture)
- [Project Structure Required](#project-structure-required)
- [Examples](#examples)

---

## Workflow Architecture

### Pull Request Workflow Architecture

```mermaid
graph TD
    A[Pull Request Created/Updated] --> B[pullrequest-orchestrator.yml]
    B --> C[commit-lint]
    B --> D[license-lint]
    B --> E[dependency-review]
    B --> F[megalint]
    B --> G[publiccode-lint]
    C --> H[lint-status]
    D --> H
    E --> H
    F --> H
    G --> H
    H --> I[Project test.yml]

    style A fill:#e1f5ff
    style B fill:#fff4e1
    style H fill:#e8f5e9
    style I fill:#f3e5f5
```

### Release Workflow Architecture (v2-dev with Containers)

```mermaid
graph TD
    A[Tag Push: v*.*.* ] --> B[release-orchestrator.yml]
    B --> C[release-prerequisites.yml]
    C --> D[version-bump.yml]
    D --> E1[generate-full-changelog.yml]
    D --> E2[generate-minimal-changelog.yml]
    E1 --> F{Project Type?}
    E2 --> F

    F -->|Maven| G1[build-maven.yml]
    F -->|NPM| G2[build-npm.yml]
    F -->|Gradle| G3[build-gradle.yml]

    G1 --> H{Artifact Publisher?}
    G2 --> H
    G3 --> H

    H -->|maven-app-github| I1[publish-github.yml]
    H -->|npm-app-github| I1
    H -->|gradle-app-github| I1
    H -->|maven-lib-mavencentral| I2[publish-mavencentral.yml]

    I1 --> J{Container Builder?}
    I2 --> J

    J -->|container-image| K[publish-container.yml]
    J -->|None| L{Release Publisher?}
    K --> L

    L -->|github-cli| M[release-github.yml]

    M --> N[release-summary]

    style A fill:#e1f5ff
    style B fill:#fff4e1
    style C fill:#ffebee
    style D fill:#f3e5f5
    style G1 fill:#e1f5ff
    style G2 fill:#e1f5ff
    style G3 fill:#e1f5ff
    style I1 fill:#e8f5e9
    style I2 fill:#e8f5e9
    style K fill:#e8f5e9
    style N fill:#e0f2f1
```

### Component Interaction Flow

```mermaid
graph LR
    A[Project Workflow] --> B[Orchestrator]
    B --> C[Validator]
    B --> D[Publisher]
    B --> E[Builder]
    B --> F[Release Creator]

    C -.validates.-> G[(Secrets)]
    C -.checks.-> H[(Version)]

    D -.uploads.-> I[(Maven Central)]
    D -.uploads.-> J[(GitHub Packages)]
    D -.uploads.-> K[(NPM Registry)]

    E -.builds.-> L[(Container Image)]
    E -.generates.-> M[(SBOM)]
    E -.scans.-> N[(Vulnerabilities)]

    F -.creates.-> O[(GitHub Release)]
    F -.signs.-> P[(GPG Signatures)]

    style B fill:#fff4e1
    style C fill:#ffebee
    style D fill:#e8f5e9
    style E fill:#e1f5ff
    style F fill:#f3e5f5
```

### Workflow Execution Patterns (v2-dev)

#### Pattern 1: Maven Library (cose-lib)

```mermaid
graph LR
    A[Tag Push] --> B[Prerequisites]
    B --> C[Version Bump]
    C --> D[Build Maven]
    D --> E[Publish to Maven Central]
    E --> F[GitHub Release Created]

    style A fill:#e1f5ff
    style D fill:#e1f5ff
    style E fill:#e8f5e9
    style F fill:#f3e5f5
```

#### Pattern 2: Maven/NPM Application with Container (issuer-poc, linter)

```mermaid
graph LR
    A[Tag Push] --> B[Prerequisites]
    B --> C[Version Bump]
    C --> D[Build Maven/NPM]
    D --> E[Publish to GitHub]
    E --> F[Build Container]
    F --> G[Create GitHub Release]

    style A fill:#e1f5ff
    style D fill:#e1f5ff
    style E fill:#e8f5e9
    style F fill:#e1f5ff
    style G fill:#f3e5f5
```

#### Pattern 3: Multi-Registry Publishing

```mermaid
graph LR
    A[Tag Push] --> B[Prerequisites]
    B --> C[Version Bump]
    C --> D[Build Once]
    D --> E1[Publish to GitHub]
    D --> E2[Publish to Maven Central]
    E1 --> F[Create Release]
    E2 --> F

    style A fill:#e1f5ff
    style D fill:#e1f5ff
    style E1 fill:#e8f5e9
    style E2 fill:#e8f5e9
    style F fill:#f3e5f5
```

---

## Project Structure Required

### Maven Projects

```text
your-repo/
├── pom.xml
├── src/
├── Containerfile (optional)
└── .github/
    └── workflows/
        ├── pullrequest-workflow.yml
        └── release-workflow.yml
```

### NPM Projects

```text
your-repo/
├── package.json
├── package-lock.json
├── src/
├── Containerfile (optional)
└── .github/
    └── workflows/
        ├── pullrequest-workflow.yml
        └── release-workflow.yml
```

### Gradle Projects (Android/JVM)

```text
your-repo/
├── build.gradle.kts
├── settings.gradle.kts
├── gradle.properties              # Version management (versionName, versionCode)
├── CHANGELOG.md
├── app/
│   └── build.gradle.kts
├── gradlew
└── .github/
    └── workflows/
        ├── pullrequest-workflow.yml
        ├── release-workflow.yml
        └── test.yml (optional)
```

**Important:** For Gradle projects, version information must be stored in `gradle.properties`:

```properties
versionName=1.0.0
versionCode=5
```

And read in `app/build.gradle.kts`:

```kotlin
android {
    defaultConfig {
        versionCode = (project.property("versionCode") as String).toInt()
        versionName = project.property("versionName") as String
    }
}
```

---

## Examples

### Java Spring Boot Application

```yaml
# .github/artifacts.yml
artifacts:
  - name: my-app
    project-type: maven
    working-directory: .
    publishers:
      - maven-app-github
    config:
      javaversion: 21

containers:
  - name: my-app
    from: [my-app]
    containerfile: Containerfile
    context: .
    platforms: linux/amd64,linux/arm64

# .github/workflows/release-workflow.yml
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2-dev
    with:
      artifacts-config: .github/artifacts.yml
      release-publisher: github-cli
```

### Node.js API Service

```yaml
# .github/artifacts.yml
artifacts:
  - name: api-service
    project-type: npm
    working-directory: .
    publishers:
      - npm-app-github
    config:
      nodeversion: 22

containers:
  - name: api-service
    from: [api-service]
    containerfile: Containerfile
    context: .
    platforms: linux/amd64,linux/arm64

# .github/workflows/release-workflow.yml
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2-dev
    with:
      artifacts-config: .github/artifacts.yml
      release-publisher: github-cli
```

### Maven Library (No Container)

```yaml
# .github/artifacts.yml
artifacts:
  - name: my-lib
    project-type: maven
    working-directory: .
    publishers:
      - maven-lib-mavencentral
    config:
      javaversion: 21
      settingspath: .mvn/settings.xml

# No containers section needed

# .github/workflows/release-workflow.yml
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2-dev
    with:
      artifacts-config: .github/artifacts.yml
      release-publisher: github-cli
```

### Android Application (Gradle)

```yaml
# .github/artifacts.yml
artifacts:
  - name: my-android-app
    project-type: gradle
    working-directory: .
    publishers:
      - gradle-app-github
    config:
      javaversion: 21
      gradletasks: build assembleDemoRelease bundleDemoRelease
      buildmodule: app
      attachpattern: app/build/outputs/**/*.{apk,aab}
      gradleversionfile: gradle.properties

# .github/workflows/release-workflow.yml
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2-dev
    permissions:
      contents: write
      packages: write
      id-token: write
    with:
      artifacts-config: .github/artifacts.yml
      release-publisher: github-cli
      changelog-creator: git-cliff
      release.signartifacts: true
      release.generatesbom: true
```

**What this produces:**

- APK (release) → `app/build/outputs/apk/demo/release/*.apk`
- AAB (bundle) → `app/build/outputs/bundle/demoRelease/*.aab`
- SBOM → `sbom.cyclonedx.json`
- All attached to GitHub Release

### JVM Application (Gradle, non-Android)

```yaml
# .github/artifacts.yml
artifacts:
  - name: spring-boot-app
    project-type: gradle
    working-directory: .
    publishers:
      - gradle-app-github
    config:
      javaversion: 21
      gradletasks: build bootJar

containers:
  - name: spring-boot-app
    from: [spring-boot-app]
    containerfile: Containerfile
    context: .
    platforms: linux/amd64,linux/arm64

# .github/workflows/release-workflow.yml
jobs:
  release:
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2-dev
    with:
      artifacts-config: .github/artifacts.yml
      release-publisher: github-cli
```

### Development Builds

```yaml
on:
  push:
    branches: [develop]
jobs:
  build:
    uses: diggsweden/.github/.github/workflows/release-dev-orchestrator.yml@main
    with:
      project-type: maven  # Only builds container, no releases/artifacts
```

---

*Last updated: 2025-10-08*
