#!/bin/bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
#
# SPDX-License-Identifier: CC0-1.0
#
# Get file pattern for version bump based on project type
#
# Usage: get-file-pattern.sh <project-type> [custom-pattern]
#
# Returns the appropriate file pattern for git commit during version bump

set -euo pipefail

get_pattern() {
  local project_type="$1"

  case "$project_type" in
  maven) printf "CHANGELOG.md :(glob)**/pom.xml" ;;
  npm) printf "CHANGELOG.md package.json package-lock.json" ;;
  gradle | gradle-android) printf "CHANGELOG.md gradle.properties build.gradle.kts settings.gradle.kts build.gradle settings.gradle" ;;
  xcode-ios) printf "CHANGELOG.md versions.xcconfig :(glob)**/*.xcconfig" ;;
  python) printf "CHANGELOG.md pyproject.toml" ;;
  go) printf "CHANGELOG.md go.mod" ;;
  rust) printf "CHANGELOG.md Cargo.toml Cargo.lock" ;;
  *) printf "CHANGELOG.md" ;;
  esac
}

main() {
  local project_type="${1:-}"
  local custom_pattern="${2:-}"

  if [[ -z "$project_type" ]]; then
    printf "Error: PROJECT_TYPE is required\n" >&2
    exit 1
  fi

  if [[ -n "$custom_pattern" ]]; then
    printf "%s\n" "$custom_pattern"
    exit 0
  fi

  get_pattern "$project_type"
  printf "\n"
}

main "$@"
