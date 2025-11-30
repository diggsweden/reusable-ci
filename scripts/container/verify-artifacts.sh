#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

PROJECT_TYPE="${1:-}"
ARTIFACT_DIR="${2:-}"
CONTAINERFILE_PATH="${3:-Containerfile}"

if [[ -z "$PROJECT_TYPE" || -z "$ARTIFACT_DIR" ]]; then
  printf "::error::Usage: verify-artifacts.sh <project-type> <artifact-dir> [containerfile-path]\n"
  exit 1
fi

check_containerfile_rebuilds() {
  if [[ ! -f "$CONTAINERFILE_PATH" ]]; then
    return
  fi

  local rebuild_patterns=(
    "mvn.*package"
    "mvn.*install"
    "mvnw.*package"
    "mvnw.*install"
    "gradle.*build"
    "gradle.*assemble"
    "npm run build"
    "npm.*build.*run"
  )
  local found_rebuild=false

  for pattern in "${rebuild_patterns[@]}"; do
    if grep -qE "$pattern" "$CONTAINERFILE_PATH" 2>/dev/null; then
      found_rebuild=true
      break
    fi
  done

  if [[ "$found_rebuild" = true ]]; then
    printf "::warning::Containerfile rebuilds from source - downloaded artifacts may be ignored\n"
    printf "This means the container build will NOT use pre-built artifacts, defeating the purpose of separate build steps.\n"
    printf "Consider updating Containerfile to COPY pre-built artifacts instead of rebuilding.\n"
    printf "See: %s\n" "$CONTAINERFILE_PATH"
  fi
}

case "$PROJECT_TYPE" in
maven)
  if [[ ! -d "$ARTIFACT_DIR" || -z "$(ls -A "$ARTIFACT_DIR"/*.jar 2>/dev/null)" ]]; then
    printf "::warning::No Maven artifacts found in %s/\n" "$ARTIFACT_DIR"
    printf "Container build may fail if Containerfile expects JAR files\n"
    printf "This is acceptable if container builds from source instead\n"
  else
    printf "✓ Maven artifacts found:\n"
    ls -lh "$ARTIFACT_DIR"/*.jar
    check_containerfile_rebuilds
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
    check_containerfile_rebuilds
  fi
  ;;
gradle)
  if [[ ! -d "$ARTIFACT_DIR" || -z "$(ls -A "$ARTIFACT_DIR"/*.jar 2>/dev/null)" ]]; then
    printf "::warning::No Gradle artifacts found in %s/\n" "$ARTIFACT_DIR"
    printf "Container build may fail if Containerfile expects JAR files\n"
    printf "This is acceptable if container builds from source instead\n"
  else
    printf "✓ Gradle artifacts found:\n"
    ls -lh "$ARTIFACT_DIR"/*.jar
    check_containerfile_rebuilds
  fi
  ;;
*)
  printf "Unknown project type: %s\n" "$PROJECT_TYPE"
  exit 1
  ;;
esac
