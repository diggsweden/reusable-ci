#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

resolve_has_password() {
  local registry_password="$1"

  if [[ -n "$registry_password" ]]; then
    printf "true\n"
  else
    printf "false\n"
  fi
}

main() {
  readonly USE_GITHUB_TOKEN="${USE_GITHUB_TOKEN:?USE_GITHUB_TOKEN is required}"
  readonly TARGET_REGISTRY="${TARGET_REGISTRY:?TARGET_REGISTRY is required}"
  readonly GITHUB_REGISTRY="${GITHUB_REGISTRY:-ghcr.io}"
  readonly REGISTRY_PASSWORD="${REGISTRY_PASSWORD:-}"
  readonly VALIDATE_AUTH_SCRIPT="${VALIDATE_AUTH_SCRIPT:-.github-shared/scripts/registry/validate-auth.sh}"

  local has_password
  has_password=$(resolve_has_password "$REGISTRY_PASSWORD")

  bash "$VALIDATE_AUTH_SCRIPT" \
    "$USE_GITHUB_TOKEN" \
    "$TARGET_REGISTRY" \
    "$GITHUB_REGISTRY" \
    "$has_password"
}

main "$@"
