<!--
SPDX-FileCopyrightText: 2025 The Reusable CI Authors

SPDX-License-Identifier: CC0-1.0
-->

# Publishing Guide

Complete guide to publishing artifacts to different registries.

## Overview

The reusable workflows support multiple publishing targets:

| Target | Artifact Types | Authentication | Public/Private |
|--------|---------------|----------------|----------------|
| **GitHub Packages** | Maven, NPM, Gradle, Containers | `GITHUB_TOKEN` (automatic) | Organization only |
| **Maven Central** | Maven libraries | Sonatype credentials | Public |
| **npmjs.org** | NPM packages | NPM token | Public |
| **Container Registries** | Container images | Token/credentials | Configurable |

---

## GitHub Packages (Default)

**Overview:**

- **No setup required** - Uses `GITHUB_TOKEN` automatically
- **Always available** - Works for all DiggSweden projects
- **Organization scoped** - Only accessible within DiggSweden org

### GitHub Packages Configuration

```yaml
# .github/artifacts.yml
artifacts:
  - name: my-app
    project-type: maven  # or npm, gradle
    working-directory: .
    # publish-to defaults to [github-packages]
```

### Accessing Published Artifacts

#### Maven

```xml
<!-- ~/.m2/settings.xml or project settings -->
<servers>
  <server>
    <id>github</id>
    <username>YOUR_GITHUB_USERNAME</username>
    <password>YOUR_GITHUB_TOKEN</password>
  </server>
</servers>

<repositories>
  <repository>
    <id>github</id>
    <url>https://maven.pkg.github.com/diggsweden/REPO_NAME</url>
  </repository>
</repositories>
```

#### NPM

```bash
# Configure npm to use GitHub Packages
npm config set @diggsweden:registry https://npm.pkg.github.com
npm config set //npm.pkg.github.com/:_authToken YOUR_GITHUB_TOKEN
```

#### Containers

```bash
# Login to GitHub Container Registry
echo $GITHUB_TOKEN | podman login ghcr.io -u USERNAME --password-stdin

# Pull image
podman pull ghcr.io/diggsweden/repo-name:v1.0.0
```

---

## Maven Central

### Maven Central Overview

- **Public distribution** - Available to all Java developers worldwide
- **Libraries only** - Not for applications
- **Requires approval** - GroupId must be claimed via Sonatype
- **Signature required** - All artifacts must be GPG signed

### Maven Central Prerequisites

1. **Sonatype OSSRH Account**
   - Create account at <https://central.sonatype.com/>
   - Claim your groupId (e.g., `se.digg`)
   - Wait for approval (1-2 business days)

2. **GPG Key Setup**
   - Already configured at DiggSweden org level
   - Request access from GitHub administrators
   - Required secrets: `OSPO_BOT_GPG_PRIV`, `OSPO_BOT_GPG_PASS`, `OSPO_BOT_GPG_PUB`

3. **Maven Central Credentials**
   - Request from DiggSweden GitHub administrators
   - Required secrets: `MAVENCENTRAL_USERNAME`, `MAVENCENTRAL_PASSWORD`

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
      java-version: 21
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

  <!-- Distribution management (handled by workflow) -->
  <distributionManagement>
    <repository>
      <id>ossrh</id>
      <url>https://s01.oss.sonatype.org/service/local/staging/deploy/maven2/</url>
    </repository>
  </distributionManagement>
</project>
```

### Maven Settings (Optional)

If you need custom repository configuration:

```xml
<!-- .mvn/settings.xml -->
<settings>
  <servers>
    <server>
      <id>ossrh</id>
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

1. **Tag your release:**

   ```bash
   git tag -s v1.0.0 -m "Release v1.0.0"
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

Users add to their `pom.xml`:

```xml
<dependency>
  <groupId>se.digg</groupId>
  <artifactId>my-library</artifactId>
  <version>1.0.0</version>
</dependency>
```

---

## NPM Registry (npmjs.org)

### NPM Registry Overview

- **Public distribution** - Available to all Node.js developers
- **Scoped packages** - Use `@diggsweden/` prefix
- **No approval needed** - Publish immediately

### NPM Prerequisites

1. **npmjs.org Account**
   - Create account at <https://www.npmjs.com/>
   - Verify email address

2. **NPM Token**
   - Generate at <https://www.npmjs.com/settings/tokens>
   - Type: "Automation" token
   - Request DiggSweden administrators add as `NPM_TOKEN` secret

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
      node-version: 22
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
| `docker.io` | No | `DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN` |
| Custom | No | Custom credentials |

### GitHub Container Registry (ghcr.io)

**Default and recommended** - No setup required:

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
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2-dev
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
    uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2-dev
    with:
      artifacts-config: .github/artifacts.yml
      container.registry: registry.example.com
      container.registry-username: ${{ secrets.REGISTRY_USERNAME }}
      container.use-github-token: false
    secrets: inherit
```

---

## Security Features

### GPG Signing

**Enabled by default** for all Maven Central and GitHub releases.

**What gets signed:**

- JAR files (Maven)
- POM files (Maven)
- Release checksums
- Git tags

**Verification:**

```bash
# Verify JAR signature
gpg --verify my-library-1.0.0.jar.asc my-library-1.0.0.jar

# Import public key first
curl -s https://api.github.com/repos/diggsweden/repo/contents/GPG_PUBLIC_KEY | \
  jq -r .content | base64 -d | gpg --import
```

### SBOM Generation

**Enabled by default** for all artifacts and containers.

**Formats generated:**

- SPDX JSON
- CycloneDX JSON

**Attached to:**

- GitHub releases
- Container images (as attestation)

### SLSA Provenance

**Enabled by default** for containers.

**Verifiable with:**

```bash
# Install slsa-verifier
gh release download -R slsa-framework/slsa-verifier

# Verify container
slsa-verifier verify-image ghcr.io/diggsweden/my-app:v1.0.0 \
  --source-uri github.com/diggsweden/my-app
```

---

## Troubleshooting

### Maven Central Troubleshooting

**"Failed to deploy artifacts"**

- Check Sonatype credentials are valid
- Verify groupId is approved
- Ensure `build-type: library` is set
- Check all required POM fields present

**"GPG signing failed"**

- Request GPG secrets from administrators
- Verify secrets are accessible to repository

### NPM Issues

**"Authentication failed"**

- Check NPM_TOKEN is valid
- Regenerate token if expired
- Verify token has "Automation" type

**"Package name already taken"**

- Use scoped package: `@diggsweden/name`
- Check if package exists: `npm view @diggsweden/name`

### Container Troubleshooting

**"Failed to push to registry"**

- Check `packages: write` permission set
- Verify registry authentication
- Check registry exists and is accessible

---

## Best Practices

1. **Use GitHub Packages for internal** - No setup, works everywhere
2. **Use Maven Central for public libraries** - Industry standard
3. **Always enable signing** - Required for Maven Central, good practice
4. **Test locally first** - Build and verify before tagging
5. **Version carefully** - Can't unpublish from Maven Central
6. **Keep secrets secure** - Never log or expose tokens
7. **Monitor releases** - Check registry after publishing
8. **Document changes** - Good changelogs help users

---

## See Also

- [Configuration Reference](configuration.md) - Complete artifacts.yml guide
- [Troubleshooting](troubleshooting.md) - Common publishing errors
- [Workflows Guide](workflows.md) - Workflow configuration
