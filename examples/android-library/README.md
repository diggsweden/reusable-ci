<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Android Library Example

Example configuration for an Android library published as an **AAR** to
Maven Central and/or GitHub Packages.

## Use Case

Android libraries — as opposed to applications — that need to:

- Publish an AAR to Maven Central and/or GitHub Packages
- Be consumed by other Gradle projects as a dependency
- Sign published artefacts with the release GPG key

If you are shipping an app to Google Play, use
[`examples/android-app/`](../android-app/) instead.

## The one field that matters

`build-type: library`. Library and application are two build-types of the
same `gradle-android` project-type:

| | `build-type: library` | `build-type: application` (or unset) |
|---|---|---|
| Publish targets | `maven-central`, `github-packages` | `google-play` |
| Artefact | AAR | APK / AAB |
| Android keystore signing | not used | `enable-android-signing: true` |
| Artefact signing | GPG release key (Central) | Play app signing |

There is **no** `setup-android` option. The Android SDK comes from the
runtime image, and the orchestrator routes `gradle-android` artifacts to
`runtime-image-android` (`reusable-ci-runtime-android-35`) automatically.

## Files

### `artifacts.yml`

Artifact configuration for release builds.

### `release-workflow.yml`

Production release workflow triggered by version tags.

## Build script requirements

Publishing happens through your project's own `maven-publish`
configuration, so the build script has to hold up its end:

1. Apply `maven-publish`, and `signing` when targeting Maven Central.
2. Name the repository blocks `GitHubPackages` and `MavenCentral` — the
   derived publish task is `publishAllPublicationsTo<Name>Repository`. If
   your plugin names its task differently, set `config.publish-tasks`
   instead (vanniktech's plugin wants `publishToMavenCentral`).
3. Publish sources and javadoc jars — Maven Central rejects bundles
   without them, and it does so *after* the publish appears to succeed.

Run `reusable-ci doctor` to check all of the above against your build
script before the first release; it is considerably cheaper than finding
out from a Sonatype validation error.

## Note on rebuilds

`publish-gradle.yml` publishes from **source**: Gradle's `maven-publish`
needs the project to produce its publications, so the publish job checks
out and rebuilds rather than consuming the build stage's uploaded AAR.
That build-stage artefact is still what gets attached to the release and
what the Build SBOM describes.

## Required Secrets

For Maven Central:

| Secret | Purpose |
|---|---|
| `MAVEN_CENTRAL_USERNAME` | Sonatype Central portal username |
| `MAVEN_CENTRAL_PASSWORD` | Sonatype Central portal token |
| `RELEASE_GPG_PRIVATE_KEY` | Armored OpenPGP key used to sign artefacts |
| `RELEASE_GPG_PASSPHRASE` | Passphrase for that key |

GitHub Packages needs no secrets — it uses the auto-provided
`GITHUB_TOKEN`.
