#!/bin/bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

REGISTRY="${1}"
IMAGE_NAME="${2}"
REPOSITORY="${3}"
REPOSITORY_OWNER="${4}"

if [ -z "$IMAGE_NAME" ]; then
  IMAGE_NAME="$REPOSITORY"
fi

if [[ "$IMAGE_NAME" != *"/"* ]] || [[ "$IMAGE_NAME" != *"."* ]]; then
  if [ "$REGISTRY" = "docker.io" ]; then
    IMAGE_NAME="$REPOSITORY_OWNER/$IMAGE_NAME"
  else
    IMAGE_NAME="$REGISTRY/$IMAGE_NAME"
  fi
fi

echo "Image name: $IMAGE_NAME"
echo "name=$IMAGE_NAME"
