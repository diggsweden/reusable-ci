#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

create_sbom_zip() {
  local project_name="$1"
  local version="$2"
  local script_root="$3"

  bash "$script_root/create-sbom-zip.sh" "$project_name" "$version"
}

sign_sbom_zip_if_present() {
  local project_name="$1"
  local version="$2"
  local version_no_v="$3"
  local sign_artifacts="$4"
  local gpg_key_id="$5"

  local sbom_zip
  sbom_zip=$(ci_sbom_zip_name "$project_name" "$version_no_v")

  if [[ -f "$sbom_zip" ]] && [[ "$sign_artifacts" = "true" ]]; then
    ci_gpg_sign "$gpg_key_id" "$sbom_zip"
  fi
}

main() {
  readonly PROJECT_NAME="${PROJECT_NAME:?PROJECT_NAME is required}"
  readonly VERSION="${VERSION:?VERSION is required}"
  readonly VERSION_NO_V="${VERSION_NO_V:?VERSION_NO_V is required}"
  readonly SIGN_ARTIFACTS="${SIGN_ARTIFACTS:-false}"
  readonly GPG_KEY_ID="${GPG_KEY_ID:-}"
  readonly RELEASE_SCRIPT_ROOT="${RELEASE_SCRIPT_ROOT:?RELEASE_SCRIPT_ROOT is required}"

  create_sbom_zip "$PROJECT_NAME" "$VERSION" "$RELEASE_SCRIPT_ROOT"
  sign_sbom_zip_if_present "$PROJECT_NAME" "$VERSION" "$VERSION_NO_V" "$SIGN_ARTIFACTS" "$GPG_KEY_ID"
}

main "$@"
