#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
#
# Build summary for Gradle JVM projects (libraries/apps/plugins).
# Android summaries live in write-android-build-summary.sh.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  readonly JAVA_VERSION="${JAVA_VERSION:?JAVA_VERSION is required}"
  readonly GRADLE_TASKS="${GRADLE_TASKS:?GRADLE_TASKS is required}"
  readonly SKIP_TESTS="${SKIP_TESTS:-false}"
  readonly VERSION="${VERSION:-}"

  {
    printf "## Gradle Build Summary 🔨\n"
    printf "\n"
    printf "%s **Java:** %s\n" "-" "$JAVA_VERSION"
    printf "%s **Tasks:** %s\n" "-" "$GRADLE_TASKS"
    ci_test_status "$SKIP_TESTS"

    if [[ -n "$VERSION" ]]; then
      printf "%s **Version:** %s\n" "-" "$VERSION"
    fi

    printf "\n"
    printf "*Build completed at %s*\n" "$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
  } >>"$(ci_summary_file)"
}

main "$@"
