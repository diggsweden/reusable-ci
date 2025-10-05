#!/bin/bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

PROJECT_TYPE="${1}"
ARTIFACT_DIR="${2}"

case "$PROJECT_TYPE" in
maven)
  if [ ! -d "$ARTIFACT_DIR" ] || [ -z "$(ls -A "$ARTIFACT_DIR"/*.jar 2>/dev/null)" ]; then
    echo "::warning::No Maven artifacts found in $ARTIFACT_DIR/"
    echo "Container build may fail if Containerfile expects JAR files"
    echo "This is acceptable if container builds from source instead"
  else
    echo "✓ Maven artifacts found:"
    ls -lh "$ARTIFACT_DIR"/*.jar
  fi
  ;;
npm)
  if [ ! -d "$ARTIFACT_DIR" ] || [ -z "$(ls -A "$ARTIFACT_DIR" 2>/dev/null)" ]; then
    echo "::warning::No NPM artifacts found in $ARTIFACT_DIR/"
    echo "Container build may fail if Containerfile expects built files"
    echo "This is acceptable if container builds from source instead"
  else
    echo "✓ NPM artifacts found:"
    ls -lh "$ARTIFACT_DIR"
  fi
  ;;
*)
  echo "Unknown project type: $PROJECT_TYPE"
  exit 1
  ;;
esac
