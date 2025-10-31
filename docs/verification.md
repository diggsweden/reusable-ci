<!--
SPDX-FileCopyrightText: 2025 The Reusable CI Authors

SPDX-License-Identifier: CC0-1.0
-->

# Verification Guide

Documentation for verifying code quality and the authenticity and integrity of artifacts produced by DiggSweden workflows.

## Code Quality Verification

### Just+Mise Linting Workflow

The `lint-just-mise.yml` workflow provides dynamic, MegaLinter-style output for code quality verification.

#### Overview

This lightweight linting workflow automatically discovers and runs linting tasks defined in your project's `justfile`, providing rich formatted output in GitHub Actions similar to MegaLinter but without the overhead.

**Key Features:**
- **Dynamic Discovery**: Automatically finds all `lint-*` tasks in your justfile
- **Rich Output**: Individual linter results with pass/fail status, timing, and error details
- **Zero Configuration**: No need to specify which linters to run - adapts automatically
- **Excludes Fix Tasks**: Automatically skips `*-fix` tasks (e.g., `lint-yaml-fix`)
- **MegaLinter-Style UI**: Formatted markdown tables in GitHub Actions Summary tab

#### Requirements

Your project must have:
1. **justfile** with tasks named `lint-*` (e.g., `lint-java`, `lint-markdown`, `lint-yaml`)
2. **.mise.toml** with required tools specified
3. **install** task in justfile to set up tools via mise

#### Usage

Add to your pull request workflow:

```yaml
jobs:
  lint:
    uses: diggsweden/reusable-ci/.github/workflows/lint-just-mise.yml@main
    permissions:
      contents: read
      security-events: write
```

#### Example Justfile Structure

```just
# Install development tools
install:
    mise install

# Run all linters
lint: lint-java lint-markdown lint-yaml lint-actions lint-shell lint-secrets

# Individual linter tasks (auto-discovered)
# Lint Java code
lint-java:
    mvn checkstyle:check pmd:check spotbugs:check

# Lint markdown files
lint-markdown:
    rumdl check .

# Lint YAML files
lint-yaml:
    yamlfmt -lint .

# Lint GitHub Actions
lint-actions:
    actionlint

# Lint shell scripts
lint-shell:
    find . -name '*.sh' | xargs shellcheck

# Scan for secrets
lint-secrets:
    gitleaks detect --no-banner

# Fix tasks (automatically excluded from CI)
lint-yaml-fix:
    yamlfmt .

lint-markdown-fix:
    rumdl check --fix .
```

#### Linter Metadata (Optional)

To customize linter names in GitHub UI, add metadata comments before `lint-*` tasks:

```just
# linter-name: Java Code Quality
# linter-tools: checkstyle, pmd, spotbugs
# Lint Java code (via Maven plugins)
lint-java:
    @mvn checkstyle:check pmd:check spotbugs:check
```

**Order matters**: The last comment is the recipe description shown by `just --list`:

```text
$ just --list
...
lint-java          # Lint Java code (via Maven plugins)
...
```

Both metadata lines optional. Defaults to task name if missing.

#### GitHub Actions Output

The workflow generates a formatted summary showing:

```markdown
# 🔍 Just+Mise Linting Results

**Linters Run:** 8
**Started:** 2025-10-30 16:45:12 UTC

## Individual Linter Results

| Linter   | Status  | Duration | Details              |
|----------|---------|----------|----------------------|
| actions  | ✅ Pass | 0.03s    | Success              |
| java     | ✅ Pass | 10.81s   | Success              |
| markdown | ❌ Fail | 0.01s    | [View errors below]  |
| yaml     | ✅ Pass | 0.15s    | Success              |

## ❌ Failed Linters

<details>
<summary>❌ markdown - Click to expand error details</summary>

**Exit code:** 1
**Duration:** 0.01s

### Output:
```
README.md:45 - Line too long (found 120, expected 100)
```text
</details>

---

### Summary

**Total Duration:** 11.89s
**Pass:** ✅ 6 | **Fail:** ❌ 2
```

