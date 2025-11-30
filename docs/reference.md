## Environment Variables Matrix

| Variable/Secret | Required For | When Checked | Expected Value | Notes |
|-----------------|--------------|--------------|----------------|--------|
| **GITHUB_TOKEN** | All workflows | Always | Valid GitHub token | Provided by GitHub Actions |
| **RELEASE_BOT_TOKEN** | Release workflows | During release | GitHub PAT | Bot token for pushing commits, tags, and creating releases |
| **OSPO_BOT_GPG_PUB** | GPG signing | During signing | GPG public key | Public key for verification |
| **OSPO_BOT_GPG_PRIV** | GPG signing | During signing | Base64 GPG private key | Private key for signing |
| **OSPO_BOT_GPG_PASS** | GPG signing | During signing | GPG key passphrase | Passphrase for GPG key |
| **MAVENCENTRAL_USERNAME** | Maven Central publishing | During publish | Sonatype username | Maven Central auth |
| **MAVENCENTRAL_PASSWORD** | Maven Central publishing | During publish | Sonatype password | Maven Central auth |
| **NPM_TOKEN** | NPM publishing to npmjs.org | During publish | npmjs.org auth token | NPM public registry auth (not GitHub Packages) |
| **AUTHORIZED_RELEASE_DEVELOPERS** | Production releases | Pre-release check | Comma-separated usernames | Who can release |

## Prerequisites Check Matrix

| Check | When Performed | What It Validates | Fails If | How to Fix |
|-------|----------------|-------------------|----------|------------|
| **Version Match** | Release workflow | Tag matches project version | `v1.0.0` tag but pom.xml has `1.0.1` | Ensure tag matches version exactly |
| **GPG Key** | When `signatures: true` | GPG key is valid and accessible | Key expired or malformed | Generate new GPG key, export as base64 |
| **Maven Central Creds** | Maven Central publishing | Can authenticate to Sonatype | Invalid username/password | Verify Sonatype account credentials |
| **NPM Registry** | NPM publishing to npmjs.org | Can authenticate to registry | Token expired or invalid scope | Generate new NPM token with publish scope |
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
| | `security-events: write` | Upload scan results | Security tab won't show results |
| **Release Workflow** | `contents: write` | Create tags/releases | Cannot create release |
| | `packages: write` | Push packages | Cannot publish artifacts |
| | `id-token: write` | OIDC for SLSA | No attestation |
| | `attestations: write` | Attach SBOMs | No SBOM attachment |
| | `actions: read` | Read workflow | SLSA generation fails |
| | `issues: write` | Update issues | Cannot add labels/comments |
| **Dev Workflow** | `contents: read` | Read code | Cannot checkout |
| | `packages: write` | Push images | Cannot push to ghcr.io |

## Getting Access to Secrets

### How Secrets Work

**All secrets are managed centrally at the DiggSweden organization level.** As a developer in a DiggSweden project, you:

1. **Don't need to create secrets** - They already exist at DiggSweden org level
2. **Request access** - Contact your DiggSweden GitHub org owner/admin
3. **Specify which ones** - Tell them which secrets your repo needs:
   - Release bot token → Request `RELEASE_BOT_TOKEN`
   - GPG signing → Request `OSPO_BOT_GPG_PRIV`, `OSPO_BOT_GPG_PASS`, and `OSPO_BOT_GPG_PUB`
   - Maven Central → Request `MAVENCENTRAL_USERNAME` and `MAVENCENTRAL_PASSWORD`
   - NPM public registry → Request `NPM_TOKEN` (only if publishing to npmjs.org)
4. **Get enabled** - DiggSweden admin grants your repository access to the secrets

- **No manual configuration** - Developers never touch secret values

### RELEASE_BOT_TOKEN

Used for pushing commits, moving tags, and creating GitHub releases.

**Requires a fine-grained PAT** with `contents: write` permission, scoped to specific repositories. Classic PATs are rejected.

Note: GitHub Packages uploads use `GITHUB_TOKEN` (automatic, no configuration needed).

---

## Prerequisites

Some features require GitHub secrets:
- **GPG signing** needs GPG keys
- **Maven Central** needs Sonatype credentials  
- **Container registries** use GITHUB_TOKEN (automatic)

NOTE: All required GitHub secrets are configured at the DiggSweden organization level. Request access from DiggSweden GitHub administrators to enable secrets for the repository. You can of course also set up your own secrets.

## Local Testing

The release workflow includes several validation scripts that you can run locally before creating a tag:

See [Scripts Reference](scripts.md) for detailed documentation on validation scripts.

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

The orchestrator validates your configuration at runtime:

1. **Artifact names unique** - No duplicate names in `artifacts[]`
2. **Container references valid** - All `from:` entries exist in `artifacts[]`
3. **Project type valid** - Must be `maven`, `npm`, or `gradle`
4. **Working directory exists** - Path must exist in repository
5. **Build type valid** - Must be `application` or `library`
6. **Publishing targets valid** - Must be known registry
7. **Maven Central requirements** - Must have `build-type: library`

---

## Best Practices

1. **Use semantic artifact names** - `backend-api` not `app1`
2. **Set explicit versions** - Don't rely on defaults
3. **Enable security features** - Keep SLSA, SBOM, scanning enabled
4. **Multi-platform for production** - Always build `linux/amd64,linux/arm64`
5. **Require authorization for libraries** - Prevent accidental releases
6. **Use settings-path for credentials** - Don't hardcode in pom.xml

---
