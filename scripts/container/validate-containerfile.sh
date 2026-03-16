#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  readonly CONTAINERFILE="${1:?Usage: $0 <containerfile>}"

  if [ ! -f "$CONTAINERFILE" ]; then
    printf "Error: Containerfile '%s' not found\n" "$CONTAINERFILE"
    exit 1
  fi

  printf "Using containerfile: %s\n" "$CONTAINERFILE"
  ci_output "containerfile" "$CONTAINERFILE"
}

main "$@"
