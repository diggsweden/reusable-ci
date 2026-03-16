#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  readonly EXPLICIT_FILE_PATTERN="${EXPLICIT_FILE_PATTERN:-}"
  readonly PROJECT_TYPE="${PROJECT_TYPE:?PROJECT_TYPE is required}"
  readonly SCRIPT_ROOT="${SCRIPT_ROOT:?SCRIPT_ROOT is required}"

  local pattern

  if [[ -n "$EXPLICIT_FILE_PATTERN" ]]; then
    pattern="$EXPLICIT_FILE_PATTERN"
  else
    pattern=$(bash "$SCRIPT_ROOT/get-file-pattern.sh" "$PROJECT_TYPE" "")
  fi

  ci_output "pattern" "$pattern"
}

main "$@"
