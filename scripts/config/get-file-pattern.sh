#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
#
# Get file pattern for version bump based on project type
#
# Usage (positional args): get-file-pattern.sh <project-type> [custom-pattern]
# Usage (env vars):        EXPLICIT_FILE_PATTERN=... PROJECT_TYPE=... get-file-pattern.sh
#
# When called with no args, reads PROJECT_TYPE and EXPLICIT_FILE_PATTERN from env
# and writes the result to CI output as "pattern=<value>".
#
# Returns the appropriate file pattern for git commit during version bump

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

get_pattern() {
  local project_type="$1"

  case "$project_type" in
  maven) printf "CHANGELOG.md :(glob)**/pom.xml" ;;
  npm) printf "CHANGELOG.md package.json package-lock.json" ;;
  gradle | gradle-android) printf "CHANGELOG.md gradle.properties build.gradle.kts settings.gradle.kts" ;;
  xcode-ios) printf "CHANGELOG.md versions.xcconfig :(glob)**/*.xcconfig" ;;
  python) printf "CHANGELOG.md pyproject.toml" ;;
  go) printf "CHANGELOG.md go.mod" ;;
  rust) printf "CHANGELOG.md Cargo.toml Cargo.lock" ;;
  *) printf "CHANGELOG.md" ;;
  esac
}

main() {
  local project_type="${1:-${PROJECT_TYPE:-}}"
  local custom_pattern="${2:-${EXPLICIT_FILE_PATTERN:-}}"

  if [[ -z "$project_type" ]]; then
    printf "Error: PROJECT_TYPE is required\n" >&2
    exit 1
  fi

  if [[ -n "$custom_pattern" ]]; then
    printf "%s\n" "$custom_pattern"
    # Write to CI output when called via env vars (no positional args)
    if [[ $# -eq 0 ]]; then
      ci_output "pattern" "$custom_pattern"
    fi
    exit 0
  fi

  local pattern
  pattern=$(get_pattern "$project_type")

  if [[ $# -eq 0 ]]; then
    ci_output "pattern" "$pattern"
  fi

  printf "%s\n" "$pattern"
}

main "$@"
