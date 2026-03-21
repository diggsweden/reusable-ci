#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

readonly ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
readonly WORKFLOWS_DIR="$ROOT_DIR/.github/workflows"

failures=0

while IFS= read -r file; do
  while IFS=: read -r line_no line_text; do
    [[ -n "$line_no" ]] || continue
    printf '::error file=%s,line=%s::workflow_call input defaults must be literal values, found expression: %s\n' \
      "${file#"$ROOT_DIR"/}" "$line_no" "$line_text"
    failures=1
  done < <(grep -nE '^[[:space:]]+default:[[:space:]].*\$\{\{' "$file" || true)
done < <(find "$WORKFLOWS_DIR" -maxdepth 1 -name '*.yml' -print | sort)

if [[ $failures -ne 0 ]]; then
  exit 1
fi

printf 'Workflow input defaults look valid.\n'
