#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

main() {
  readonly CHANGELOG_FILE="${1:-CHANGELOG.md}"

  if [ ! -f "$CHANGELOG_FILE" ]; then
    printf "::error::Full changelog (%s) not found\n" "$CHANGELOG_FILE"
    printf "This file is required for the version bump commit\n"
    exit 1
  fi

  printf "✓ Full changelog found (%s lines)\n" "$(wc -l <"$CHANGELOG_FILE")"
}

main "$@"
