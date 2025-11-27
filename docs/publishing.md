<!--
SPDX-FileCopyrightText: 2025 The Reusable CI Authors

SPDX-License-Identifier: CC0-1.0
-->

# Publishing Guide

Complete guide to publishing artifacts to different registries.

## Overview

The reusable workflows support multiple publishing targets:

| Target | Artifact Types | Authentication |  |
|--------|---------------|----------------|----------------|
| **Maven Central** | Maven libraries | Sonatype credentials |  |
| **npmjs.org** | NPM packages | NPM token |  |
| **Container Registries** | Container images | Token/credentials |  |

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

## Security Features

All published artifacts include security features:

- **GPG Signing** - JAR files, POM files, release checksums, git tags
- **SBOM Generation** - SPDX and CycloneDX formats for all artifacts and containers
- **SLSA Provenance** - Level 3 attestations for containers
- **Namespace Validation** - Enforces correct registry namespaces to prevent unauthorized publishing

For verification instructions, see [Artifact Verification Guide](verification.md).
