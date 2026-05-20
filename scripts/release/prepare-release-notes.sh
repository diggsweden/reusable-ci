#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Prepare release notes from changelog artifact or create fallback
#
# Optional env: SOURCE_FILE, TARGET_FILE, RELEASE_VERSION, RELEASE_COMMIT

set -euo pipefail

main() {
  readonly SOURCE_FILE="${SOURCE_FILE:-ReleasenotesTmp}"
  readonly TARGET_FILE="${TARGET_FILE:-release-notes.md}"
  readonly RELEASE_VERSION="${RELEASE_VERSION:-}"
  readonly RELEASE_COMMIT="${RELEASE_COMMIT:-}"

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

main
