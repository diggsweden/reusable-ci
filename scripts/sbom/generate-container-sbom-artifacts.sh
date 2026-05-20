#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/env.sh"

resolve_project_name() {
  local repository="$1"

  basename "$repository"
}

main() {
  readonly ARTIFACT_TYPES="${ARTIFACT_TYPES:?ARTIFACT_TYPES is required}"
  readonly CI_REF_NAME="${CI_REF_NAME:?CI_REF_NAME is required}"
  readonly CI_REPO="${CI_REPO:?CI_REPO is required}"
  readonly IMAGE_NAME="${IMAGE_NAME:?IMAGE_NAME is required}"
  readonly IMAGE_DIGEST="${IMAGE_DIGEST:?IMAGE_DIGEST is required}"
  readonly SBOM_SCRIPT_ROOT="${SBOM_SCRIPT_ROOT:?SBOM_SCRIPT_ROOT is required}"

  local version
  local project_name
  local image

  version="${CI_REF_NAME#v}"
  project_name=$(resolve_project_name "$CI_REPO")
  image="${IMAGE_NAME}@${IMAGE_DIGEST}"

  bash "${SBOM_SCRIPT_ROOT}/generate-container-sbom.sh" \
    "$ARTIFACT_TYPES" \
    "$version" \
    "$project_name" \
    "$image" \
    "$SBOM_SCRIPT_ROOT"
}

main "$@"
