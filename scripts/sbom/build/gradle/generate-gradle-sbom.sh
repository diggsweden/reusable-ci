#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

: "${CYCLONEDX_GRADLE_VERSION:?CYCLONEDX_GRADLE_VERSION is required}"

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
