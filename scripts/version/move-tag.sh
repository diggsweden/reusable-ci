#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

LATEST_TAG=$(git describe --tags --abbrev=0)
PREV_SHA=$(git rev-parse HEAD~1)
TAG_SHA=$(git rev-list -n 1 "$LATEST_TAG")

if [[ "$TAG_SHA" == "$PREV_SHA" ]]; then
  printf "Moving tag %s from previous commit to current\n" "$LATEST_TAG"
  git tag -f -s "$LATEST_TAG" -m "$LATEST_TAG"
  git push --force origin "$LATEST_TAG"
else
  printf "✗ Tag %s points to unexpected commit\n" "$LATEST_TAG"
  printf "Expected: %s (HEAD~1)\n" "$PREV_SHA"
  printf "Found: %s\n" "$TAG_SHA"
  exit 1
fi
