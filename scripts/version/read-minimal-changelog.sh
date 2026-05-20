#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  readonly CHANGELOG_FILE="${1:-minimal-changelog.txt}"

  if [[ -f "$CHANGELOG_FILE" ]]; then
    local content
    content=$(<"$CHANGELOG_FILE")
    printf "%s\n" "$content" | ci_output_multiline "content"
  else
    ci_output "content" "No changes for this release"
  fi
}

main "$@"
