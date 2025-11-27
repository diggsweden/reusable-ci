#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

PROJECT_TYPE="${1:-}"
ARTIFACT_DIR="${2:-}"

if [[ -z "$PROJECT_TYPE" || -z "$ARTIFACT_DIR" ]]; then
  printf "::error::Usage: verify-artifacts.sh <project-type> <artifact-dir>\n"
  exit 1
fi

case "$PROJECT_TYPE" in
maven)
  if [[ ! -d "$ARTIFACT_DIR" || -z "$(ls -A "$ARTIFACT_DIR"/*.jar 2>/dev/null)" ]]; then
    printf "::warning::No Maven artifacts found in %s/\n" "$ARTIFACT_DIR"
    printf "Container build may fail if Containerfile expects JAR files\n"
    printf "This is acceptable if container builds from source instead\n"
  else
    printf "✓ Maven artifacts found:\n"
    ls -lh "$ARTIFACT_DIR"/*.jar
  fi
  ;;
npm)
  if [[ ! -d "$ARTIFACT_DIR" || -z "$(ls -A "$ARTIFACT_DIR" 2>/dev/null)" ]]; then
    printf "::warning::No NPM artifacts found in %s/\n" "$ARTIFACT_DIR"
    printf "Container build may fail if Containerfile expects built files\n"
    printf "This is acceptable if container builds from source instead\n"
  else
    printf "✓ NPM artifacts found:\n"
    ls -lh "$ARTIFACT_DIR"
  fi
  ;;
*)
  printf "Unknown project type: %s\n" "$PROJECT_TYPE"
  exit 1
  ;;
esac
