#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
#
# Generate a CycloneDX Build SBOM for a Gradle project without requiring the
# consumer to edit build.gradle. Applies the cyclonedx-gradle-plugin to every
# project via a throwaway init script and runs the `cyclonedxBom` task.
#
# Runs in the current working directory; the caller (workflow) picks the
# project root via `working-directory`.
#
# Required env:
#   CYCLONEDX_GRADLE_VERSION — pinned plugin version (Renovate-managed in the
#   calling workflow).

set -euo pipefail

: "${CYCLONEDX_GRADLE_VERSION:?CYCLONEDX_GRADLE_VERSION is required}"

# Refuse to run in a directory without a gradlew — saves a confusing
# "./gradlew: not found" further down.
if [[ ! -x "./gradlew" ]]; then
  printf '::error::gradlew not found or not executable in %s\n' "$(pwd)" >&2
  exit 1
fi

INIT_SCRIPT="$(mktemp --suffix=.init.gradle.kts)"
trap 'rm -f "$INIT_SCRIPT"' EXIT

cat >"$INIT_SCRIPT" <<EOF
import org.cyclonedx.gradle.CyclonedxPlugin

initscript {
    repositories {
        gradlePluginPortal()
    }
    dependencies {
        classpath("org.cyclonedx.bom:org.cyclonedx.bom.gradle.plugin:${CYCLONEDX_GRADLE_VERSION}")
    }
}

allprojects {
    apply<CyclonedxPlugin>()
}
EOF

./gradlew --init-script "$INIT_SCRIPT" cyclonedxBom
