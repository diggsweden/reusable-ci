# Gradle Publishing Onboarding Guide

This guide walks through adding Maven publishing support to a Gradle project so it can publish to **GitHub Packages** and/or **Maven Central** via the reusable CI `publish-gradle` workflow.

Follow the steps in order. GitHub Packages is simpler (no GPG) and is the recommended first smoke test. Maven Central requires additional secrets and signing configuration.

---

## Prerequisites

- The project uses Gradle with a `build.gradle.kts` (Kotlin DSL) or `build.gradle` (Groovy DSL).
- The project has a `gradlew` wrapper committed.
- The consuming repo is already integrated with reusable-ci (has a release workflow calling `release-orchestrator.yml`).
- For Maven Central: a Sonatype OSSRH account with an approved `groupId`, and a GPG key pair. The secrets `MAVENCENTRAL_USERNAME`, `MAVENCENTRAL_PASSWORD`, `OSPO_BOT_GPG_PRIV`, and `OSPO_BOT_GPG_PASS` must be set in the repo or org.

---

## Step 1 — Set the version to a SNAPSHOT

In `gradle.properties` (or wherever the version is defined), ensure the version ends in `-SNAPSHOT`:

```properties
version=0.1.0-SNAPSHOT
```

This ensures the publish run goes to the snapshot repository rather than attempting a release. Change this back to a non-SNAPSHOT version only when doing a real release.

---

## Step 2 — Add plugins to `build.gradle.kts`

```kotlin
plugins {
    // ... your existing plugins ...
    `maven-publish`
    signing
}
```

If using Groovy DSL (`build.gradle`):

```groovy
plugins {
    // ... your existing plugins ...
    id 'maven-publish'
    id 'signing'
}
```

---

## Step 3 — Configure the `publishing` block

The CI workflow injects credentials as Gradle project properties via `ORG_GRADLE_PROJECT_*` environment variables. Read them with `findProperty`.

Add the following to `build.gradle.kts`, replacing the placeholder values with your actual project coordinates:

```kotlin
// Read CI-injected credentials (empty strings when running locally without them)
val githubToken: String = findProperty("githubToken") as String? ?: ""
val githubActor: String = findProperty("githubActor") as String? ?: ""
val mavenCentralUsername: String = findProperty("mavenCentralUsername") as String? ?: ""
val mavenCentralPassword: String = findProperty("mavenCentralPassword") as String? ?: ""

publishing {
    publications {
        create<MavenPublication>("release") {
            // Replace these with your actual coordinates
            groupId = "se.digg.example"        // must match your approved Sonatype groupId
            artifactId = "my-gradle-lib"
            version = project.version.toString()

            // Publish the main jar. If this is a Java/Kotlin library, use:
            from(components["java"])
            // For Android libraries, use: from(components["release"])

            pom {
                name.set("My Gradle Library")
                description.set("A short description of the library.")
                url.set("https://github.com/YOUR_ORG/YOUR_REPO")

                licenses {
                    license {
                        name.set("Apache-2.0")
                        url.set("https://www.apache.org/licenses/LICENSE-2.0")
                    }
                }
                developers {
                    developer {
                        id.set("ospo-bot")
                        name.set("OSPO Bot")
                        email.set("ospo@example.se")
                    }
                }
                scm {
                    connection.set("scm:git:https://github.com/YOUR_ORG/YOUR_REPO.git")
                    developerConnection.set("scm:git:ssh://github.com/YOUR_ORG/YOUR_REPO.git")
                    url.set("https://github.com/YOUR_ORG/YOUR_REPO")
                }
            }
        }
    }

    repositories {
        // GitHub Packages
        maven {
            name = "GitHubPackages"
            url = uri("https://maven.pkg.github.com/YOUR_ORG/YOUR_REPO")
            credentials {
                username = githubActor
                password = githubToken
            }
        }

        // Maven Central (Sonatype OSSRH)
        // SNAPSHOT versions go to the snapshots repo automatically based on the version suffix.
        maven {
            name = "MavenCentral"
            val isSnapshot = project.version.toString().endsWith("-SNAPSHOT")
            url = uri(
                if (isSnapshot)
                    "https://s01.oss.sonatype.org/content/repositories/snapshots/"
                else
                    "https://s01.oss.sonatype.org/service/local/staging/deploy/maven2/"
            )
            credentials {
                username = mavenCentralUsername
                password = mavenCentralPassword
            }
        }
    }
}
```

---

## Step 4 — Configure signing

Signing is required for Maven Central (including SNAPSHOTs). It is not needed for GitHub Packages.

The CI workflow injects the GPG key as Gradle project properties. Add the following block after the `publishing` block:

```kotlin
val signingKeyId: String = findProperty("signingKeyId") as String? ?: ""
val signingKey: String = findProperty("signingKey") as String? ?: ""
val signingPassword: String = findProperty("signingPassword") as String? ?: ""

signing {
    // Only activate signing when the credentials are present (i.e., in CI).
    if (signingKeyId.isNotBlank() && signingKey.isNotBlank()) {
        useInMemoryPgpKeys(signingKeyId, signingKey, signingPassword)
        sign(publishing.publications["release"])
    }
}
```

