#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  readonly IMAGE_NAME="${IMAGE_NAME:?IMAGE_NAME is required}"
  readonly IMAGE_DIGEST="${IMAGE_DIGEST:?IMAGE_DIGEST is required}"
  readonly IMAGE_METADATA="${IMAGE_METADATA:?IMAGE_METADATA is required}"

  ci_output "image" "$IMAGE_NAME"
  printf "Image: %s\n" "$IMAGE_NAME"
  printf "Digest: %s\n" "$IMAGE_DIGEST"
  printf "Metadata: %s\n" "$IMAGE_METADATA"
}

main "$@"
