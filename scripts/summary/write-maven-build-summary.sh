#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  readonly BUILD_TYPE="${BUILD_TYPE:?BUILD_TYPE is required}"
  readonly GROUP_ID="${GROUP_ID:?GROUP_ID is required}"
  readonly ARTIFACT_ID="${ARTIFACT_ID:?ARTIFACT_ID is required}"
  readonly VERSION="${VERSION:?VERSION is required}"
  readonly JAVA_VERSION="${JAVA_VERSION:?JAVA_VERSION is required}"
  readonly SKIP_TESTS="${SKIP_TESTS:-false}"
  readonly IS_SNAPSHOT="${IS_SNAPSHOT:?IS_SNAPSHOT is required}"

  {
    printf "## Maven Build Summary 🔨\n"
    printf "\n"
    printf "%s **Type:** %s\n" "-" "$BUILD_TYPE"
    printf "%s **Artifact:** \`%s:%s:%s\`\n" "-" "$GROUP_ID" "$ARTIFACT_ID" "$VERSION"
    printf "%s **Java:** %s\n" "-" "$JAVA_VERSION"
    ci_test_status "$SKIP_TESTS"
    printf "%s **Snapshot:** %s\n" "-" "$IS_SNAPSHOT"
    printf "\n"
    printf "*Build completed at %s*\n" "$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
  } >>"$(ci_summary_file)"
}

main "$@"
