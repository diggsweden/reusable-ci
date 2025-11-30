#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

readonly TAG_NAME="${1:?Usage: $0 <tag-name>}"

printf "→ Validating Tag Points to Unique Commit\n"

TAG_COMMIT=$(git rev-parse "${TAG_NAME}^{commit}")
printf "✓ Tag '%s' points to commit: %s\n" "$TAG_NAME" "$TAG_COMMIT"

OTHER_TAGS=$(git tag --points-at "$TAG_COMMIT" | grep -v "^${TAG_NAME}$" || true)

if [[ -n "$OTHER_TAGS" ]]; then
  printf "::error::Tag '%s' points to the same commit as other tag(s)\n" "$TAG_NAME"
  printf "\n"
  printf "The following tags also point to commit %s:\n" "$TAG_COMMIT"
  printf "%s\n" "$OTHER_TAGS" | sed 's/^/  - /'
  printf "\n"
  printf "Multiple tags on the same commit cause changelog generation issues.\n"
  printf "This is a known limitation in git-cliff:\n"
  printf "https://github.com/orhun/git-cliff/issues/1036\n"
  printf "\n"
  exit 1
fi

printf "✓ Tag '%s' points to a unique commit\n" "$TAG_NAME"
printf "✓ No other tags found on commit %s\n" "$TAG_COMMIT"
