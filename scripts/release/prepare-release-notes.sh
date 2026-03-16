#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

main() {
  readonly SOURCE_FILE="${1:-ReleasenotesTmp}"
  readonly TARGET_FILE="${2:-release-notes.md}"
  readonly RELEASE_VERSION="${3:-}"
  readonly RELEASE_COMMIT="${4:-}"

  if [[ -f "$SOURCE_FILE" ]]; then
    printf "Changelog artifact found (%s bytes)\n" "$(stat -c%s "$SOURCE_FILE" 2>/dev/null || stat -f%z "$SOURCE_FILE" 2>/dev/null || printf '?')"
    cp "$SOURCE_FILE" "$TARGET_FILE"
    printf "Using git-cliff generated release notes\n"
  elif [[ -n "$RELEASE_VERSION" ]]; then
    printf "No changelog artifact found - creating fallback\n"
    {
      printf "# Release %s\n\n" "$RELEASE_VERSION"
      if [[ -n "$RELEASE_COMMIT" ]]; then
        printf "Release created from commit %s\n" "$RELEASE_COMMIT"
      fi
    } >"$TARGET_FILE"
  else
    printf "No release notes generated\n"
    touch "$TARGET_FILE"
  fi
}

main "$@"
