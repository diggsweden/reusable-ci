#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

readonly IMAGE_NAME="${1:?Usage: $0 <image-name> <repository> <registry> <enforce-namespace>}"
readonly REPOSITORY="${2:?Usage: $0 <image-name> <repository> <registry> <enforce-namespace>}"
readonly REGISTRY="${3:?Usage: $0 <image-name> <repository> <registry> <enforce-namespace>}"
readonly ENFORCE_NAMESPACE="${4:?Usage: $0 <image-name> <repository> <registry> <enforce-namespace>}"

printf "Validating image namespace...\n"
printf "  Image: %s\n" "$IMAGE_NAME"
printf "  Repository: %s\n" "$REPOSITORY"
printf "  Registry: %s\n" "$REGISTRY"
printf "  Enforced namespace: %s\n" "$ENFORCE_NAMESPACE"

REPO_SHORT="${REPOSITORY##*/}"

if [[ "$REGISTRY" == "ghcr.io" ]]; then
  EXPECTED_PREFIX="ghcr.io/${ENFORCE_NAMESPACE}/${REPO_SHORT}"

  if [[ ! "$IMAGE_NAME" =~ ^${EXPECTED_PREFIX}(-.*)?$ ]]; then
    printf "::error::Security: Image name must start with '%s'\n" "${EXPECTED_PREFIX}"
    printf "::error::Got: %s\n" "$IMAGE_NAME"
    printf "::error::This prevents pushing to unauthorized namespaces\n"
    exit 1
  fi

  printf "✓ Image namespace validated: %s\n" "$IMAGE_NAME"
else
  printf "⚠ Non-GHCR registry - namespace validation skipped\n"
fi