#### How It Works

1. **Discovery**: Script discovers all tasks matching `lint-*` pattern (excludes `*-fix`)
2. **Execution**: Each linter runs individually with output captured
3. **Timing**: Duration tracked per linter
4. **Summary**: Results formatted as markdown table in GitHub Actions Summary tab
5. **Errors**: Failed linters show expandable error details

#### Adding/Removing Linters

Simply add or remove `lint-*` tasks in your justfile - no workflow changes needed:

```just
# Add a new linter - automatically discovered
lint-rust:
    cargo clippy -- -D warnings

# Remove by deleting the task - automatically excluded
```

## Artifact Verification Methods

| Artifact Type | Verification Methods | Security Level | What It Proves |
|--------------|---------------------|----------------|----------------|
| **Container Images** | Cosign signatures, SLSA provenance, SBOM attestations | High/Maximum | Built by official CI, unmodified, with traceable dependencies |
| **Maven JARs** | GPG signatures, checksums | High | Signed, unchanged since publication |
| **NPM Packages** | NPM provenance, signatures | High | Package integrity and build authenticity |
| **Release Assets** | GPG signatures, SHA256 checksums | High | Authentic release files from official builds |
| **Git Tags** | GPG/SSH signatures | High | Release tags created by authorized developers |
| **Git Commits** | GPG/SSH signatures | High | Commits made by verified developers |

All verification methods use industry-standard cryptographic signatures and attestations.

## Purpose

Artifact verification prevents tampering and validates authenticity in CI/CD pipelines.

## Software Bill of Materials (SBOM) Strategy

DiggSweden workflows generate comprehensive multi-layer SBOMs for complete supply chain transparency and compliance with international standards.

### 3-Layer SBOM Architecture

Every release includes **three layers** of SBOMs, each in **two formats** (SPDX + CycloneDX):

| Layer | Source | Captures | Use Case | Formats |
|-------|--------|----------|----------|---------|
| **POM** | `pom.xml`, `build.gradle`, `package.json` | Declared dependencies + transitive dependencies | License compliance, dependency analysis, build-time security | SPDX 2.3, CycloneDX 1.6 |
| **JAR** | JAR binaries (may be multiple) | Actual packaged libraries (including shaded deps) | Runtime dependency verification, binary analysis | SPDX 2.3, CycloneDX 1.6 |
| **Container** | Container image | OS packages, JRE, runtime environment | Deployment security, runtime vulnerability scanning | SPDX 2.3, CycloneDX 1.6 |

**Total SBOMs per release:** 6-10+ files (3+ layers × 2 formats, more if multiple JARs)

### SBOM Naming Convention

SBOMs follow a consistent, versioned naming scheme with explicit `-sbom` suffix:

```text
{jar-filename}-jar-sbom.{format}.json

Examples:
- PROJECT-VERSION-pom-sbom.spdx.json
- PROJECT-VERSION-pom-sbom.cyclonedx.json
- PROJECT-VERSION-jar-sbom.spdx.json        (library JAR)
- PROJECT-jar-sbom.spdx.json              (fat/executable JAR)
- PROJECT-VERSION-container-sbom.spdx.json
- PROJECT-VERSION-container-sbom.cyclonedx.json
```

**Multiple JAR Artifacts:**

Maven/Spring Boot projects may produce multiple JARs, each with its own SBOM:

| JAR Type | Filename | SBOM Size | Dependencies | Use Case |
|----------|----------|-----------|--------------|----------|
| **Library JAR** | `app-1.0.0.jar` | Small (5-10 KB) | ~2 packages (application code only) | Library consumers, Maven dependency |
| **Fat/Uber JAR** | `app.jar` | Large (500+ KB) | 100+ packages (all embedded deps) | Deployment, security scanning, runtime analysis |

The fat JAR SBOM shows the dependency tree deployed to production.

### Compliance & Standards Alignment

