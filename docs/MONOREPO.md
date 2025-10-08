# Monorepo Support - Technical Documentation

## Overview

Reusable CI v2 introduces monorepo support, allowing a single repository to build and publish multiple artifacts with different project types (Maven, NPM, etc.).

## Key Features

- **Backward Compatible**: All v1 workflows continue to work unchanged
- **Flexible Configuration**: Define artifacts in external file or inline YAML
- **Same Field Names**: Artifact configs use identical field names as v1
- **Parallel Builds**: Multiple artifacts build in parallel where possible
- **Unified Release**: Single changelog and GitHub release for all artifacts

## Architecture

### Mode Detection

The orchestrator detects which mode to use based on inputs:

1. **Monorepo mode**: If `artifactsConfig` or `artifacts` is specified
2. **Single-repo mode (v1)**: Otherwise, uses traditional inputs

### Workflow Jobs

#### 1. detect-mode
- Loads artifacts from config file or inline YAML
- Converts v1 inputs to single-artifact array (for compatibility)
- Outputs artifacts JSON array for consumption by other jobs

#### 2. version-and-changelog (matrix)
- Iterates over all artifacts using GitHub Actions matrix strategy
- Bumps version in each artifact's version file (pom.xml, package.json)
- Uses artifact-specific settings (Java version, Node version, etc.)

#### 3. publish-maven-app (matrix)
- Iterates over artifacts where `artifactPublisher == 'maven-app-github'`
- Publishes each Maven artifact to GitHub Packages
- Runs in parallel for all matching artifacts

#### 4. publish-npm-app (matrix)
- Iterates over artifacts where `artifactPublisher == 'npm-app-github'`
- Publishes each NPM artifact to GitHub Packages
- Runs in parallel for all matching artifacts

#### 5. publish-maven-lib (matrix)
- Iterates over artifacts where `artifactPublisher == 'maven-lib-mavencentral'`
- Publishes each Maven library to Maven Central
- Runs in parallel for all matching artifacts

#### 6. publish-container (single)
- Runs once after all artifacts are published
- Container can use artifacts from all previous builds
- Same configuration as v1

#### 7. create-release (single)
- Creates single GitHub release for all artifacts
- Attaches artifacts from all builds
- Generates unified changelog

## Configuration Format

### artifacts.yml Structure

```yaml
artifacts:
  - name: string              # Required: Unique identifier
    projectType: string       # Required: maven, npm, gradle, python
    workingDirectory: string  # Required: Path to artifact source
    artifactPublisher: string # Required: maven-app-github, npm-app-github, etc.
    artifact:                 # Optional: Artifact-specific configuration
      javaversion: string     # Java version (Maven)
      nodeversion: string     # Node version (NPM)
      attachpattern: string   # Files to attach to release
      npmtag: string          # NPM dist tag
      settingspath: string    # Maven settings path
      jreleaserenabled: bool  # Enable JReleaser plugin
```

### Field Mapping: v1 → v2

| v1 Input | v2 Location | Example |
|----------|-------------|---------|
| `projectType` | `artifacts[].projectType` | `maven` |
| `artifactPublisher` | `artifacts[].artifactPublisher` | `maven-app-github` |
| `workingDirectory` | `artifacts[].workingDirectory` | `backend` |
| `artifact.javaversion` | `artifacts[].artifact.javaversion` | `21` |
| `artifact.nodeversion` | `artifacts[].artifact.nodeversion` | `22` |
| `containerBuilder` | `containerBuilder` (root level) | `containerimage-ghcr` |
| `changelogCreator` | `changelogCreator` (root level) | `git-cliff` |

## Implementation Details

### Matrix Strategy

GitHub Actions matrix strategy is used to parallelize artifact builds:

```yaml
strategy:
  matrix:
    artifact: ${{ fromJson(needs.detect-mode.outputs.artifacts) }}
  fail-fast: false
```

Each job filters the matrix using conditions:

```yaml
if: ${{ matrix.artifact.artifactPublisher == 'maven-app-github' }}
```

This ensures:
- Maven jobs only process Maven artifacts
- NPM jobs only process NPM artifacts
- Jobs with no matching artifacts are skipped

### Fallback Values

Artifact-specific values override root-level v1 inputs:

