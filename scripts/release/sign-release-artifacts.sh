#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Sign release artifacts with GPG
#
# Required env: GPG_KEY_ID

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

sign_checksums_file() {
  local gpg_key_id="$1"

  if [[ -s "$CI_CHECKSUMS_FILE" ]]; then
    printf "Signing %s with GPG\n" "$CI_CHECKSUMS_FILE"
    ci_gpg_sign "$gpg_key_id" "$CI_CHECKSUMS_FILE"
  fi
}

sign_release_artifacts() {
  local gpg_key_id="$1"

  if [[ -d "./release-artifacts" ]]; then
    printf "Signing individual artifacts\n"
    while IFS= read -r -d '' artifact; do
      printf "Signing %s\n" "$(basename "$artifact")"
      ci_gpg_sign "$gpg_key_id" "$artifact"
      mv "${artifact}.asc" "$(basename "${artifact}").asc"
    done < <(ci_find_release_artifacts)
  fi
}

main() {
  readonly GPG_KEY_ID="${GPG_KEY_ID:?GPG_KEY_ID is required}"

  sign_checksums_file "$GPG_KEY_ID"
  sign_release_artifacts "$GPG_KEY_ID"
}

main
