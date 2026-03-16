#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

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

validate_type_artifacts() {
  local type_label="$1"
  local artifact_glob="$2"
  local expect_label="$3"

  # shellcheck disable=SC2086 # Intentional globbing
  if [[ ! -d "$ARTIFACT_DIR" || -z "$(ls -A "$ARTIFACT_DIR"/$artifact_glob 2>/dev/null)" ]]; then
    printf "::warning::No %s artifacts found in %s/\n" "$type_label" "$ARTIFACT_DIR"
    printf "Container build may fail if Containerfile expects %s\n" "$expect_label"
    printf "This is acceptable if container builds from source instead\n"
  else
    printf "✓ %s artifacts found:\n" "$type_label"
    # shellcheck disable=SC2086 # Intentional globbing
    ls -lh "$ARTIFACT_DIR"/$artifact_glob
    check_containerfile_rebuilds
  fi
}

main() {
  local PROJECT_TYPE="${1:-}"
  local ARTIFACT_DIR="${2:-}"
  local CONTAINERFILE_PATH="${3:-Containerfile}"

  if [[ -z "$PROJECT_TYPE" || -z "$ARTIFACT_DIR" ]]; then
    printf "::error::Usage: validate-artifacts.sh <project-type> <artifact-dir> [containerfile-path]\n"
    exit 1
  fi

  case "$PROJECT_TYPE" in
  maven) validate_type_artifacts "Maven" "*.jar" "JAR files" ;;
  npm) validate_type_artifacts "NPM" "*" "built files" ;;
  gradle) validate_type_artifacts "Gradle" "*.jar" "JAR files" ;;
  *)
    printf "Unknown project type: %s\n" "$PROJECT_TYPE"
    exit 1
    ;;
  esac
}

main "$@"
