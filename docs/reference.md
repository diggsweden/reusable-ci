# Secrets, Permissions, and Validation

Required secrets, permissions, and validation matrices for reusable-ci
workflows. For the generated command surface, see [CLI Reference](cli-reference.md).

## Environment Variables Matrix

| Variable/Secret | Required For | When Checked | Expected Value | Notes |
|-----------------|--------------|--------------|----------------|--------|
| **GITHUB_TOKEN** | All workflows | Always | Valid GitHub token | Provided by GitHub Actions |
| **RELEASE_TOKEN** | Release workflows | During release | GitHub PAT | Bot token for pushing commits, tags, and creating releases |
| **RELEASE_GPG_PUBLIC_KEY** | Release validation/signing | During prerequisite validation | GPG public key | Public key for tag/signature verification |
| **RELEASE_GPG_PRIVATE_KEY** | Release validation/signing | During prerequisite validation | Base64 GPG private key | Private key imported for release validation, version bump signing, and artifact signing |
| **RELEASE_GPG_PASSPHRASE** | Release validation/signing | During prerequisite validation | GPG key passphrase | Passphrase for GPG key |
| **MAVEN_CENTRAL_USERNAME** | Maven Central publishing | During publish | Sonatype username | Maven Central auth |
| **MAVEN_CENTRAL_PASSWORD** | Maven Central publishing | During publish | Sonatype password | Maven Central auth |
| **NPM_TOKEN** | Future npmjs.org publishing | Not checked by current production release | npmjs.org auth token | Reserved; current NPM publishing uses GitHub Packages and `GITHUB_TOKEN` |