DiggSweden SBOM generation meets the following standards:

- ✅ **[NTIA Minimum Elements for SBOM](https://www.ntia.gov/sites/default/files/publications/sbom_minimum_elements_report_0.pdf)** - All SBOMs include supplier name, component name, version, dependencies, and unique identifiers
- ✅ **[CISA SBOM Requirements](https://www.cisa.gov/sbom)** - Compliant with CISA guidance for federal cybersecurity
- ✅ **[EU Cyber Resilience Act (CRA)](https://digital-strategy.ec.europa.eu/en/policies/cyber-resilience-act)** - Aligned with EU requirements for software transparency and security
- ✅ **[SLSA Level 3 Provenance](https://slsa.dev/spec/v1.0/levels)** - Container SBOMs include cryptographically signed build provenance attestations

### SBOM Delivery & Access

SBOMs are delivered in two ways:

#### 1. SBOM Archive (GitHub Release Asset)

All SBOMs packaged in a signed ZIP archive:

```bash
# Download complete SBOM package
gh release download v1.0.0 -p "*-sboms.zip"
gh release download v1.0.0 -p "*-sboms.zip.asc"

# Verify GPG signature
gpg --verify PROJECT-VERSION-sboms.zip.asc

# Extract all SBOMs
unzip PROJECT-VERSION-sboms.zip
```

Contents include:
- POM/build file SBOM (SPDX + CycloneDX)
- JAR artifact SBOM (SPDX + CycloneDX)
- Container image SBOM (SPDX + CycloneDX)

#### 2. Container Image Attestation (Runtime Layer)

Container SBOM attached as signed attestation:

```bash
# Verify and download container SBOM attestation
cosign verify-attestation \
  --type spdx \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp "^https://github.com/diggsweden/PROJECT" \
  ghcr.io/diggsweden/PROJECT:v1.0.0

# Extract SBOM from attestation
cosign download attestation \
  --predicate-type https://spdx.dev/Document \
  ghcr.io/diggsweden/PROJECT:v1.0.0 | \
  jq -r '.payload' | base64 -d | jq '.predicate' > container-sbom.spdx.json
```

#### 3. Checksum Verification

SBOM archive included in release checksums:

```bash
# Download checksums and signature
gh release download v1.0.0 -p "checksums.sha256*"

# Verify GPG signature on checksums
gpg --verify checksums.sha256.asc checksums.sha256

# Verify SBOM archive integrity
sha256sum -c checksums.sha256 --ignore-missing | grep sboms.zip
```

### Using SBOMs for Security Analysis

SBOMs are generated with **Syft** and scanned with **Trivy** (the same tools used by CI/CD workflows).

#### Vulnerability Scanning with Trivy

Trivy scans SBOMs for vulnerabilities and is used by the CI/CD workflows.

```bash
# Scan POM layer (declared dependencies)
trivy sbom PROJECT-VERSION-pom-sbom.spdx.json

# Scan JAR layer - library JAR (application code only)
trivy sbom PROJECT-VERSION-jar-sbom.spdx.json

# Scan JAR layer - fat JAR (all embedded dependencies)
trivy sbom PROJECT-jar-sbom.cyclonedx.json

# Scan container layer (runtime environment)
trivy sbom PROJECT-VERSION-container-sbom.spdx.json

# Scan with severity filtering
trivy sbom --severity HIGH,CRITICAL PROJECT-VERSION-container-sbom.spdx.json

# Output as JSON for processing
trivy sbom -f json -o vulnerabilities.json PROJECT-VERSION-jar-sbom.spdx.json
```

#### License Compliance Analysis

```bash
# Extract license information from SBOM
jq '.packages[] | {name: .name, version: .versionInfo, license: .licenseConcluded}' \
  PROJECT-VERSION-pom-sbom.spdx.json

# Scan for license issues with Trivy
trivy sbom --scanners license PROJECT-VERSION-pom-sbom.spdx.json
```

#### Dependency Graph Visualization with Syft

Syft generates SBOMs and can display dependency information.

```bash
# Generate dependency tree from SBOM
syft packages PROJECT-VERSION-jar-sbom.spdx.json -o table

# Export to dependency graph format
syft packages PROJECT-VERSION-pom-sbom.cyclonedx.json -o json | \
  jq '.artifacts[] | {name: .name, version: .version, type: .type}'
```

### SBOM Generation Workflow

SBOMs are generated automatically during the release process:

1. **Container Build** → Generates container SBOM (2 formats)
2. **Maven/NPM Build** → Generates POM + JAR SBOMs (4 formats)
3. **Release Step** → Downloads container SBOM, packages all SBOMs into ZIP
4. **Signing** → GPG signs SBOM archive and checksums
5. **Upload** → ZIP archive to GitHub Release
6. **Attestation** → Container SBOM attached to image via Cosign

### SBOM Format Comparison

| Feature | SPDX 2.3 | CycloneDX 1.6 |
|---------|----------|---------------|
| **Standards Body** | Linux Foundation | OWASP |
| **Primary Use** | License compliance, legal | Security, vulnerability management |
| **Vulnerability Mapping** | CPE, PURL | CPE, PURL, SWID |
| **License Expression** | SPDX License List | SPDX License List |
| **Tool Ecosystem** | Broader legal/compliance tools | Security-focused tools (Dependency-Track) |
| **Government Adoption** | NTIA recommended | CISA recommended |

**DiggSweden provides both** to maximize compatibility with downstream tools.

### References & Further Reading

- [NTIA SBOM Minimum Elements](https://www.ntia.gov/sites/default/files/publications/sbom_minimum_elements_report_0.pdf)
- [CISA SBOM Guidance](https://www.cisa.gov/sbom)
- [EU Cyber Resilience Act](https://digital-strategy.ec.europa.eu/en/policies/cyber-resilience-act)
- [SPDX Specification 2.3](https://spdx.github.io/spdx-spec/v2.3/)
- [CycloneDX Specification 1.6](https://cyclonedx.org/specification/overview/)
- [SLSA Framework](https://slsa.dev/)
- [Syft SBOM Generator](https://github.com/anchore/syft)
- [Trivy Vulnerability Scanner](https://github.com/aquasecurity/trivy)

## Full Verification Guide

### 1. Container Image Verification

Verifies container authenticity and DiggSweden CI build origin.

#### Verify Container Signature

```bash
# Set project and version
PROJECT="your-project-name"
VERSION="v1.0.0"

# Verify image signature (keyless signing via GitHub OIDC)
# Containers are signed via SLSA generator workflow, so identity is from slsa-framework
cosign verify \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp "^https://github.com/diggsweden/${PROJECT}" \
  ghcr.io/diggsweden/${PROJECT}:${VERSION}
```

#### Verify SLSA Provenance

**Using slsa-verifier (Recommended - Simple):**

```bash
# Install slsa-verifier
gh release download -R slsa-framework/slsa-verifier

# Verify container image
slsa-verifier verify-image ghcr.io/diggsweden/my-app:v1.0.0 \
  --source-uri github.com/diggsweden/my-app
```

**Using cosign (Advanced - Detailed):**

```bash
# Verify SLSA Level 3 provenance attestation
# SLSA attestations are created by slsa-framework/slsa-github-generator, not the repository itself
cosign verify-attestation \
  --type slsaprovenance \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/slsa-framework/slsa-github-generator' \
  ghcr.io/diggsweden/${PROJECT}:${VERSION}

# View the attestation content
cosign verify-attestation \
  --type slsaprovenance \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/slsa-framework/slsa-github-generator' \
  ghcr.io/diggsweden/${PROJECT}:${VERSION} | jq -r '.payload' | base64 -d | jq
```

#### Verify SBOM Attestation

```bash
# Verify SBOM attestation
cosign verify-attestation \
  --type cyclonedx \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp "^https://github.com/diggsweden/${PROJECT}" \
  ghcr.io/diggsweden/${PROJECT}:${VERSION}

# Download and inspect SBOM
cosign download attestation \
  --predicate-type cyclonedx \
  ghcr.io/diggsweden/${PROJECT}:${VERSION} | jq -r '.payload' | base64 -d > sbom.json
```

### 2. Maven Artifact Verification

Verifies OSPO_BOT signature and artifact integrity.

#### Verify GPG Signature

```bash
# Import DiggSwedenBot public key
curl -sSfL https://github.com/diggsweden/.github/raw/main/pubkey/ospo.digg.pub.asc -o ospo.digg.pub.asc

# Verify fingerprint before importing
gpg --show-keys ospo.digg.pub.asc
# Expected: 94DC AF60 8AA5 3E16 4F94 F2C8 5D23 336A 384E D816

gpg --import ospo.digg.pub.asc

# Download artifact and signature from GitHub Packages
curl -H "Authorization: token GITHUB_TOKEN" \
  -L https://maven.pkg.github.com/diggsweden/${PROJECT}/${ARTIFACT}/${VERSION}/${ARTIFACT}-${VERSION}.jar \
  -o ${ARTIFACT}.jar

curl -H "Authorization: token GITHUB_TOKEN" \
  -L https://maven.pkg.github.com/diggsweden/${PROJECT}/${ARTIFACT}/${VERSION}/${ARTIFACT}-${VERSION}.jar.asc \
  -o ${ARTIFACT}.jar.asc

# Verify signature
gpg --verify ${ARTIFACT}.jar.asc ${ARTIFACT}.jar
```

### 3. NPM Package Verification

Verifies package integrity and build provenance.

```bash
# Verify NPM package provenance (npm 9.5+ required)
npm audit signatures @diggsweden/${PACKAGE}

# View package attestations
npm view @diggsweden/${PACKAGE} --json | jq '.attestations'
```

### 4. Release Artifact Verification

Verifies release artifact authenticity and signatures.

#### Download and Verify Release Assets

```bash
# Set version
VERSION="v1.0.0"

# Download release asset and checksums
gh release download ${VERSION} -p "*.tar.gz"
gh release download ${VERSION} -p "checksums.sha256"
gh release download ${VERSION} -p "checksums.sha256.asc"

# Verify GPG signature on checksums
gpg --verify checksums.sha256.asc checksums.sha256

# Verify file integrity
sha256sum -c checksums.sha256 --ignore-missing
```

### 5. SSH Key Setup for Git Verification

SSH signature verification requires configuring Git with signer public keys.

#### Configure SSH Signature Verification

```bash
# Download a user's SSH public keys from GitHub
curl https://github.com/<username>.keys -o /tmp/<username>.keys

# Configure Git to use the allowed signers file
git config gpg.ssh.allowedSignersFile ~/.ssh/allowed_signers

# Add the user's keys to allowed signers
# Format: <email> <key-type> <public-key>
echo "developer@example.com $(cat /tmp/<username>.keys)" >> ~/.ssh/allowed_signers

# For multiple keys from the same user, add each on a separate line
while IFS= read -r key; do
  echo "developer@example.com $key" >> ~/.ssh/allowed_signers
done < /tmp/<username>.keys

# Example for DiggSweden developers
curl https://github.com/diggsweden-bot.keys -o /tmp/diggsweden-bot.keys
echo "ospo@digg.se $(cat /tmp/diggsweden-bot.keys)" >> ~/.ssh/allowed_signers
```

### 6. Git Tag Verification

Verifies tag signatures and authenticity.

```bash
# Fetch tags
git fetch --tags

# Verify GPG signed tag
git verify-tag v1.0.0

# Verify SSH signed tag (requires SSH key setup from section 5)
git verify-tag v1.0.0 --raw
```

### 7. Git Commit Verification

Verifies developer identity via GPG or SSH signatures. SSH signature verification requires SSH key setup from section 5.

```bash
# Verify a specific commit signature
git verify-commit <commit-hash>

# Show commit signature details
git show --show-signature <commit-hash>

# List commits with signature status
git log --show-signature

# Check signature status in one-line format
git log --pretty="format:%h %G? %aN %s" --abbrev-commit
# Where %G? shows: G=good GPG, B=bad GPG, U=untrusted GPG, X=expired GPG, Y=expired key GPG, R=revoked key GPG, E=missing key, N=no signature

# Verify SSH signed commits (requires SSH key setup from section 5)
git verify-commit <commit-hash> --raw

# Configure git to show signatures by default
git config --local log.showSignature true
```

#### Automated Commit Verification

```bash
# Verify all commits in a branch
git log --format='%H' origin/main..HEAD | while read commit; do
  echo "Verifying $commit..."
  git verify-commit $commit || echo "WARNING: Unsigned commit $commit"
done

# Ensure all commits in PR are signed
git log --format='%G? %h %s' origin/main..HEAD | grep -E '^[NBU]' && echo "Found unsigned commits!" && exit 1 || echo "All commits signed"
```

### 8. Podman/Docker Verification

Verify and pull images securely:

```bash
# Enable signature verification in Podman
podman image trust set -t signedBy \
  -f ghcr.io/diggsweden \
  --pubkeysfile ospo.digg.pub.asc

# Pull with verification
podman pull ghcr.io/diggsweden/${PROJECT}:${VERSION}

# Inspect image signatures
skopeo inspect --raw ghcr.io/diggsweden/${PROJECT}:${VERSION}
```

### 9. Getting Public Keys from GitHub

GitHub provides access to users' public keys:

- **GPG keys**: `https://github.com/<username>.gpg`
- **SSH keys**: `https://github.com/<username>.keys`

```bash
# Download GPG public keys
curl https://github.com/<username>.gpg | gpg --import

# Download SSH public keys
curl https://github.com/<username>.keys >> ~/.ssh/allowed_signers
```

## Useful Tools

### Install Verification Tools

Using [mise](https://mise.jdx.dev/) with aqua backend:

```bash
# Install tools via mise with aqua backend
mise use -g aqua:sigstore/cosign
mise use -g aqua:slsa-framework/slsa-verifier
mise use -g aqua:containers/skopeo

# Verify installation
cosign version
slsa-verifier version
skopeo --version
```

## Additional Resources

### Container & Supply Chain Security

- [Sigstore Documentation](https://docs.sigstore.dev/)
- [Cosign Verification Guide](https://docs.sigstore.dev/cosign/verify/)
- [SLSA Framework](https://slsa.dev/)
- [GitHub Artifact Attestations](https://docs.github.com/en/actions/security-guides/using-artifact-attestations)
- [SBOM (CycloneDX) Specification](https://cyclonedx.org/)
- [Skopeo Documentation](https://github.com/containers/skopeo)
- [Podman Image Trust](https://docs.podman.io/en/latest/markdown/podman-image-trust.1.html)

### Code Signing & Verification

- [Git Signing Documentation](https://git-scm.com/book/en/v2/Git-Tools-Signing-Your-Work)
- [GitHub SSH Commit Verification](https://docs.github.com/en/authentication/managing-commit-signature-verification/about-commit-signature-verification#ssh-commit-signature-verification)
- [GPG Best Practices](https://www.gnupg.org/documentation/manuals/gnupg/OpenPGP-Key-Management.html)
- [Maven GPG Plugin](https://maven.apache.org/plugins/maven-gpg-plugin/)
- [NPM Package Provenance](https://docs.npmjs.com/generating-provenance-statements)

### GitHub Security Features

- [GitHub Security Hardening](https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions)
- [GitHub OIDC Token](https://docs.github.com/en/actions/deployment/security-hardening-your-deployments/about-security-hardening-with-openid-connect)
- [GitHub Packages Authentication](https://docs.github.com/en/packages/learn-github-packages/introduction-to-github-packages#authenticating-to-github-packages)
