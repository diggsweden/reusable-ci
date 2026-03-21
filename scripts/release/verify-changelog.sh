#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Verify that a changelog file was generated and has content.
#
# Required env:
#   CHANGELOG_FILE   Path to the generated changelog file

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local CHANGELOG_FILE="${CHANGELOG_FILE:?CHANGELOG_FILE is required}"

  if [[ -f "$CHANGELOG_FILE" ]]; then
    printf "✓ Changelog generated successfully: %s\n" "$CHANGELOG_FILE"
    printf "  • File size: %s bytes\n" "$(stat -c%s "$CHANGELOG_FILE")"
    printf "  • Line count: %s\n" "$(wc -l <"$CHANGELOG_FILE")"

    # Show first few lines for debugging
    printf "\n"
    printf "Preview (first 10 lines):\n"
    printf "========================\n"
    head -10 "$CHANGELOG_FILE"
  else
    ci_log_error "No changelog generated"
    exit 1
  fi
}

main "$@"
