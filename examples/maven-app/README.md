<!--
SPDX-FileCopyrightText: 2025 The Reusable CI Authors

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
        └── release-workflow.yml
```

## Configuration Files

### `.github/artifacts.yml`

See [artifacts.yml](artifacts.yml) in this directory.

Key points:
- Single Maven artifact
- Publishes to GitHub Packages
- Builds multi-platform container

### `.github/workflows/release-workflow.yml`

See [release-workflow.yml](release-workflow.yml) in this directory.

## How to Use

1. **Copy files to your repository:**
   ```bash
   mkdir -p .github/workflows
   cp examples/maven-app/artifacts.yml .github/
   cp examples/maven-app/release-workflow.yml .github/workflows/
   ```

2. **Customize for your project:**
   - Update `name` in artifacts.yml
   - Adjust `java-version` if needed
   - Verify `Containerfile` path

3. **Create first release:**
   ```bash
   git tag -s v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

## What Gets Built

- Maven JAR artifact → `target/*.jar`
- Published to → GitHub Packages
- Container image → `ghcr.io/org/repo:v1.0.0`
- Platforms → `linux/amd64`, `linux/arm64`

## See Also

- [Artifacts Reference](../../docs/artifacts-reference.md)
- [Publishing Guide](../../docs/publishing.md)
