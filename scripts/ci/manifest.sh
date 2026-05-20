#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# File-based CI manifest helpers.
#
# Usage: source this file from scripts that need to persist structured data
# between jobs or stages.

ci_results_dir() {
  local dir="${CI_RESULTS_DIR:-.ci-results}"
  mkdir -p "$dir"
  printf '%s' "$dir"
}

ci_manifest_path() {
  local name="$1"
  printf '%s/%s.json' "$(ci_results_dir)" "$name"
}

ci_write_manifest() {
  local name="$1"
  local content="$2"
  local path

  path="$(ci_manifest_path "$name")"
  printf '%s\n' "$content" >"$path"
  printf '%s' "$path"
}

ci_read_manifest() {
  local name="$1"
  local path

  path="$(ci_manifest_path "$name")"
  if [[ -f "$path" ]]; then
    cat "$path"
  fi
}

ci_stage_result_path() {
  local stage="$1"
  ci_manifest_path "${stage}-result"
}

ci_write_stage_result() {
  local stage="$1"
  local content="$2"
  ci_write_manifest "${stage}-result" "$content"
}

ci_read_stage_result() {
  local stage="$1"
  ci_read_manifest "${stage}-result"
}

ci_json_input() {
  local inline_json="${1:-}"
  local path="${2:-}"

  if [[ -n "$path" && -f "$path" ]]; then
    tr -d '\n' <"$path"
  else
    printf '%s' "$inline_json"
  fi
}
