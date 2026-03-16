#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

resolve_project_name() {
  local artifact_name="$1"
  local repository="$2"

  if [[ -n "$artifact_name" ]]; then
    printf "Using artifact name: %s\n" "$artifact_name" >&2
    printf "%s\n" "$artifact_name"
  else
    local project_name
    project_name=$(basename "$repository")
    printf "Using repository name: %s\n" "$project_name" >&2
    printf "%s\n" "$project_name"
  fi
}

write_outputs() {
  local version="$1"
  local version_no_v="$2"
  local project_name="$3"

  {
    printf "version=%s\n" "$version"
    printf "version-no-v=%s\n" "$version_no_v"
    printf "project-name=%s\n" "$project_name"
  } >>"$(ci_output_file)"
}

main() {
  readonly VERSION="${1:?Usage: $0 <version> <artifact-name> <repository>}"
  readonly ARTIFACT_NAME="${2:-}"
  readonly REPOSITORY="${3:?Usage: $0 <version> <artifact-name> <repository>}"
  readonly VERSION_NO_V="${VERSION#v}"
  local project_name

  project_name=$(resolve_project_name "$ARTIFACT_NAME" "$REPOSITORY")
  write_outputs "$VERSION" "$VERSION_NO_V" "$project_name"
}

main "$@"
