<!--
SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Maven Application Example

Simple Maven application with container build.

## Project Structure

```text
my-maven-app/
├── src/
│   └── main/java/...
├── pom.xml
├── Containerfile
└── .github/
    ├── artifacts.yml
    └── workflows/
        ├── pullrequest-workflow.yml
        └── release-workflow.yml
```

## Configuration Files

### `.reusable-ci/artifacts.yml`

See [artifacts.yml](artifacts.yml) in this directory.

Key points:
- Single Maven artifact
- Publishes the container image to GHCR
- Builds multi-platform container

### `.github/workflows/release-workflow.yml`

See [release-workflow.yml](release-workflow.yml) in this directory.

### `.github/workflows/pullrequest-workflow.yml`

See [pullrequest-workflow.yml](pullrequest-workflow.yml) in this directory.

## How to Use

1. **Copy files to your repository:**
   ```bash
   mkdir -p .github/workflows
   cp examples/maven-app/artifacts.yml .github/
   cp examples/maven-app/pullrequest-workflow.yml .github/workflows/
   cp examples/maven-app/release-workflow.yml .github/workflows/
   ```

2. **Customize for your project:**
   - Update `name` in artifacts.yml
   - Review any `java-version` publishing config
   - Verify `Containerfile` path
   - Configure release secrets from [Reference Guide](../../docs/reference.md), including `RELEASE_TOKEN` and GPG signing secrets
   - **Set `<project.build.outputTimestamp>` in `pom.xml` `<properties>`** — required for reproducible jars; `validate jvm-reproducibility` fails the release if missing. See [Reproducible Builds](../../docs/verification.md#reproducible-builds) for the exact snippet.

3. **Create first release:**
   ```bash
   git tag -s v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

## What Gets Built

- Maven JAR artifact → `target/*.jar`
- Container image → `ghcr.io/org/repo:v1.0.0`
- Platforms → `linux/amd64`, `linux/arm64`

## See Also

- [Artifacts Reference](../../docs/artifacts-reference.md)
- [Publishing Guide](../../docs/publishing.md)
