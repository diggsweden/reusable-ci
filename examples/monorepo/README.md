<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Monorepo Example

Multiple artifacts built from a single repository with container support.

## Project Structure

```text
my-monorepo/
├── services/
│   ├── backend/
│   │   ├── src/
│   │   ├── pom.xml
│   │   └── Containerfile
│   └── worker/
│       ├── src/
│       └── pom.xml
├── apps/
│   └── frontend/
│       ├── src/
│       ├── package.json
│       └── Containerfile
├── libs/
│   └── shared/
│       ├── src/
│       └── pom.xml
└── .github/
    ├── artifacts.yml
    └── workflows/
        └── release-workflow.yml
```

## Configuration Examples

This directory contains multiple monorepo configuration examples:

### 1. Basic Monorepo - [artifacts.yml](artifacts.yml)

**Use case:** Build multiple artifacts, each with their own container

**Contains:**
- Maven backend (Java 21)
- NPM frontend (Node 22)
- Maven shared library (published to Maven Central)

**Result:** 2 containers + 1 library package

---

### 2. Multi-Artifact Container - [multi-artifact-container.yml](multi-artifact-container.yml)

**Use case:** Combine multiple builds into ONE container

**Contains:**
- API service (Maven)
- Worker service (Maven)
- Web frontend (NPM)

**Result:** 1 combined container with all three artifacts

---

## How Monorepo Builds Work

```text
1. Parse artifacts.yml
   └─> Identify all artifacts and containers

2. Build Stage (parallel)
   ├─> Build backend (Maven)
   ├─> Build frontend (NPM)
   └─> Build shared-lib (Maven)

3. Publish Stage (parallel)
   ├─> backend → GitHub Packages
   ├─> frontend → GitHub Packages
   └─> shared-lib → GitHub Packages + Maven Central

4. Container Stage (parallel)
   ├─> backend container (from: [backend])
   └─> frontend container (from: [frontend])

5. Release Stage
   └─> Single GitHub release with all artifacts
```

## Key Features

### Unified Versioning
All artifacts share the same version from git tag:
```bash
git tag -s v1.0.0 -m "Release v1.0.0"
# All artifacts become version 1.0.0
```

### Independent Publishing
Each artifact can publish to different targets:
```yaml
artifacts:
  - name: backend
    publish-to: [github-packages]

  - name: shared-lib
    publish-to: [github-packages, maven-central]
```

### Flexible Container Dependencies
Containers reference artifacts by name:
```yaml
containers:
  # Single artifact container
  - name: backend
    from: [backend]

  # Multi-artifact container
  - name: combined
    from: [api, worker, web]
```

## Getting Started

### 1. Choose Your Example

**For separate containers:**
```bash
cp examples/monorepo/artifacts.yml .github/
```

**For combined container:**
```bash
cp examples/monorepo/multi-artifact-container.yml .github/artifacts.yml
```

### 2. Customize Configuration

Update artifact names and paths to match your structure:
```yaml
artifacts:
  - name: backend
    working-directory: services/backend  # Match your structure
```

### 3. Create Workflow

```bash
cp examples/monorepo/release-workflow.yml .github/workflows/
```

### 4. Release

```bash
git tag -s v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

## Common Patterns

### Pattern 1: Microservices
Multiple services, each with own container:
```yaml
artifacts:
  - name: api
    working-directory: services/api
  - name: auth
    working-directory: services/auth
  - name: worker
    working-directory: services/worker

containers:
  - name: api
    from: [api]
  - name: auth
    from: [auth]
  - name: worker
    from: [worker]
```

### Pattern 2: Full-Stack Application
Frontend + Backend in one container:
```yaml
artifacts:
  - name: backend
    project-type: maven
    working-directory: backend
  - name: frontend
    project-type: npm
    working-directory: frontend

containers:
  - name: full-stack-app
    from: [backend, frontend]
    container-file: Containerfile  # At repo root
```

### Pattern 3: Shared Libraries
Publish libraries to Maven Central, apps to GitHub:
```yaml
artifacts:
  - name: core-lib
    build-type: library
    publish-to: [github-packages, maven-central]
    require-authorization: true

  - name: app
    build-type: application
    publish-to: [github-packages]
```

## Limitations

- **Unified versioning** - All artifacts share same version
- **Single changelog** - One changelog for entire repo
- **No selective builds** - All artifacts build on every release
- **Sequential version bumps** - Version files updated one at a time

See [Artifacts Reference](../../docs/artifacts-reference.md#monorepo-support) for details.

## Troubleshooting

### "Artifact not found" in container build
**Problem:** Container references non-existent artifact

**Solution:** Check artifact names match exactly:
```yaml
artifacts:
  - name: my-backend  # Must match exactly

containers:
  - name: backend-container
    from: [my-backend]  # Must match artifact name
```

### Dependencies between artifacts
**Problem:** Shared library not available during build

**Solution:** Build order is automatic. Shared libs are built first if referenced.

### Container needs multiple artifacts
**Problem:** How to combine multiple builds?

**Solution:** Use multi-artifact containers:
```yaml
containers:
  - name: combined
    from: [api, worker, web]
    container-file: Containerfile
```

Your Containerfile accesses artifacts:
```dockerfile
# Artifacts are downloaded to build context
COPY api/target/*.jar /app/api.jar
COPY worker/target/*.jar /app/worker.jar
COPY web/dist /app/web
```

## See Also

- [Artifacts Reference](../../docs/artifacts-reference.md)
- [Publishing Guide](../../docs/publishing.md)
