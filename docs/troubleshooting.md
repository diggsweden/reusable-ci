<!--
SPDX-FileCopyrightText: 2025 The Reusable CI Authors

SPDX-License-Identifier: CC0-1.0
-->

# Troubleshooting Guide

Common errors and how to fix them.

## Quick Diagnosis

| Symptom | Likely Cause | Section |
|---------|--------------|---------|
| "No such file or directory" | Wrong working-directory | [Build Failures](#build-failures) |
| "Permission denied" | Missing permission in workflow | [Permission Errors](#permission-errors) |
| "Authentication failed" | Missing or invalid secret | [Authentication Errors](#authentication-errors) |
| "Tag validation failed" | Tag format wrong | [Tag Validation Errors](#tag-validation-errors) |
| "Version mismatch" | Tag doesn't match pom.xml/package.json | [Version Errors](#version-errors) |
| Build succeeds but nothing published | Missing `publish-to` configuration | [Publishing Issues](#publishing-issues) |

---

## Build Failures

### Error: "pom.xml not found" or "package.json not found"

**Symptom:**

```text
Error: Could not find pom.xml in directory: services/backend
```

**Cause:** `working-directory` in artifacts.yml points to wrong location

**Fix:**

```yaml
# Check your directory structure
artifacts:
  - name: backend
    working-directory: services/backend  # Must contain pom.xml
```

**Verify locally:**

```bash
ls -la services/backend/pom.xml
```

---

### Error: "Build failed: compilation error"

**Symptom:**

```text
[ERROR] Failed to execute goal org.apache.maven.plugins:maven-compiler-plugin
```

**Causes:**

1. Code doesn't compile
2. Wrong Java version
3. Missing dependencies

**Fix:**

1. **Test locally first:**

   ```bash
   cd services/backend
   mvn clean package
   ```

2. **Check Java version:**

   ```yaml
   config:
     java-version: 21  # Must match your pom.xml requirements
   ```

3. **Check dependencies accessible:**
   - GitHub Packages: Ensure `secrets: inherit` in workflow
   - Maven Central: Dependencies should resolve automatically

---

### Error: "npm ERR! code ELIFECYCLE"

**Symptom:**

```text
npm ERR! errno 1
npm ERR! my-app@1.0.0 build: `tsc`
```

**Causes:**

1. TypeScript compilation errors
2. Missing dependencies
3. Wrong Node version

**Fix:**

1. **Test locally:**

   ```bash
   npm ci
   npm run build
   ```

2. **Check Node version:**

   ```yaml
   config:
     node-version: 22  # Must match your package.json engines
   ```

---

## Permission Errors

### Error: "Resource not accessible by integration"

**Symptom:**

```text
HttpError: Resource not accessible by integration
Status: 403
```

**Cause:** Missing required permissions in workflow

**Fix:** Add permissions to your workflow file:

```yaml
# .github/workflows/release-workflow.yml
jobs:
  release:
    permissions:
      contents: write         # For creating releases
      packages: write         # For publishing artifacts
      id-token: write        # For SLSA attestation
      actions: read          # For reading workflow info
      security-events: write # For security scans
      attestations: write    # For SBOM attestation
```

---

### Error: "Failed to create release"

**Symptom:**

```text
Error: Failed to create GitHub release: 403 Forbidden
```

**Cause:** Missing `contents: write` permission

**Fix:**

```yaml
permissions:
  contents: write  # Required for gh release create
```

---

## Authentication Errors

### Error: "Failed to publish to GitHub Packages"

**Symptom:**

```text
401 Unauthorized: GitHub Packages authentication failed
```

**Causes:**

1. Missing `secrets: inherit`
2. Missing `packages: write` permission
3. Token expired (rare)

**Fix:**

```yaml
# .github/workflows/release-workflow.yml
jobs:
  release:
    secrets: inherit  # Pass org-level secrets
    permissions:
      packages: write  # Required for publishing
```

---

### Error: "Failed to publish to Maven Central"

**Symptom:**

```text
Failed to execute goal: Authentication failed for Sonatype OSSRH
```

**Causes:**

1. Missing `MAVENCENTRAL_USERNAME` or `MAVENCENTRAL_PASSWORD` secrets
2. Invalid credentials
3. Repository not enabled for secrets

**Fix:**

1. **Request secrets from DiggSweden administrators:**

```text
   Required secrets:
   - MAVENCENTRAL_USERNAME
   - MAVENCENTRAL_PASSWORD
```

2. **Verify configuration:**

   ```yaml
   artifacts:
     - name: my-lib
       build-type: library  # Required for Maven Central
       publish-to:
         - maven-central
   ```

3. **Check Sonatype account:**
   - Login at <https://central.sonatype.com/>
   - Verify credentials work
   - Ensure groupId is approved

---

### Error: "NPM authentication failed"

**Symptom:**

```text
npm ERR! code E401
npm ERR! Unable to authenticate
```

**Cause:** Missing or invalid `NPM_TOKEN` secret

**Fix:**

1. **Request NPM_TOKEN from administrators**

2. **Verify configuration:**

   ```yaml
   publish-to:
     - npmjs
   ```

3. **Regenerate token if expired:**
   - <https://www.npmjs.com/settings/tokens>
   - Type: "Automation"

---

## Tag Validation Errors

### Error: "Tag format invalid"

**Symptom:**

```text
Error: Tag 'v1.0' does not match required format
```

**Cause:** Tag doesn't follow semantic versioning

**Valid formats:**

- `v1.0.0` ✅
- `v1.0.0-alpha.1` ✅
- `v1.0.0-beta.1` ✅
- `v1.0.0-rc.1` ✅
- `v1.0.0-SNAPSHOT` ✅

**Invalid formats:**

- `v1.0` ❌ (missing patch version)
- `1.0.0` ❌ (missing 'v' prefix)
- `v1.0.0-dev` ❌ (dev suffix not allowed)

**Fix:**

```bash
# Delete wrong tag
git tag -d v1.0
git push origin :refs/tags/v1.0

# Create correct tag
git tag -s v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

---

### Error: "Tag must be signed"

**Symptom:**

```text
Error: Tag 'v1.0.0' is not cryptographically signed
```

**Cause:** Tag created without GPG/SSH signature

**Fix:**

**Use GPG signing:**

```bash
# Create signed tag
git tag -s v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

**Or use SSH signing:**

```bash
# Configure git to use SSH
git config gpg.format ssh
git config user.signingkey ~/.ssh/id_ed25519.pub

# Create signed tag
git tag -s v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

---

### Error: "Tag must be annotated"

**Symptom:**

```text
Error: Tag 'v1.0.0' is a lightweight tag (not annotated)
```

**Cause:** Tag created without `-a` or `-s` flag

**Fix:**

```bash
# Delete lightweight tag
git tag -d v1.0.0
git push origin :refs/tags/v1.0.0

# Create annotated signed tag
git tag -s v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

---

## Version Errors

### Error: "Version mismatch"

**Symptom:**

```text
Error: Tag version 'v1.0.0' does not match project version '1.0.1'
```

**Cause:** Git tag doesn't match version in pom.xml or package.json

**Fix:**

**Option 1: Update project version**

```bash
# For Maven
mvn versions:set -DnewVersion=1.0.0

# For NPM
npm version 1.0.0 --no-git-tag-version

# Commit and tag
git add pom.xml  # or package.json
git commit -m "Bump version to 1.0.0"
git tag -s v1.0.0 -m "Release v1.0.0"
git push origin main v1.0.0
```

**Option 2: Fix tag**

```bash
# Delete wrong tag
git tag -d v1.0.0
git push origin :refs/tags/v1.0.0

# Create correct tag
git tag -s v1.0.1 -m "Release v1.0.1"
git push origin v1.0.1
```

---

## Publishing Issues

### Build succeeds but nothing published

**Symptom:**

- Workflow shows green checkmark
- No artifacts in GitHub Packages
- No Maven Central release

**Cause:** Missing or incorrect `publish-to` configuration

**Fix:**

```yaml
# .github/artifacts.yml
artifacts:
  - name: my-app
    project-type: maven
    working-directory: .
    publish-to:              # Add this!
      - github-packages
      - maven-central        # If library
```

**Default behavior:** If `publish-to` is omitted, only publishes to `github-packages`

---

### Container builds but not pushed

**Symptom:**

- Container builds successfully
- No image in ghcr.io

**Causes:**

1. Missing `packages: write` permission
2. Push disabled in dev workflow
3. Registry authentication failed

**Fix for production:**

```yaml
permissions:
  packages: write  # Required for container push
```

**Note:** Dev workflows may skip container push intentionally.

---

## Container Errors

### Error: "Containerfile not found"

**Symptom:**

```text
Error: Containerfile not found at path: Containerfile
```

**Cause:** Path in `container-file` is wrong

**Fix:**

```yaml
containers:
  - name: my-app
    container-file: Containerfile  # Must exist at repo root
    # Or specify relative path
    container-file: services/backend/Containerfile
```

**Verify:**

```bash
ls -la Containerfile
# or
ls -la services/backend/Containerfile
```

---

### Error: "FROM reference not found"

**Symptom:**

```text
Error: Artifact 'backend-api' referenced in container 'my-app' does not exist
```

**Cause:** Container `from:` references non-existent artifact

**Fix:**

```yaml
artifacts:
  - name: backend-api  # Must match exactly

containers:
  - name: my-app
    from: [backend-api]  # Must match artifact name
```

---

### Error: "Multi-platform build failed"

**Symptom:**

```text
Error: failed to solve: no match for platform linux/arm64
```

**Causes:**

1. Base image doesn't support arm64
2. Dependencies not available for arm64

**Fix:**

**Option 1: Use multi-platform base image**

```dockerfile
FROM eclipse-temurin:21-jre  # Supports amd64 and arm64
```

**Option 2: Build single platform**

```yaml
containers:
  - name: my-app
    platforms: linux/amd64  # Remove arm64
```

---

## Workflow Errors

### Error: "artifacts.yml not found"

**Symptom:**

```text
Error: Configuration file not found: .github/artifacts.yml
```

**Causes:**

1. File doesn't exist
2. Wrong path in workflow
3. Typo in filename

**Fix:**

1. **Create the file:**

   ```bash
   mkdir -p .github
   touch .github/artifacts.yml
   ```

2. **Verify path in workflow:**

   ```yaml
   with:
     artifacts-config: .github/artifacts.yml  # Must match actual path
   ```

---

### Error: "Invalid YAML syntax"

**Symptom:**

```text
Error: Invalid artifacts configuration: YAML parse error
```

**Cause:** Syntax error in artifacts.yml

**Common mistakes:**

```yaml
# Wrong: using tabs
artifacts:
 - name: my-app  # ❌ Don't use tabs

# Correct: use spaces
artifacts:
  - name: my-app  # ✅ Use 2 spaces
```

**Validate locally:**

```bash
# Install yamllint
pip install yamllint

# Check syntax
yamllint .github/artifacts.yml
```

---

## Secret Issues

### Error: "Secret not found"

**Symptom:**

```text
Warning: Input 'MAVENCENTRAL_USERNAME' is empty
```

**Cause:** Repository not enabled for org-level secrets

**Fix:**

1. **Request access from DiggSweden administrators:**
   - Specify which secrets you need
   - Provide repository name
   - Explain use case

2. **Verify secrets added:**
   - Go to repository Settings → Secrets and variables → Actions
   - Check if org-level secrets are listed

---

## Performance Issues

### Builds are very slow

**Symptom:** Builds take >15 minutes

**Causes:**

1. No dependency caching
2. Building unnecessary platforms
3. Slow network to dependencies

**Optimizations:**

1. **Ensure caching enabled** (should be automatic)

2. **Reduce platforms if not needed:**

   ```yaml
   containers:
     - platforms: linux/amd64  # Faster than amd64,arm64
   ```

3. **Use closer mirrors:**

   ```xml
   <!-- pom.xml - use European mirrors -->
   <repositories>
     <repository>
       <id>central</id>
       <url>https://repo.maven.apache.org/maven2</url>
     </repository>
   </repositories>
   ```

---

## Getting Help

### Workflow Logs

View detailed logs in GitHub Actions:

1. Go to repository → Actions tab
2. Click failed workflow run
3. Click failed job
4. Expand step to see error details

### Enable Debug Logging

Add to workflow:

```yaml
env:
  ACTIONS_STEP_DEBUG: true  # Verbose logging
  ACTIONS_RUNNER_DEBUG: true
```

### Check Workflow Status

```bash
# List recent workflow runs
gh run list --repo diggsweden/your-repo

# View specific run
gh run view RUN_ID --log
```

### Ask for Help

If still stuck:

1. Check this documentation
2. Search existing issues
3. Open new issue with:
   - Full error message
   - Link to failed workflow run
   - Your artifacts.yml
   - Steps to reproduce

---

## See Also

- [Configuration Reference](configuration.md) - Complete field documentation
- [Publishing Guide](publishing.md) - Registry setup instructions
- [Workflows Guide](workflows.md) - Workflow configuration
