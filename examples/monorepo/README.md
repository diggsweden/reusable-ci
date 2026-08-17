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
│       └── build.gradle.kts
├── apps/
│   └── frontend/
│       ├── src/
│       ├── package.json
│       └── Containerfile
├── .reusable-ci/
│   └── artifacts.yml
└── .github/
    └── workflows/
        └── release-workflow.yml
```

## Configuration Examples

This directory contains multiple monorepo configuration examples:

### 1. Basic Monorepo - [artifacts.yml](artifacts.yml)

**Use case:** Build multiple artifacts, each with their own container

**Contains:**
- Maven backend (Java 25 runtime image)
- NPM frontend (Node 24 runtime image)

**Result:** 2 containers + frontend package

---

### 2. Multi-Artifact Container - [multi-artifact-container.yml](multi-artifact-container.yml)

**Use case:** Combine multiple builds into ONE container

**Contains:**
- API service (Maven)
- Worker service (Gradle JVM)
- Web frontend (NPM)

**Result:** 1 combined container with all three artifacts

---

## How Monorepo Builds Work

```text
1. Parse artifacts.yml
   └─> Identify all artifacts and containers

2. Build Stage (parallel)
    ├─> Build backend (Maven)
   └─> Build frontend (NPM)

3. Publish Stage (parallel)
    ├─> backend container → GHCR
    ├─> frontend package → GitHub Packages
   └─> frontend container → GHCR

4. Release Stage
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
    project-type: maven
    # Maven applications publish as containers, not Maven packages.

  - name: frontend
    project-type: npm
    publish-to: [forge-packages]

  # If you need Maven shared libraries and Maven applications in the same repo,
  # use one Maven reactor/root artifact today so fixed Maven upload names do not collide.
```

### Flexible Container Dependencies
Containers reference artifacts by name:
```yaml
containers:
  # Single artifact container
  - name: backend
    from: [backend]
    context: .

  # Multi-artifact container
  - name: combined
    from: [api, worker, web]
    context: .
```

## Getting Started

### 1. Choose Your Example

**For separate containers:**
```bash
mkdir -p .reusable-ci
cp examples/monorepo/artifacts.yml .reusable-ci/artifacts.yml
```

**For combined container:**
```bash
mkdir -p .reusable-ci
cp examples/monorepo/multi-artifact-container.yml .reusable-ci/artifacts.yml
```

### 2. Customize Configuration

Update artifact names and paths to match your structure:
```yaml
artifacts:
  - name: backend
    project-type: maven
    working-directory: services/backend  # Match your structure
```

### 3. Create Workflow

```bash
cp examples/monorepo/release-workflow.yml .github/workflows/
```

### 4. Release

Configure release secrets first, including `RELEASE_TOKEN`, GPG signing secrets,
and any target-specific credentials such as Maven Central. See
[Reference Guide](../../docs/reference.md).

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
    project-type: maven
    working-directory: services/api
  - name: auth
    project-type: npm
    working-directory: services/auth
  - name: worker
    project-type: gradle
    working-directory: services/worker

containers:
  - name: api
    from: [api]
    context: .
  - name: auth
    from: [auth]
    context: .
  - name: worker
    from: [worker]
    context: .
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
    context: .
    container-file: Containerfile  # At repo root
```

### Pattern 3: Maven Modules And Applications

Model Maven modules that depend on each other as one reactor/root artifact today.
Separate Maven artifacts currently use fixed upload names and can collide.

```yaml
artifacts:
  - name: java-suite
    project-type: maven
    build-type: application
    working-directory: .

containers:
  - name: app
    from: [java-suite]
    container-file: apps/app/Containerfile
    context: .
```

## Limitations

- **Unified versioning** - All artifacts share same version
- **Single changelog** - One changelog for entire repo
- **No selective builds** - All artifacts build on every release
- **Maven upload names are fixed today** - Model multiple Maven modules as one
  Maven reactor/root artifact instead of separate Maven artifacts until
  reusable-ci threads per-artifact Maven upload names.
- **Serialized release preparation** - Artifact version bumps run one at a time;
  after all bumps succeed, the final release tag is created once at branch HEAD
  without force.

See [Artifacts Reference](../../docs/artifacts-reference.md#monorepo-configuration) for details.

## Troubleshooting

### "Artifact not found" in container build
**Problem:** Container references non-existent artifact

**Solution:** Check artifact names match exactly:
```yaml
artifacts:
  - name: my-backend  # Must match exactly
    project-type: maven

containers:
  - name: backend-container
    from: [my-backend]  # Must match artifact name
    context: .
```

### Dependencies between artifacts
**Problem:** Shared library not available during build

**Solution:** Model intra-repository dependencies in your build system. The
orchestrator fans out artifact builds by ecosystem; it does not infer arbitrary
"shared library before app" ordering. Use a Maven reactor/root build, install the
shared library in the consuming build, or publish it before consuming it.

### Container needs multiple artifacts
**Problem:** How to combine multiple builds?

**Solution:** Use multi-artifact containers:
```yaml
containers:
  - name: combined
    from: [api, worker, web]
    context: .
    container-file: Containerfile
```

Your Containerfile accesses the build outputs after `publish-container.yml`
downloads and unpacks them into the container build workspace. Do not assume the
original source working directories are preserved; inspect the workflow artifact
layout for complex monorepos.

```dockerfile
# Typical unpacked locations for the current workflow
COPY target/*api*.jar /app/api.jar
COPY build/libs/*worker*.jar /app/worker.jar
COPY dist /app/web
```

## See Also

- [Artifacts Reference](../../docs/artifacts-reference.md)
- [Publishing Guide](../../docs/publishing.md)
