#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  readonly IMAGE_NAME="${1:?Usage: $0 <image-name> <repository> <registry> <enforce-namespace>}"
  readonly REPOSITORY="${2:?Usage: $0 <image-name> <repository> <registry> <enforce-namespace>}"
  readonly REGISTRY="${3:?Usage: $0 <image-name> <repository> <registry> <enforce-namespace>}"
  readonly ENFORCE_NAMESPACE="${4:?Usage: $0 <image-name> <repository> <registry> <enforce-namespace>}"

  printf "Validating image namespace...\n"
  printf "  Image: %s\n" "$IMAGE_NAME"
  printf "  Repository: %s\n" "$REPOSITORY"
  printf "  Registry: %s\n" "$REGISTRY"
  printf "  Enforced namespace: %s\n" "$ENFORCE_NAMESPACE"

  local REPO_SHORT="${REPOSITORY##*/}"

  if [[ "$REGISTRY" == "ghcr.io" ]]; then
    local EXPECTED_PREFIX="ghcr.io/${ENFORCE_NAMESPACE}/${REPO_SHORT}"

    # Accept the prefix exactly, a single -suffix segment (sibling variant
    # package — '-[^/]*'), or a /subpath (multi-container form
    # 'ghcr.io/org/repo/<name>[/...]'), but not the two combined: a name like
    # '<repo>-evil/payload' would be a sibling top-level package masquerading
    # as a subpath under <repo>, with no documented use case.
    if [[ ! "$IMAGE_NAME" =~ ^${EXPECTED_PREFIX}(-[^/]*|/.*)?$ ]]; then
      ci_log_error "Security: Image '${IMAGE_NAME}' is outside the allowed namespace"
      ci_log_error "Allowed: '${EXPECTED_PREFIX}', '${EXPECTED_PREFIX}-<suffix>', or '${EXPECTED_PREFIX}/<subpath>'"
      ci_log_error "This prevents pushing to unauthorized namespaces"
      exit 1
    fi

    printf "✓ Image namespace validated: %s\n" "$IMAGE_NAME"
  else
    printf "⚠ Non-GHCR registry - namespace validation skipped\n"
  fi
}

main "$@"
