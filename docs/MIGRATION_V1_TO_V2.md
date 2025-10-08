# Migration Guide: v1 to v2

## Overview

v2 introduces a clean, unified architecture but requires workflow updates. This guide helps you migrate from v1 to v2.

## Breaking Changes

### 1. `containerBuilder` Removed

**v1:**
```yaml
containerBuilder: containerimage-ghcr
```

**v2:**
```yaml
publishers: container-image-ghcr
# Or combined with other publishers:
publishers: maven-app-github,container-image-ghcr
```

### 2. `artifactPublisher` → `publishers`

**v1:**
```yaml
artifactPublisher: maven-app-github
```

**v2:**
```yaml
publishers: maven-app-github
```

### 3. `artifact.*` → `config.*`

**v1:**
```yaml
artifact.javaversion: 21
artifact.nodeversion: 22
artifact.attachpattern: target/*.jar
```

**v2:**
```yaml
config.javaversion: 21
config.nodeversion: 22
config.attachpattern: target/*.jar
```

### 4. `container.*` → `config.*`

**v1:**
```yaml
container.platforms: linux/amd64,linux/arm64
container.containerfile: Containerfile
container.enableslsa: true
```

**v2:**
```yaml
config.platforms: linux/amd64,linux/arm64
config.containerfile: Containerfile
config.enableslsa: true
```

## Migration Examples

### Example 1: Maven App Only

**v1:**
```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v1
with:
  projectType: maven
  artifactPublisher: maven-app-github
  artifact.javaversion: 21
  releasePublisher: github-cli
```

**v2:**
```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2
with:
  projectType: maven
  publishers: maven-app-github
  config.javaversion: 21
  releasePublisher: github-cli
```

### Example 2: Maven App + Container

**v1:**
```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v1
with:
  projectType: maven
  artifactPublisher: maven-app-github
  artifact.javaversion: 21
  containerBuilder: containerimage-ghcr
  container.platforms: linux/amd64,linux/arm64
  container.containerfile: Containerfile
  releasePublisher: github-cli
```

**v2:**
```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2
with:
  projectType: maven
  publishers: maven-app-github,container-image-ghcr
  config.javaversion: 21
  config.platforms: linux/amd64,linux/arm64
  config.containerfile: Containerfile
  releasePublisher: github-cli
```

### Example 3: NPM App + Container

**v1:**
```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v1
with:
  projectType: npm
  artifactPublisher: npm-app-github
  artifact.nodeversion: 22
  containerBuilder: containerimage-ghcr
  container.platforms: linux/amd64,linux/arm64
  releasePublisher: github-cli
```

**v2:**
```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2
with:
  projectType: npm
  publishers: npm-app-github,container-image-ghcr
  config.nodeversion: 22
  config.platforms: linux/amd64,linux/arm64
  releasePublisher: github-cli
```

### Example 4: Container Only

**v1:**
```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v1
with:
  projectType: maven  # Still needed for version bump
  containerBuilder: containerimage-ghcr
  container.platforms: linux/amd64,linux/arm64
```

**v2:**
```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2
with:
  projectType: container
  publishers: container-image-ghcr
  config.platforms: linux/amd64,linux/arm64
```

### Example 5: Maven Library to Maven Central

**v1:**
```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v1
with:
  projectType: maven
  artifactPublisher: maven-lib-mavencentral
  artifact.javaversion: 21
  artifact.settingspath: .mvn/settings.xml
  artifact.jreleaserenabled: true
  releasePublisher: jreleaser
```

**v2:**
```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2
with:
  projectType: maven
  publishers: maven-lib-mavencentral
  config.javaversion: 21
  config.settingspath: .mvn/settings.xml
  config.jreleaserenabled: true
  releasePublisher: jreleaser
```

## Monorepo Migration

### v1 (No Monorepo Support)

In v1, you had to create separate workflows for each component or use hacky workarounds.

### v2 (Native Monorepo Support)

Create `.github/artifacts.yml`:

