# Workflow Architecture and Patterns

Advanced workflow documentation for DiggSweden reusable CI/CD workflows.

For basic usage, configuration, and getting started, see [README.md](../README.md).

## Table of Contents
- [Workflow Architecture](#workflow-architecture)
- [JReleaser Integration Patterns](#jreleaser-integration-patterns)
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

### Release Workflow Architecture

```mermaid
graph TD
    A[Tag Push: v*.*.* ] --> B[release-orchestrator.yml]
    B --> C[release-prerequisites.yml]
    C --> D[version-bump.yml]
    D --> E1[generate-full-changelog.yml]
    D --> E2[generate-minimal-changelog.yml]
    E1 --> F{Project Type?}
    E2 --> F

    F -->|Maven App| G1[publish-maven-app-github.yml]
    F -->|Maven Lib| G2[publish-maven-lib-central.yml]
    F -->|NPM App| G3[publish-npm-app-github.yml]

    G1 --> H{Container Enabled?}
    G2 --> H
    G3 --> H

    H -->|Yes| I[build-container-ghcr.yml]
    H -->|No| J{Release Publisher?}
    I --> J

    J -->|JReleaser| K1[release-github.yml with JReleaser]
    J -->|GitHub CLI| K2[release-github.yml with gh CLI]

    K1 --> L[release-summary]
    K2 --> L

    style A fill:#e1f5ff
    style B fill:#fff4e1
    style C fill:#ffebee
    style D fill:#f3e5f5
    style I fill:#e8f5e9
    style L fill:#e0f2f1
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

### Workflow Execution Patterns

#### Pattern 1: Maven Library (cose-lib)
```mermaid
graph LR
    A[Tag Push] --> B[Prerequisites]
    B --> C[Version Bump]
    C --> D[Publish to Maven Central]
    D --> E[JReleaser in publish step]
    E --> F[GitHub Release Created]

    style A fill:#e1f5ff
    style D fill:#e8f5e9
    style E fill:#fff4e1
    style F fill:#f3e5f5
```

#### Pattern 2: Maven/NPM Application with Container (issuer-poc, linter)
```mermaid
graph LR
    A[Tag Push] --> B[Prerequisites]
    B --> C[Version Bump]
    C --> D[Publish Artifacts]
    D --> E[Build Container]
    E --> F[Create GitHub Release]
    F --> G[JReleaser or GitHub CLI]

    style A fill:#e1f5ff
    style D fill:#e8f5e9
    style E fill:#e1f5ff
    style F fill:#f3e5f5
    style G fill:#fff4e1
```

---

## JReleaser Integration Patterns

JReleaser is used differently depending on your project type. Understanding when and how JReleaser runs is critical for correct configuration.

### Pattern 1: Library Publishing (Maven Central)

**Used by:** Maven libraries published to Maven Central (e.g., `cose-lib`)

**How it works:**
```yaml
# release-workflow.yml
with:
  artifactPublisher: maven-lib-mavencentral
  artifact.jreleaserenabled: true
  # No releasePublisher configured
```

**JReleaser runs DURING the Maven publish step:**
```text
1. Version bump
2. Publish to Maven Central
   └─ mvn deploy (publishes to Central)
   └─ mvn jreleaser:full-release (creates GitHub release)
3. Done (no separate release step)
```

**Why this pattern:**
- Libraries typically only publish JARs (no containers)
- JReleaser is configured as a Maven plugin in `pom.xml`
- GitHub release creation happens as part of publishing
- Single-step deployment to both Central and GitHub

**Configuration required:**
```xml
<!-- In pom.xml -->
<plugin>
  <groupId>org.jreleaser</groupId>
  <artifactId>jreleaser-maven-plugin</artifactId>
  <version>${jreleaser-maven-plugin.version}</version>
  <configuration>
    <configFile>${project.basedir}/jreleaser.yml</configFile>
  </configuration>
</plugin>
```

**Example repositories:**
- `cose-lib` - See `.github/workflows/release-workflow.yml`

---

### Pattern 2: Application Publishing (with Container Images)

**Used by:** Applications that build both artifacts AND containers (e.g., `eudiw-wallet-issuer-poc`, `rest-api-profil-lint-processor`)

**How it works:**
```yaml
# release-workflow.yml
with:
  artifactPublisher: maven-app-github  # or npm-app-github
  containerBuilder: containerimage-ghcr
  releasePublisher: jreleaser  # or github-cli
```

**JReleaser runs AFTER all artifacts are ready:**
```text
1. Version bump
2. Publish artifacts (JAR or NPM)
3. Build container image
4. Create GitHub release
   └─ JReleaser or GitHub CLI creates release
   └─ Attaches JAR + container reference + SBOM
```

**Why this pattern:**
- Applications need both artifacts AND containers
- Container must be built before creating release
- Release notes need to reference container image
- GitHub release is created after ALL artifacts ready

**When to use JReleaser vs GitHub CLI:**
- **JReleaser:** Maven projects with complex artifact sets
- **GitHub CLI:** NPM projects or simpler releases

**Example repositories:**
- `eudiw-wallet-issuer-poc` - Maven app with JReleaser
- `rest-api-profil-lint-processor` - NPM app with GitHub CLI

---

### Decision Tree: Which Pattern Should I Use?

```text
Is this a library or application?
├─ Library
│  └─ Publishing to Maven Central?
│     ├─ Yes → Use Pattern 1 (JReleaser in publish step)
│     └─ No → Use Pattern 2 (separate release step)
│
└─ Application
   └─ Building container images?
      ├─ Yes → Use Pattern 2 (JReleaser after container)
      └─ No → Either pattern works (Pattern 2 recommended)
```

### Common Mistakes

**❌ Wrong: Using Pattern 1 for applications with containers**
```yaml
# This doesn't work if you build containers!
artifactPublisher: maven-app-github
artifact.jreleaserenabled: true  # ← JReleaser runs too early
containerBuilder: containerimage-ghcr  # ← Container built AFTER release created
```

**Problem:** GitHub release is created before container exists, so container can't be referenced in release notes.

**✅ Correct: Using Pattern 2 for applications with containers**
```yaml
artifactPublisher: maven-app-github
containerBuilder: containerimage-ghcr
releasePublisher: jreleaser  # ← JReleaser runs AFTER container ready
```

---

## Project Structure Required

### Maven Projects
```text
your-repo/
├── pom.xml
├── src/
├── Containerfile (optional)
├── jreleaser.yml (optional)
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

---

## Examples

### Java Spring Boot Application
```yaml
jobs:
  release:
    uses: diggsweden/.github/.github/workflows/release-orchestrator.yml@main
    with:
      projectType: maven
      artifactPublisher: maven-app-github      # JAR to GitHub Packages
      containerBuilder: containerimage-ghcr    # Docker image to ghcr.io
      releasePublisher: jreleaser              # GitHub release with changelog
      artifact.javaversion: "21"               # Java 21 LTS
      container.platforms: "linux/amd64,linux/arm64"  # Intel + ARM support
```

### Node.js API Service
```yaml
jobs:
  release:
    uses: diggsweden/.github/.github/workflows/release-orchestrator.yml@main
    with:
      projectType: npm
      artifactPublisher: npm-app-github     # Package to GitHub NPM registry
      containerBuilder: containerimage-ghcr # Docker image with Node.js app
      releasePublisher: github-cli          # GitHub CLI for releases
      artifact.nodeversion: "22"            # Latest Node.js LTS
```

### Maven Library (No Container)
```yaml
jobs:
  release:
    uses: diggsweden/.github/.github/workflows/release-orchestrator.yml@main
    with:
      projectType: maven
      artifactPublisher: maven-lib-mavencentral  # Publish to Maven Central
      releasePublisher: jreleaser                # Handles Central deployment
      artifact.settingspath: ".mvn/settings.xml" # Contains Central credentials
      artifact.jreleaserenabled: true            # JReleaser plugin in pom.xml
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
      projectType: maven  # Only builds container, no releases/artifacts
```

---

*Last updated: 2024*
