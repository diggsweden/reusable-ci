#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  readonly CHANGELOG_FILE="${1:-CHANGELOG.md}"

  if [ ! -f "$CHANGELOG_FILE" ]; then
    ci_log_error "Full changelog ($CHANGELOG_FILE) not found"
    printf "This file is required for the version bump commit\n"
    exit 1
  fi

  printf "✓ Full changelog found (%s lines)\n" "$(wc -l <"$CHANGELOG_FILE")"
}

main "$@"
