## Environment Variables Matrix

| Variable/Secret | Required For | When Checked | Expected Value | Notes |
|-----------------|--------------|--------------|----------------|--------|
| **GITHUB_TOKEN** | All workflows | Always | Valid GitHub token | Provided by GitHub Actions |
| **RELEASE_TOKEN** | Release workflows | During release | GitHub PAT | Bot token for pushing commits, tags, and creating releases |
| **RELEASE_GPG_PUBLIC_KEY** | GPG signing | During signing | GPG public key | Public key for verification |
| **RELEASE_GPG_PRIVATE_KEY** | GPG signing | During signing | Base64 GPG private key | Private key for signing |
| **RELEASE_GPG_PASSPHRASE** | GPG signing | During signing | GPG key passphrase | Passphrase for GPG key |
| **MAVEN_CENTRAL_USERNAME** | Maven Central publishing | During publish | Sonatype username | Maven Central auth |
| **MAVEN_CENTRAL_PASSWORD** | Maven Central publishing | During publish | Sonatype password | Maven Central auth |
| **NPM_TOKEN** | NPM publishing to npmjs.org | During publish | npmjs.org auth token | NPM public registry auth (not GitHub Packages) |
| **AUTHORIZED_RELEASE_DEVELOPERS** | Production releases | Pre-release check | Comma-separated usernames | Who can release |

## Prerequisites Check Matrix

| Check | When Performed | What It Validates | Fails If | How to Fix |
|-------|----------------|-------------------|----------|------------|
| **Release Source** | Prerequisites and immediately before version bump | Triggering tag points to the selected release branch HEAD | Tag is on an older commit or a pre-merge PR commit | Merge first, update the release branch locally, then create a new signed tag on its tip |
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
| **Release Workflow** | `contents: write` | Create tags/releases | Cannot create release |
| | `pull-requests: read` | Read PR metadata for changelogs using `GITHUB_TOKEN` | Nested workflow rejected or private-repo changelog API returns 403 |
| | `packages: write` | Push packages | Cannot publish artifacts |
| | `id-token: write` | OIDC for SLSA | No attestation |
| | `attestations: write` | Attach SBOMs | No SBOM attachment |
| | `actions: read` | Read workflow | SLSA generation fails |
| | `issues: write` | Update issues | Cannot add labels/comments |
| **Dev Workflow** | `contents: read` | Read code | Cannot checkout |
| | `packages: write` | Push images | Cannot push to ghcr.io |

The release caller must permit `pull-requests: read` even for public repositories: nested workflows cannot elevate the caller's token permissions. This applies to the automatic `GITHUB_TOKEN`, not the release PAT. See the short [Migration Guide](migration.md).

For later steps in private repositories, see [Private Repository Release Notes](private-repositories.md) for package authentication, repository rules, and attestation/upload availability.

## Passing Secrets

> [!WARNING]
> v3.0.0 requires explicit mappings using the canonical secret names for all consumers. `secrets: inherit` is unsupported in v3, including for existing callers upgrading from v2. Follow the short [Migration Guide](migration.md).

For PR checks, replace the caller's `secrets: inherit` line with:

```yaml
secrets:
  CODE_SCANNING_TOKEN: ${{ secrets.CODE_SCANNING_TOKEN }}
```

The upload token is optional. Omit this block when you do not need Code Scanning uploads; scans and report artifacts still work. `GITHUB_TOKEN` is provided automatically and does not need to be mapped.

For releases, map the signing and publishing secrets needed by your configuration using the canonical names below. See the [ecosystem examples](../examples/README.md). Reusable workflows declare their accepted secrets under `on.workflow_call.secrets` and forward them explicitly to the jobs that need them. Optional declarations do not remove credential requirements for features such as signing or publishing.

### Canonical Secret Names (Breaking Change)

This revision aligns with `main-golang`. The previous secret names are not accepted as workflow parameters or read as runtime environment fallbacks.

| Previous name | Required canonical name |
|---------------|-------------------------|
| `RELEASE_BOT_TOKEN` | `RELEASE_TOKEN` |
| `OSPO_BOT_GPG_PRIV` | `RELEASE_GPG_PRIVATE_KEY` |
| `OSPO_BOT_GPG_PASS` | `RELEASE_GPG_PASSPHRASE` |
| `OSPO_BOT_GPG_PUB` | `RELEASE_GPG_PUBLIC_KEY` |
| `MAVENCENTRAL_USERNAME` | `MAVEN_CENTRAL_USERNAME` |
| `MAVENCENTRAL_PASSWORD` | `MAVEN_CENTRAL_PASSWORD` |
| `SARIF_UPLOAD_TOKEN` | `CODE_SCANNING_TOKEN` |

Update caller mappings and repository/organization secret configuration before adopting this revision. Ensure each consuming repository has access to the new names and that its GPG key, passphrase, and public key match. Custom Maven settings must use `${env.MAVEN_CENTRAL_USERNAME}` and `${env.MAVEN_CENTRAL_PASSWORD}`. Direct script users must update their environment variables too.

There is no grace period in v3.0.0 for inherited calls or retired secret names. No aliases or automatic fallback to previous credentials are provided.

### Compatible Workflow Versions

The updated examples pin both `uses:` and `reusable-ci-ref` to `747ac6d4ff82d409a48c125266939a15df9a3270`, which contains the canonical secret declarations, OpenGrep suppression-counting fix, and changelog PR-read permissions. Use that commit once published, or a release containing it. Older revisions may lack these contracts or fixes. Keep the workflow ref and helper-script ref aligned when upgrading.

