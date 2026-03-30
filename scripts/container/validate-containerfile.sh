#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  readonly CONTAINERFILE="${1:?Usage: $0 <containerfile>}"

  # Check exact path first
  if [ -f "$CONTAINERFILE" ]; then
    printf "Using containerfile: %s\n" "$CONTAINERFILE"
    ci_output "containerfile" "$CONTAINERFILE"
    return 0
  fi

  # Try matching Dockerfile* and Containerfile* patterns in the same directory
  local dir
  dir="$(dirname "$CONTAINERFILE")"
  local base
  base="$(basename "$CONTAINERFILE")"
  local matches=()

  while IFS= read -r -d '' match; do
    matches+=("$match")
  done < <(find "$dir" -maxdepth 1 -type f \( -name "Dockerfile*" -o -name "Containerfile*" \) -print0 2>/dev/null)

  if [ ${#matches[@]} -eq 0 ]; then
    printf "Error: Containerfile '%s' not found and no Dockerfile*/Containerfile* match found in '%s'\n" "$CONTAINERFILE" "$dir"
    exit 1
  fi

  if [ ${#matches[@]} -gt 1 ]; then
    printf "Error: Multiple containerfiles found in '%s', please specify an exact path:\n" "$dir"
    printf "  %s\n" "${matches[@]}"
    exit 1
  fi

  printf "Using containerfile: %s\n" "${matches[0]}"
  ci_output "containerfile" "${matches[0]}"
}

main "$@"