```yaml
javaVersion: ${{ matrix.artifact.artifact.javaversion || inputs['artifact.javaversion'] }}
```

This allows:
- Per-artifact configuration in monorepo mode
- Global defaults from v1 inputs as fallback

### Version Management

In monorepo mode:
- All artifacts share the same version (from git tag)
- Each artifact's version file is updated independently
- Single changelog commit includes all version file changes

## Migration Guide

### From v1 to v2 (Keeping Single-Repo)

No changes needed! v2 is fully backward compatible.

### From v1 Single-Repo to v2 Monorepo

1. **Create artifacts.yml:**
   ```yaml
   artifacts:
     - name: app
       projectType: maven
       workingDirectory: .
       artifactPublisher: maven-app-github
       artifact:
         javaversion: 21
   ```

2. **Update workflow:**
   ```yaml
   # Remove projectType and artifactPublisher from with:
   with:
     artifactsConfig: .github/artifacts.yml  # Add this
     containerBuilder: containerimage-ghcr   # Keep these
     changelogCreator: git-cliff
   ```

### Converting Existing Monorepo

If you already have a monorepo structure:

1. **Identify artifacts:**
   - List all buildable components
   - Determine project type for each
   - Choose appropriate publisher

2. **Create artifacts.yml:**
   ```yaml
   artifacts:
     - name: backend
       projectType: maven
       workingDirectory: services/backend
       artifactPublisher: maven-app-github
     
     - name: frontend
       projectType: npm
       workingDirectory: packages/frontend
       artifactPublisher: npm-app-github
   ```

3. **Update Containerfile:**
   - Ensure it copies from correct artifact paths
   - Example:
     ```dockerfile
     COPY --from=builder services/backend/target/*.jar app.jar
     COPY --from=builder packages/frontend/dist /app/static
     ```

## Limitations & Future Work

### Current Limitations

1. **Unified Versioning**: All artifacts must share the same version
2. **No Change Detection**: All artifacts build on every release
3. **Sequential Version Bumps**: Artifacts version-bump one at a time
4. **Single Changelog**: No per-artifact changelogs

### Planned Enhancements (v3)

1. **Independent Versioning**: Each artifact tracks its own version
2. **Change Detection**: Only build artifacts with changes
3. **Dependency Graphs**: Artifact dependencies (e.g., backend depends on shared-lib)
4. **Parallel Version Bumps**: Bump all versions simultaneously
5. **Scoped Changelogs**: Per-artifact changelog sections

## Examples

See:
- `examples/monorepo-artifacts.yml` - Example configuration
- `eudiw-wallet-issuer-poc/.github/artifacts.yml.example` - Real-world example
- `eudiw-wallet-issuer-poc/.github/workflows/release-workflow-monorepo.yml.example` - Example workflow

## Troubleshooting

### "artifactsConfig specified but file not found"

Ensure the file path is correct and relative to repository root:
```yaml
artifactsConfig: .github/artifacts.yml  # Correct
artifactsConfig: artifacts.yml          # Wrong (unless in root)
```

### "No 'artifacts' key found"

Your YAML file must have top-level `artifacts:` key:
```yaml
artifacts:  # Required
  - name: ...
```

### "Invalid artifacts YAML provided"

When using inline `artifacts:`, ensure proper YAML syntax:
```yaml
artifacts: |
  - name: backend
    projectType: maven
    # etc...
```

### Jobs skipped unexpectedly

Check that `artifactPublisher` values match expected types:
- `maven-app-github` (not `maven-github`)
- `npm-app-github` (not `npm-github`)

### Matrix job names unclear

Each job shows artifact name in title:
```
Publish Maven Application - backend
Publish NPM Application - frontend
```

## Testing

To test monorepo support:

1. Create test artifacts.yml with 2-3 artifacts
2. Create version tag: `git tag -s v0.1.0-test -m "Test release"`
3. Push tag: `git push origin v0.1.0-test`
4. Monitor workflow runs in GitHub Actions
5. Verify all artifacts published successfully
6. Check GitHub release has all artifacts attached

## Support

For issues or questions:
- Open issue in reusable-ci repository
- Reference this documentation in bug reports
- Include your artifacts.yml and workflow file
