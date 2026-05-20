#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Debug helper that lists workspace structure and GitHub context
# Usage: debug-workspace.sh
#
# Environment variables:
#   ACTION_REPOSITORY  - github.action_repository
#   ACTION_REF         - github.action_ref

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  printf "=== Workspace structure ===\n"
  ls -la
  printf "\n"
  printf "=== .github-shared structure ===\n"
  ls -la .github-shared/ || printf ".github-shared not found\n"
  printf "\n"
  printf "=== Looking for scripts ===\n"
  find .github-shared -name "validate-*.sh" 2>/dev/null || printf "No scripts found\n"
  printf "\n"
  printf "=== GitHub context ===\n"
  printf "action_repository: %s\n" "${ACTION_REPOSITORY:-}"
  printf "action_ref: %s\n" "${ACTION_REF:-}"
}

main "$@"
