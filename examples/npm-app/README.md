<!--
SPDX-FileCopyrightText: 2025 The Reusable CI Authors

SPDX-License-Identifier: CC0-1.0
-->

# NPM Application Example

Simple NPM/Node.js application with container build.

## Project Structure

```text
my-npm-app/
├── src/
│   └── index.ts
├── package.json
├── tsconfig.json
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
- Single NPM artifact
- Publishes to GitHub Packages
- Builds multi-platform container

### `.github/workflows/release-workflow.yml`

See [release-workflow.yml](release-workflow.yml) in this directory.

## How to Use

1. **Copy files to your repository:**
   ```bash
   mkdir -p .github/workflows
   cp examples/npm-app/artifacts.yml .github/
   cp examples/npm-app/release-workflow.yml .github/workflows/
   ```

2. **Customize for your project:**
   - Update `name` in artifacts.yml
   - Adjust `node-version` if needed
   - Verify `Containerfile` path

3. **Create first release:**
   ```bash
   git tag -s v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

## What Gets Built

- NPM package → Built with `npm run build`
- Published to → GitHub Packages
- Container image → `ghcr.io/org/repo:v1.0.0`
- Platforms → `linux/amd64`, `linux/arm64`

## Publishing to npmjs.org

To publish to public NPM registry:

```yaml
# artifacts.yml
artifacts:
  - name: my-package
    publish-to:
      - github-packages
      - npmjs  # Add this
```

**Requirements:**
- NPM_TOKEN secret configured
- Package name must be scoped: `@org/package`

See [Publishing Guide](../../docs/publishing.md#npm-registry-npmjsorg) for details.

## See Also

- [Configuration Reference](../../docs/configuration.md)
- [Publishing Guide](../../docs/publishing.md)
- [Troubleshooting](../../docs/troubleshooting.md)
