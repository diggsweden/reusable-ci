#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Generate Android build summary for GitHub Actions step summary
#
# Required env: JAVA_VERSION, JDK_DIST, BUILD_MODULE
# Optional env: FLAVOR, BUILD_TYPES, INCLUDE_AAB, SIGNING, SKIP_TESTS,
#               VERSION, VERSION_CODE, DEBUG_NAME, RELEASE_NAME, AAB_NAME

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

readonly JAVA_VERSION="${JAVA_VERSION:?JAVA_VERSION is required}"
readonly JDK_DIST="${JDK_DIST:?JDK_DIST is required}"
readonly BUILD_MODULE="${BUILD_MODULE:?BUILD_MODULE is required}"
readonly FLAVOR="${FLAVOR:-}"
readonly BUILD_TYPES="${BUILD_TYPES:-debug,release}"
readonly INCLUDE_AAB="${INCLUDE_AAB:-true}"
readonly SIGNING="${SIGNING:-false}"
readonly SKIP_TESTS="${SKIP_TESTS:-false}"
readonly VERSION="${VERSION:-unknown}"
readonly VERSION_CODE="${VERSION_CODE:-unknown}"
readonly DEBUG_NAME="${DEBUG_NAME:-}"
readonly RELEASE_NAME="${RELEASE_NAME:-}"
readonly AAB_NAME="${AAB_NAME:-}"

bool_icon() {
  [[ "$1" == "true" ]] && printf "✓" || printf "✗"
}

generate_summary() {
  printf "## Android Variants Build Summary 📱\n\n"

  printf "### Configuration\n"
  printf "| Setting | Value |\n"
  printf "|---------|-------|\n"
  printf "| **Java** | %s (%s) |\n" "$JAVA_VERSION" "$JDK_DIST"
  printf "| **Module** | %s |\n" "$BUILD_MODULE"
  printf "| **Flavor** | %s |\n" "${FLAVOR:-default}"
  printf "| **Build Types** | %s |\n" "$BUILD_TYPES"
  printf "| **Include AAB** | %s |\n" "$(bool_icon "$INCLUDE_AAB")"
  printf "| **Signing** | %s |\n" "$(ci_bool_status "$SIGNING")"
  printf "| **Tests** | %s |\n" "$([[ "$SKIP_TESTS" == "true" ]] && printf "⊘ Skipped" || printf "✓ Executed")"

  if [[ -n "$VERSION" && "$VERSION" != "unknown" ]]; then
    printf "| **Version** | %s (%s) |\n" "$VERSION" "$VERSION_CODE"
  fi

  printf "\n### Artifacts Generated\n"

  if [[ "$BUILD_TYPES" == *"debug"* ]]; then
    printf "✓ Debug APK: \`%s\`\n" "$DEBUG_NAME"
  fi

  if [[ "$BUILD_TYPES" == *"release"* ]]; then
    printf "✓ Release APK: \`%s\`\n" "$RELEASE_NAME"
  fi

  if [[ "$INCLUDE_AAB" == "true" && "$BUILD_TYPES" == *"release"* ]]; then
    printf "✓ Release AAB: \`%s\`\n" "$AAB_NAME"
  fi

  printf "\n*Build completed at %s*\n" "$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
}

main() {
  generate_summary
}

main "$@"
