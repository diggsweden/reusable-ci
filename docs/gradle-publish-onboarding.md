<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Gradle Publishing Onboarding

How to add Maven publishing to a Gradle project so it can publish to
**GitHub Packages** and/or **Maven Central** through reusable-ci's
`publish-gradle.yml`.

This applies to plain JVM Gradle projects (`project-type: gradle`) and to
Android **libraries** (`project-type: gradle-android` +
`build-type: library`) alike — they take the same publish path.

> **Check your work as you go.** `reusable-ci doctor` validates most of
> this guide offline against your build script. It is much cheaper than
> discovering a missing javadoc jar from a Sonatype rejection.

## What reusable-ci does and does not do

The CI runs your project's own `maven-publish` configuration and supplies
credentials. It does not generate publishing config for you. That split
is why this guide is mostly about `build.gradle.kts`.

Two consequences worth knowing up front:

- **The publish job rebuilds.** Gradle's `maven-publish` needs the
  project to produce its publications, so `publish-gradle.yml` checks out
  and rebuilds rather than consuming the build stage's uploaded JAR/AAR.
  That build-stage artefact is still what gets attached to the release
  and what the Build SBOM describes.
- **Property names are a contract.** A Gradle project property only
  arrives if your build script asks for it, via `findProperty("…")` or a
  plugin that does so on your behalf. The table in
  [publishing.md](publishing.md#credentials) is the authoritative list.

## Prerequisites

- A committed Gradle wrapper (`gradlew`).
- For Maven Central: a Sonatype Central Portal account with an approved
  `groupId`, plus the release GPG key configured in the repository
  secrets.
- For GitHub Packages: nothing — the auto-provided `GITHUB_TOKEN` is used.

## Step 1 — Apply the plugins

```kotlin
plugins {
    `java-library`
    `maven-publish`
    signing          // Maven Central only
}
```

## Step 2 — Publish sources and javadoc jars

Maven Central **requires** both. This is the most common onboarding
failure, and it fails late — Central rejects the bundle after the publish
step has already reported success.

```kotlin
java {
    withSourcesJar()
    withJavadocJar()
}
```

For an Android library, the equivalent lives in the `android` block:

```kotlin
android {
    publishing {
        singleVariant("release") {
            withSourcesJar()
            withJavadocJar()
        }
    }
}
```

## Step 3 — Configure the `publishing` block

Credentials arrive as Gradle project properties (from
`ORG_GRADLE_PROJECT_*` environment variables), so read them with
`findProperty`.

**The repository names matter.** The publish task is derived per target as
`publishAllPublicationsTo<Name>Repository`, so the repository blocks must
be named exactly `GitHubPackages` and `MavenCentral` — or you must set
`config.publish-tasks` to override the derived task. See
[Step 6](#step-6--overriding-the-derived-task).

```kotlin
val githubActor: String = findProperty("githubActor") as String? ?: ""
val githubToken: String = findProperty("githubToken") as String? ?: ""
val mavenCentralUsername: String = findProperty("mavenCentralUsername") as String? ?: ""
val mavenCentralPassword: String = findProperty("mavenCentralPassword") as String? ?: ""

publishing {
    publications {
        create<MavenPublication>("release") {
            groupId = "se.digg.example"   // must match your approved Sonatype groupId
            artifactId = "my-gradle-lib"
            version = project.version.toString()

            from(components["java"])
            // Android library: from(components["release"])

            // Maven Central validates every field below. Omitting any of
            // them is a rejection, not a warning.
            pom {
                name.set("My Gradle Library")
                description.set("A short description of the library.")
                url.set("https://github.com/YOUR_ORG/YOUR_REPO")

                licenses {
                    license {
                        name.set("EUPL-1.2")
                        url.set("https://joinup.ec.europa.eu/collection/eupl/eupl-text-eupl-12")
                    }
                }
                developers {
                    developer {
                        id.set("YOUR_ID")
                        name.set("YOUR NAME")
                        email.set("you@example.org")
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
        maven {
            name = "GitHubPackages"   // → publishAllPublicationsToGitHubPackagesRepository
            url = uri("https://maven.pkg.github.com/YOUR_ORG/YOUR_REPO")
            credentials {
                username = githubActor
                password = githubToken
            }
        }
        maven {
            name = "MavenCentral"     // → publishAllPublicationsToMavenCentralRepository
            url = uri("https://ossrh-staging-api.central.sonatype.com/service/local/staging/deploy/maven2/")
            credentials {
                username = mavenCentralUsername
                password = mavenCentralPassword
            }
        }
    }
}
```

## Step 4 — Configure signing

Required for Maven Central, unnecessary for GitHub Packages.

```kotlin
val signingKeyId: String = findProperty("signingKeyId") as String? ?: ""
val signingKey: String = findProperty("signingKey") as String? ?: ""
val signingPassword: String = findProperty("signingPassword") as String? ?: ""

signing {
    // Guard so local builds without GPG credentials still work.
    if (signingKeyId.isNotBlank() && signingKey.isNotBlank()) {
        useInMemoryPgpKeys(signingKeyId, signingKey, signingPassword)
        sign(publishing.publications["release"])
    }
}
```

The CI re-exports the signing key from its keyring before invoking
Gradle, because `useInMemoryPgpKeys` goes through Bouncycastle, which
reads only RFC 4880 packets — keys exported by newer GnuPG in the v5
format are rejected. You do not need to do anything for this; it is
mentioned because it explains why the key is not simply the raw
`RELEASE_GPG_PRIVATE_KEY` secret.

## Step 5 — Configure `artifacts.yml`

```yaml
artifacts:
  - name: my-gradle-lib
    project-type: gradle
    build-type: library          # required for maven-central
    publish-to:
      - github-packages
      - maven-central
    config:
      java-version: 25
```

For an Android library, change two things — and note there is **no**
`setup-android` option; the Android SDK comes from the runtime image,
which the orchestrator selects automatically:

```yaml
  - name: my-android-lib
    project-type: gradle-android
    build-type: library
    publish-to: [maven-central]
    config:
      build-module: lib
```

## Step 6 — Overriding the derived task

Leave `publish-tasks` unset unless your publishing plugin names its task
differently. The most common case by far is vanniktech's
`gradle-maven-publish-plugin`:

```yaml
    config:
      publish-tasks: publishToMavenCentral
```

That plugin also reads the signing credentials under a different
spelling (`signingInMemoryKey…` rather than `signingKey…`). The CI binds
**both** spellings with identical values, so either works without
configuration — but if you invented a third spelling, neither will
arrive. `reusable-ci doctor` reports which convention your build script
actually reads.

Note that the derived tasks are the named-repository forms rather than
the bare `publish` task. `publish` pushes every publication to *every*
configured repository, which would publish to Central from inside the
GitHub Packages job.

## Step 7 — Verify secrets

For Maven Central, the repository (or org) needs:

| Secret | Purpose |
|---|---|
| `MAVEN_CENTRAL_USERNAME` | Sonatype Central portal username |
| `MAVEN_CENTRAL_PASSWORD` | Sonatype Central portal token (not the account password) |
| `RELEASE_GPG_PRIVATE_KEY` | Armored OpenPGP private key |
| `RELEASE_GPG_PASSPHRASE` | Passphrase for that key |

These names are fixed: `validate secrets` and
`report status prerequisites` check for exactly these.

## Step 8 — Check before you tag

```bash
reusable-ci doctor
```

This validates the plugins, the sources/javadoc jars, the repository
names, and the credential-property convention against your build script —
offline, in about a second.

Then publish by pushing a release tag, as with any other artefact type.

## A note on SNAPSHOTs

There is **no** snapshot pipeline for Gradle. Dev-publish is NPM-only.

Publishing a `-SNAPSHOT` version is a manual testing technique — set the
version in `gradle.properties`, run the publish task locally against the
snapshots repository — not a pipeline feature. Do not expect a tagged
release to produce one.

## Troubleshooting

| Symptom | Cause |
|---|---|
| `Task 'publishAllPublicationsToMavenCentralRepository' not found` | Repository block is not named `MavenCentral`. Rename it, or set `config.publish-tasks`. |
| Central rejects the bundle as unsigned | The `signing` block never activated — usually the credential properties are spelled differently from what the script reads. |
| Central rejects the bundle for missing javadoc/sources | `withSourcesJar()` / `withJavadocJar()` not configured. |
| Publish succeeds but nothing appears on Central | Central publishing is a staged deployment; check the Central Portal for a validation failure. |
| Publish summary shows no version | `build gradle metadata` reads `gradle.properties` only. A project computing its version in `build.gradle.kts` gets an empty summary — cosmetic, not a failure. |

## See also

- [publishing.md](publishing.md#maven-central-and-github-packages-gradle) — target reference and credential table
- [artifacts-reference.md](artifacts-reference.md#configuration-fields-gradle) — every Gradle config field
- [`examples/gradle-app/`](../examples/gradle-app/) — JVM library example
- [`examples/android-library/`](../examples/android-library/) — Android library example
