#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  readonly PACKAGE_NAME="${PACKAGE_NAME:?PACKAGE_NAME is required}"
  readonly VERSION="${VERSION:?VERSION is required}"
  readonly NODE_VERSION="${NODE_VERSION:?NODE_VERSION is required}"
  readonly SKIP_TESTS="${SKIP_TESTS:-false}"

  {
    printf "## NPM Build Summary 🔨\n"
    printf "\n"
    printf "%s **Package:** \`%s@%s\`\n" "-" "$PACKAGE_NAME" "$VERSION"
    printf "%s **Node.js:** %s\n" "-" "$NODE_VERSION"
    ci_test_status "$SKIP_TESTS"
    printf "\n"
    printf "*Build completed at %s*\n" "$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
  } >>"$(ci_summary_file)"
}

main "$@"