```yaml
artifacts:
  - name: service-a
    projectType: maven
    workingDirectory: services/service-a
    publishers:
      - maven-app-github
      - container-image-ghcr
    config:
      javaversion: 21
      containerfile: services/service-a/Containerfile
      platforms: linux/amd64,linux/arm64
  
  - name: service-b
    projectType: maven
    workingDirectory: services/service-b
    publishers:
      - maven-app-github
      - container-image-ghcr
    config:
      javaversion: 21
      containerfile: services/service-b/Containerfile
```

Workflow:
```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2
with:
  artifactsConfig: .github/artifacts.yml
  releasePublisher: github-cli
```

## Field Mapping Reference

| v1 | v2 | Notes |
|----|-----|-------|
| `projectType` | `projectType` | Unchanged |
| `artifactPublisher` | `publishers` | Now comma-separated or array |
| `containerBuilder` | Part of `publishers` | e.g., `container-image-ghcr` |
| `artifact.javaversion` | `config.javaversion` | Namespace change |
| `artifact.nodeversion` | `config.nodeversion` | Namespace change |
| `artifact.attachpattern` | `config.attachpattern` | Namespace change |
| `artifact.npmtag` | `config.npmtag` | Namespace change |
| `artifact.settingspath` | `config.settingspath` | Namespace change |
| `artifact.jreleaserenabled` | `config.jreleaserenabled` | Namespace change |
| `container.platforms` | `config.platforms` | Namespace change |
| `container.containerfile` | `config.containerfile` | Namespace change |
| `container.enableslsa` | `config.enableslsa` | Namespace change |
| `container.enablesbom` | `config.enablesbom` | Namespace change |
| `container.enablescan` | `config.enablescan` | Namespace change |
| `container.registry` | `config.registry` | Namespace change |
| All other inputs | Unchanged | `branch`, `releasePublisher`, `changelog.*`, etc. |

## Publisher Name Changes

| v1 containerBuilder | v2 publishers |
|---------------------|---------------|
| `containerimage-ghcr` | `container-image-ghcr` |
| `containerimage-dockerhub` | `container-image-dockerhub` |

Note the dash between `container` and `image` for consistency with other publishers.

## Testing Your Migration

1. **Create a test branch:**
   ```bash
   git checkout -b test-v2-migration
   ```

2. **Update workflow file** with v2 syntax

3. **Create test tag:**
   ```bash
   git tag -s v0.0.1-test -m "Test v2 migration"
   git push origin v0.0.1-test
   ```

4. **Monitor workflow:** Check GitHub Actions for any errors

5. **Verify artifacts:** Ensure all expected artifacts are published

6. **Clean up test:**
   ```bash
   git tag -d v0.0.1-test
   git push origin :refs/tags/v0.0.1-test
   gh release delete v0.0.1-test
   ```

## Troubleshooting

### "publishers input required"

You forgot to add the `publishers` input:
```yaml
publishers: maven-app-github  # Add this
```

### "No container published but expected"

Check that your publishers list includes a container publisher:
```yaml
publishers: maven-app-github,container-image-ghcr  # Add container-image-*
```

### "config.* not recognized"

Make sure you're using `@v2` tag:
```yaml
uses: diggsweden/reusable-ci/.github/workflows/release-orchestrator.yml@v2
                                                                        ^^^
```

### "Job skipped unexpectedly"

The publisher name might be wrong. Check for typos:
- ✅ `maven-app-github` 
- ❌ `maven-github`
- ✅ `container-image-ghcr`
- ❌ `containerimage-ghcr` (v1 format)

## Rollback Strategy

If v2 doesn't work, you can always roll back to v1:

1. Revert workflow changes
2. Change `@v2` back to `@v1`
3. Push changes

v1 will remain supported (security fixes only, no new features).

## Getting Help

- Check [README.md](../README.md) for v2 examples
- Review [MONOREPO.md](MONOREPO.md) for monorepo technical details
- Open issue in reusable-ci repository with:
  - Your v1 workflow
  - Your attempted v2 workflow
  - Error messages from GitHub Actions
