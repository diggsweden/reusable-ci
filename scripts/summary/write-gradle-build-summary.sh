#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

print_signing_status() {
  printf "%s **Signing:** %s\n" "-" "$(ci_bool_status "$1")"
}

main() {
  readonly JAVA_VERSION="${JAVA_VERSION:?JAVA_VERSION is required}"
  readonly BUILD_MODULE="${BUILD_MODULE:?BUILD_MODULE is required}"
  readonly GRADLE_TASKS="${GRADLE_TASKS:?GRADLE_TASKS is required}"
  readonly SKIP_TESTS="${SKIP_TESTS:-false}"
  readonly ENABLE_SIGNING="${ENABLE_SIGNING:-false}"
  readonly VERSION="${VERSION:-}"
  readonly VERSION_CODE="${VERSION_CODE:-}"

  {
    printf "## Gradle Build Summary 🔨\n"
    printf "\n"
    printf "%s **Java:** %s\n" "-" "$JAVA_VERSION"
    printf "%s **Module:** %s\n" "-" "$BUILD_MODULE"
    printf "%s **Tasks:** %s\n" "-" "$GRADLE_TASKS"
    ci_test_status "$SKIP_TESTS"
    print_signing_status "$ENABLE_SIGNING"

    if [[ -n "$VERSION" ]]; then
      printf "%s **Version:** %s (%s)\n" "-" "$VERSION" "$VERSION_CODE"
    fi

    printf "\n"
    printf "*Build completed at %s*\n" "$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
  } >>"$(ci_summary_file)"
}

main "$@"