Release authorisation is not a secret. It lives in the repo as
`.reusable-ci/allowed_signers` (SSH) and
`.reusable-ci/allowed_gpg_keys.asc` (GPG). See [verification.md](verification.md#release-authorisation).

## Prerequisites Check Matrix

| Check | When Performed | What It Validates | Fails If | How to Fix |
|-------|----------------|-------------------|----------|------------|
| **Version Match** | Release workflow | Tag matches project version | `v1.0.0` tag but pom.xml has `1.0.1` | Ensure tag matches version exactly |
| **GPG Key** | Production release workflow | Release GPG keys are valid and accessible | Key missing, expired, or malformed | Generate new GPG key, export as base64 |
| **Maven Central Creds** | Maven Central publishing | Can authenticate to Sonatype | Invalid username/password | Verify Sonatype account credentials |
| **NPM Registry** | Future npmjs.org publishing | Not currently enforced | N/A until support is enabled | Generate a publish-scoped NPM token when npmjs.org support is enabled |
| **Container Registry** | Container in `containers[]` | Can push to registry | No write permission | Ensure `packages: write` permission |
| **GitHub Release** | Release creation | Can create releases | No `contents: write` | Add permission to workflow |
| **Protected Branch** | On push to main | User has bypass rights | Actor lacks permission | Add user to bypass list |
| **Artifact Existence** | During upload | Build artifacts exist | `target/*.jar` not found | Ensure build succeeds first |
| **Container/Containerfile** | Container build | Containerfile exists | No Containerfile at specified path | Create Containerfile or specify correct path |
| **License Compliance** | PR checks | Dependencies have compatible licenses | GPL in proprietary project | Review and replace dependencies |

## Permission Requirements Matrix

| Workflow | Permission | Why Needed | If Missing |
|----------|------------|------------|------------|
| **PR Workflow** | `contents: read` | Read code | Cannot checkout |
| | `packages: read` | Read private packages | Cannot fetch dependencies |
| | `secrets: inherit` | Pass `CODE_SCANNING_TOKEN` | Code Scanning won't show results |
| **Release Workflow** | `contents: write` | Create tags/releases | Cannot create release |
| | `packages: write` | Push packages | Cannot publish artifacts |
| | `id-token: write` | OIDC for SLSA | No attestation |
| | `attestations: write` | Attach SBOMs | No SBOM attachment |
| | `actions: read` | Read workflow | SLSA generation fails |
| **Snapshot Workflow** | `contents: read` | Read code | Cannot checkout |
| | `packages: write` | Publish npm snapshot to GitHub Packages | Cannot publish package |

## Getting Access to Secrets

### How Secrets Work

For general use, create the required secrets as repository or organization
secrets in GitHub. Production releases currently require `RELEASE_TOKEN`,
`RELEASE_GPG_PRIVATE_KEY`, `RELEASE_GPG_PASSPHRASE`, and
`RELEASE_GPG_PUBLIC_KEY` because validation, version bumping, and artifact
signing use them by default.

Orgs that manage these secrets centrally typically expose them as
organization-level secrets and grant per-repository access. In that
setup a repo maintainer asks the org admin for the names the repo
actually needs:

- Release token → `RELEASE_TOKEN`
- GPG signing → `RELEASE_GPG_PRIVATE_KEY`, `RELEASE_GPG_PASSPHRASE`, `RELEASE_GPG_PUBLIC_KEY`
- Maven Central → `MAVEN_CENTRAL_USERNAME`, `MAVEN_CENTRAL_PASSWORD`
- NPM public registry → `NPM_TOKEN` (only once npmjs.org publishing is enabled for the repo)
- Code Scanning upload → `CODE_SCANNING_TOKEN`

The repo workflows then reference them by name with `secrets:
inherit` or an explicit `secrets:` block at the caller. Adopters
outside such an org configure the same names as repository or
organization secrets in their own GitHub setup.

### RELEASE_TOKEN

Used for pushing commits, moving tags, and creating GitHub releases.

**Requires a fine-grained PAT** with `contents: write` permission, scoped to specific repositories. Classic PATs are rejected.

Note: GitHub Packages uploads use `GITHUB_TOKEN` (automatic, no configuration needed).

### CODE_SCANNING_TOKEN

Used for uploading security scan results (SARIF) to GitHub Security / Code Scanning. Without this token, scans still run, SARIF is still generated, and SARIF files are still saved as workflow artifacts, but results won't appear in Security / Code Scanning.

**Option A — GitHub App (recommended):**
- Create a GitHub App with `code_scanning_alerts: write` repository permission
- Install on target repositories
- Generate installation token and store as repository or organization secret `CODE_SCANNING_TOKEN`

**Option B — Fine-grained PAT:**
- Create a fine-grained PAT with "Code scanning alerts" set to **Write**
- Scope to the target repositories
- Store as repository or organization secret `CODE_SCANNING_TOKEN`

The token is passed to reusable workflows via `secrets: inherit`.

**Code Scanning categories:**

Results appear in the Code Scanning tab grouped by category:

| Category | Scanner | When |
|----------|---------|------|
| `dependency-review` | Trivy | PR dependency scan |
| `opengrep-sast` | OpenGrep | PR SAST scan |
| `scorecard` | OpenSSF Scorecard | Scheduled analysis |
| `container-scan` | Trivy | Release container build |

SARIF files are also saved as workflow artifacts (`sarif-dependency-review`, `sarif-opengrep`, `sarif-scorecard`, `sarif-container-scan`) regardless of whether the token is configured.

---

## Local Testing

The release workflow validation surface is exposed through the `reusable-ci`
binary. Run `reusable-ci validate --help` for the validation commands, or see
[CLI Reference](cli-reference.md) for the generated command reference.

---

## Version Tag Format

**Tags for releases:**
- Production: `v1.0.0`, `v2.3.4`
- Alpha: `v1.0.0-alpha`, `v1.0.0-alpha.1`
- Beta: `v1.0.0-beta`, `v1.0.0-beta.1`
- Release Candidate: `v1.0.0-rc`, `v1.0.0-rc.1`
- Snapshot: `v1.0.0-snapshot`, `v1.0.0-SNAPSHOT`

---

## Validation

The orchestrator performs core runtime validation and normalization when it parses `artifacts.yml`:

1. **Artifacts config exists and is not empty** - The configured file must exist and contain `artifacts[]`
2. **Container references valid** - All `containers[].from` entries must exist in `artifacts[]`
3. **Project type valid** - Each artifact `project-type` must be a supported value
4. **Draft-release detection** - SemVer prerelease and `-SNAPSHOT` tags can be normalized into draft-release behavior; non-SemVer tags fail tag-format validation
5. **SBOM defaults resolved** - SBOM generation is derived per artifact when not set explicitly

Additional release-specific validation happens in helper workflows such as `validate-release-prerequisites.yml`.

---

## Best Practices

1. **Use semantic artifact names** - `backend-api` not `app1`
2. **Set explicit versions** - Don't rely on defaults
3. **Enable security features** - Keep SLSA, SBOM, scanning enabled
4. **Multi-platform for production** - Always build `linux/amd64,linux/arm64`
5. **Require authorization for libraries** - Prevent accidental releases
6. **Use settings-path for credentials** - Don't hardcode in pom.xml

---
