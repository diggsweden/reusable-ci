#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

main() {
  local REGISTRY="${1}"
  local IMAGE_NAME="${2}"
  local REPOSITORY="${3}"
  local REPOSITORY_OWNER="${4}"

  if [[ -z "$IMAGE_NAME" ]]; then
    IMAGE_NAME="$REPOSITORY"
  fi

  if [[ "$IMAGE_NAME" != *"/"* ]] || [[ "$IMAGE_NAME" != *"."* ]]; then
    if [[ "$REGISTRY" = "docker.io" ]]; then
      IMAGE_NAME="$REPOSITORY_OWNER/$IMAGE_NAME"
    else
      IMAGE_NAME="$REGISTRY/$IMAGE_NAME"
    fi
  fi

  printf "name=%s\n" "$IMAGE_NAME"
}

main "$@"