This guard (`if (signingKeyId.isNotBlank())`) ensures local builds without GPG credentials do not fail.

---

## Step 5 — Ensure sources and javadoc jars are published (Maven Central requirement)

Maven Central requires a sources jar and a javadoc jar alongside the main artifact. Add this to `build.gradle.kts`:

```kotlin
java {
    withSourcesJar()
    withJavadocJar()
}
```

For Android libraries, configure this differently — see the Android section below.

---

## Step 6 — Configure `artifacts.yml` in the consuming repo

Create or update `.github/artifacts.yml` (or wherever the project's artifacts config lives):

```yaml
artifacts:
  - name: my-gradle-lib          # Must be unique within the repo
    project-type: gradle
    working-directory: .          # Relative path to the Gradle root; use "." for repo root
    build-type: library
    publish-to:
      - github-packages
      - maven-central
    config:
      java-version: 25            # Match the Java version used to build the project
      gradle-tasks: build         # Tasks run during the build stage
      gradle-version-file: gradle.properties
      publish-tasks: publish      # Runs all configured publish tasks (GitHub Packages + Maven Central)
```

If you only want to test GitHub Packages first, remove `- maven-central` from `publish-to`. Add it back when you are ready to test Maven Central.

---

## Step 7 — Verify required GitHub secrets

Navigate to the repo's **Settings → Secrets and variables → Actions** and confirm the following secrets exist. They may be inherited from the org level.

| Secret | Required for | Notes |
|--------|-------------|-------|
| `MAVENCENTRAL_USERNAME` | Maven Central | Sonatype OSSRH username |
| `MAVENCENTRAL_PASSWORD` | Maven Central | Sonatype OSSRH password or user token |
| `OSPO_BOT_GPG_PRIV` | Maven Central | ASCII-armored GPG private key (`gpg --armor --export-secret-keys KEY_ID`) |
| `OSPO_BOT_GPG_PASS` | Maven Central | Passphrase for the GPG key |
| `GITHUB_TOKEN` | GitHub Packages | Automatically provided — no action needed |

---

## Step 8 — Trigger a test run

The publish stage is triggered by the release orchestrator, which typically fires on a version tag push. For a SNAPSHOT smoke test, you have two options:

### Option A — Trigger via workflow dispatch (recommended for testing)

If the release orchestrator supports `workflow_dispatch`, trigger it manually from the GitHub Actions UI, specifying the branch and a SNAPSHOT version.

### Option B — Push a tag

```bash
git tag v0.1.0-SNAPSHOT
git push origin v0.1.0-SNAPSHOT
```

Note: some release orchestrators only trigger on tags matching a specific pattern (e.g. `v*`). Check the `on:` block in the repo's release workflow.

---

## Step 9 — Verify the SNAPSHOT was published

**GitHub Packages:**
Navigate to `https://github.com/YOUR_ORG/YOUR_REPO/packages` — the package should appear under the Maven category.

**Maven Central (Sonatype OSSRH snapshots):**
Browse to:
```
https://s01.oss.sonatype.org/content/repositories/snapshots/YOUR/GROUP/ID/
```
replacing `/` for each `.` in the groupId. Confirm that `-SNAPSHOT` metadata files are present.

---

## Troubleshooting

**`gradlew: Permission denied`**
The workflow runs `chmod +x ./gradlew` automatically. If this still fails, commit the wrapper with executable bit: `git update-index --chmod=+x gradlew`.

**`Could not find method signing()`**
The `signing` plugin was not applied. Ensure `signing` is in the `plugins {}` block (Step 2).

**`Received status code 401 from server: Unauthorized` (GitHub Packages)**
The `githubToken` property is empty. Check that `ORG_GRADLE_PROJECT_githubToken` is being read correctly — verify the `findProperty("githubToken")` call matches exactly (case-sensitive).

**`Received status code 401 from server: Unauthorized` (Maven Central)**
The `MAVENCENTRAL_USERNAME` or `MAVENCENTRAL_PASSWORD` secret is missing or incorrect. Confirm the secrets are set at repo or org level.

**`Signing key was not provided`**
The `signingKeyId` / `signingKey` properties are empty. Check that `OSPO_BOT_GPG_PRIV` and `OSPO_BOT_GPG_PASS` secrets are configured and that the GPG key is exported in ASCII-armored format.

**Publication succeeds but no artifact visible in Maven Central UI**
SNAPSHOT artifacts do not appear in the Maven Central search UI — only releases do. Check the Sonatype OSSRH snapshots browser URL listed in Step 9.

---

## Android library notes

For Android libraries, replace `from(components["java"])` with `from(components["release"])` and configure sources/javadoc differently, as the `java` component is not available. Also set `setup-android: true` in `artifacts.yml`:

```yaml
config:
  java-version: 25
  gradle-tasks: build
  publish-tasks: publish
  setup-android: true
```
