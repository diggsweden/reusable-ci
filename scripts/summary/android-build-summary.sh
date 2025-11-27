#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

readonly JAVA_VERSION="${1:?Usage: $0 <java-version> <jdk-dist> <module> <flavor> <build-types> <include-aab> <signing> <tests> <version> <version-code> <debug-name> <release-name> <aab-name>}"
readonly JDK_DIST="${2:?}"
readonly BUILD_MODULE="${3:?}"
readonly FLAVOR="${4:-}"
readonly BUILD_TYPES="${5:-debug,release}"
readonly INCLUDE_AAB="${6:-true}"
readonly SIGNING="${7:-false}"
readonly SKIP_TESTS="${8:-false}"
readonly VERSION="${9:-unknown}"
readonly VERSION_CODE="${10:-unknown}"
readonly DEBUG_NAME="${11:-}"
readonly RELEASE_NAME="${12:-}"
readonly AAB_NAME="${13:-}"

{
  printf "## Android Variants Build Summary 📱\n"
  printf "\n"
  printf "### Configuration\n"
  printf "| Setting | Value |\n"
  printf "|---------|-------|\n"
  printf "| **Java** | %s (%s) |\n" "$JAVA_VERSION" "$JDK_DIST"
  printf "| **Module** | %s |\n" "$BUILD_MODULE"
  printf "| **Flavor** | %s |\n" "${FLAVOR:-default}"
  printf "| **Build Types** | %s |\n" "$BUILD_TYPES"
  printf "| **Include AAB** | %s |\n" "$([[ "$INCLUDE_AAB" == "true" ]] && echo "✓" || echo "✗")"
  printf "| **Signing** | %s |\n" "$([[ "$SIGNING" == "true" ]] && echo "✓ Enabled" || echo "⊘ Disabled")"
  printf "| **Tests** | %s |\n" "$([[ "$SKIP_TESTS" == "true" ]] && echo "⊘ Skipped" || echo "✓ Executed")"

  if [[ -n "$VERSION" ]] && [[ "$VERSION" != "unknown" ]]; then
    printf "| **Version** | %s (%s) |\n" "$VERSION" "$VERSION_CODE"
  fi

  printf "\n"
  printf "### Artifacts Generated\n"
  if [[ "$BUILD_TYPES" == *"debug"* ]]; then
    printf "- ✓ Debug APK: \`%s\`\n" "$DEBUG_NAME"
  fi
  if [[ "$BUILD_TYPES" == *"release"* ]]; then
    printf "- ✓ Release APK: \`%s\`\n" "$RELEASE_NAME"
  fi
  if [[ "$INCLUDE_AAB" == "true" ]] && [[ "$BUILD_TYPES" == *"release"* ]]; then
    printf "- ✓ Release AAB: \`%s\`\n" "$AAB_NAME"
  fi

  printf "\n"
  printf "*Build completed at %s*\n" "$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
}
