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
├── .reusable-ci/
│   └── artifacts.yml
└── .github/
    └── workflows/
        ├── pullrequest-workflow.yml
        └── release-workflow.yml
```

## Configuration Files

### `.reusable-ci/artifacts.yml`

See [artifacts.yml](artifacts.yml) in this directory.

Key points:
- Single NPM artifact
- Publishes to GitHub Packages
- Builds multi-platform container

### `.github/workflows/release-workflow.yml`

See [release-workflow.yml](release-workflow.yml) in this directory.

### `.github/workflows/pullrequest-workflow.yml`

See [pullrequest-workflow.yml](pullrequest-workflow.yml) in this directory.

## How to Use

1. **Copy files to your repository:**
   ```bash
   mkdir -p .github/workflows .reusable-ci
   cp examples/npm-app/artifacts.yml .reusable-ci/
   cp examples/npm-app/pullrequest-workflow.yml .github/workflows/
   cp examples/npm-app/release-workflow.yml .github/workflows/
   ```

2. **Customize for your project:**
   - Update `name` in artifacts.yml
   - Review any `node-version` publishing config
   - Verify `Containerfile` path
   - Configure release secrets from [Reference Guide](../../docs/reference.md), including `RELEASE_TOKEN` and GPG signing secrets

3. **Create first release:**
   ```bash
   git tag -s v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

## What Gets Built

- NPM package → Runs `npm run build` when a `build` script exists, then packs
  the package
- Published to → GitHub Packages
- Container image → `ghcr.io/org/repo:v1.0.0`
- Platforms → `linux/amd64`, `linux/arm64`

## NPM Registry Support

Current release workflows publish NPM packages to GitHub Packages:

```yaml
# artifacts.yml
artifacts:
  - name: my-package
    project-type: npm
    publish-to:
      - forge-packages
```

npmjs.org production publishing is not implemented yet. Current config
validation rejects `npmjs`; do not add it unless you are working on that
implementation.

See [Publishing Guide](../../docs/publishing.md#npm-packages-github-packages) for details.

## See Also

- [Artifacts Reference](../../docs/artifacts-reference.md)
- [Publishing Guide](../../docs/publishing.md)
