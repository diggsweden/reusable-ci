#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  if [[ -z "${ANDROID_KEYSTORE_BASE64:-}" ]]; then
    ci_log_error "ANDROID_KEYSTORE secret not found but enable-signing is true"
    exit 1
  fi

  printf "%s" "$ANDROID_KEYSTORE_BASE64" | base64 -d >release.keystore
  printf "✓ Android keystore decoded successfully\n" >&2
  printf "ANDROID_KEYSTORE_PATH=%s/release.keystore\n" "$PWD"
}

main "$@"
