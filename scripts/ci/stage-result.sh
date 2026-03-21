#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Stage result aggregation helpers
#
# Usage: source this file from stage-result scripts
#   source "$SCRIPT_DIR/../ci/stage-result.sh"
#
# Requires: ci/output.sh must be sourced first (for ci_json_bool)
#
# Functions:
#   ci_aggregate_results <r1> <r2> ...       Aggregate: failure > cancelled > success
#   ci_stage_result <ran> <r1> <r2> ...      If not ran → skipped, else aggregate
#   ci_build_targets_json "k:v" "k:v" ...    Build {"k":"v","k":"v"} JSON object
#   ci_stage_result_json <stage> <result> <ran> <targets> ["k:v" ...]
#                                            Build full stage result JSON envelope

# Aggregate results with priority: failure > cancelled > success
ci_aggregate_results() {
  local r
  for r in "$@"; do
    [[ "$r" == "failure" ]] && {
      printf 'failure'
      return
    }
  done
  for r in "$@"; do
    [[ "$r" == "cancelled" ]] && {
      printf 'cancelled'
      return
    }
  done
  printf 'success'
}

# Determine stage result: if not ran → skipped, else aggregate
ci_stage_result() {
  local stage_ran="$1"
  shift
  if [[ "$stage_ran" != "true" ]]; then
    printf 'skipped'
  else
    ci_aggregate_results "$@"
  fi
}

# Build JSON object from colon-separated key:value pairs
ci_build_targets_json() {
  local first=true json="{"
  local pair key value
  for pair in "$@"; do
    key="${pair%%:*}"
    value="${pair#*:}"
    [[ "$first" == "true" ]] && first=false || json+=","
    json+="\"$key\":\"$value\""
  done
  json+="}"
  printf '%s' "$json"
}

# Build stage result JSON envelope
# Optional trailing key:value pairs are inserted as extra fields before "targets"
ci_stage_result_json() {
  local stage="$1" result="$2" ran="$3" targets="$4"
  shift 4
  local json
  json="{\"stage\":\"$stage\",\"result\":\"$result\",\"ran\":$(ci_json_bool "$ran")"
  local pair key value
  for pair in "$@"; do
    key="${pair%%:*}"
    value="${pair#*:}"
    json+=",\"$key\":\"$value\""
  done
  json+=",\"targets\":$targets}"
  printf '%s' "$json"
}