Once callers use the canonical names, moving from inheritance to explicit mappings only changes the `secrets:` block. No new workflow inputs are required.

### Migration To v3.0.0

| Caller | Required action |
|--------|-----------------|
| New v3 integration | Use explicit mappings with canonical secret names |
| Existing explicit mappings | Update any retired secret names before adopting v3 |
| Existing `secrets: inherit` | Replace inheritance with explicit canonical mappings before adopting v3 |
| Staying on pinned v2 workflows | Retain the selected v2 workflow's contract until upgrading |

1. Choose the v3.0.0 release once available, or the complete migration baseline above, and keep `uses:` and `reusable-ci-ref` pinned to the same revision. Include the new `pull-requests: read` permission in release callers.
2. Replace `secrets: inherit` at each call site with the canonical secrets needed by that workflow. Omit the block when no custom secrets are needed. Keep workflow inputs and build/publish configuration; update any previous secret names as described above.
3. Check the relevant PR, signing, and publishing paths with your configured canonical secrets before adopting v3.0.0. Remove any old inheritance suppression when replacing the line.

Consumers already using explicit mappings with the canonical names need no further secret-name or mapping changes. Consumers staying on pinned v2 workflows are not automatically upgraded when v3.0.0 is published. Pin the helper-script ref too: a `reusable-ci-ref` of `main` follows newer implementation changes independently of the workflow pin.

This is the supported v3 caller contract. `inherit` remains a GitHub Actions feature; declaring named secrets in a reusable workflow does not itself make GitHub reject inherited calls. A technically accepted or scanner-suppressed inherited call is still unsupported in v3.

### Suppressed Scan Findings

The pinned upgrade target above includes the OpenGrep suppression-counting fix listed under **Unreleased** in the [changelog](../CHANGELOG.md). Correctly suppressed findings do not contribute to the summary counts or failure threshold; original JSON/SARIF reports can still retain them for auditing. Other active findings continue to be checked normally. This counting fix does not provide an inheritance compatibility exception in v3.

## Getting Access to Secrets

### How Secrets Work

**All secrets are managed centrally at the DiggSweden organization level.** As a developer in a DiggSweden project, you:

1. **Don't need to create secrets** - They already exist at DiggSweden org level
2. **Request access** - Contact your DiggSweden GitHub org owner/admin
3. **Specify which ones** - Tell them which secrets your repo needs:
   - Release bot token → Request `RELEASE_TOKEN`
   - GPG signing → Request `RELEASE_GPG_PRIVATE_KEY`, `RELEASE_GPG_PASSPHRASE`, and `RELEASE_GPG_PUBLIC_KEY`
   - Maven Central → Request `MAVEN_CENTRAL_USERNAME` and `MAVEN_CENTRAL_PASSWORD`
   - NPM public registry → Request `NPM_TOKEN` (only if publishing to npmjs.org)
   - Code Scanning upload → Request `CODE_SCANNING_TOKEN`
4. **Get enabled** - DiggSweden admin grants your repository access to the secrets

- **No manual configuration** - Developers never touch secret values

### RELEASE_TOKEN

Used for pushing commits, moving tags, and creating GitHub releases.

**Requires a fine-grained PAT** with `contents: write` permission, scoped to specific repositories. Classic PATs are rejected.

Note: GitHub Packages uploads use `GITHUB_TOKEN` (automatic, no configuration needed).

### CODE_SCANNING_TOKEN

Used for uploading security scan results (SARIF) to GitHub Security / Code Scanning. Without this token, scans still run, SARIF is still generated, and SARIF files are still saved as workflow artifacts, but results won't appear in Security / Code Scanning.

**Option A — GitHub App (recommended):**
- Create a GitHub App with `code_scanning_alerts: write` repository permission
- Install on target repositories
- Generate installation token and store as org secret `CODE_SCANNING_TOKEN`

**Option B — Fine-grained PAT:**
- Create a fine-grained PAT with "Code scanning alerts" set to **Write**
- Scope to the target repositories
- Store as org secret `CODE_SCANNING_TOKEN`

Pass the token explicitly as shown in [Passing Secrets](#passing-secrets).

**Code Scanning categories:**

Results appear in the Code Scanning tab grouped by category:

| Category | Scanner | When |
|----------|---------|------|
| `megalinter` | MegaLinter | PR quality checks |
| `dependency-review` | Trivy | PR dependency scan |
| `opengrep-sast` | OpenGrep | PR SAST scan |
| `scorecard` | OpenSSF Scorecard | Scheduled analysis |
| `container-scan` | Trivy | Release container build |

SARIF files are also saved as workflow artifacts (`sarif-megalinter`, `sarif-dependency-review`, `sarif-opengrep`, `sarif-scorecard`, `sarif-container-scan`) regardless of whether the token is configured.

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

The orchestrator performs core runtime validation and normalization when it parses `artifacts.yml`:

1. **Artifacts config exists and is not empty** - The configured file must exist and contain `artifacts[]`
2. **Container references valid** - All `containers[].from` entries must exist in `artifacts[]`
3. **Project type valid** - Each artifact `project-type` must be a supported value
4. **Draft-release detection** - Non-release tags and `-SNAPSHOT` tags are normalized into draft-release behavior
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
