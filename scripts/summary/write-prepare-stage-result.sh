#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local prepare_result stage_ran stage_result

  prepare_result="$(ci_normalize_result "${PREPARE_RELEASE_RESULT:-skipped}")"

  stage_ran=false
  if [[ "${SHOULD_RUN_VERSION_BUMP:-false}" == 'true' && "${ARTIFACTS:-[]}" != '[]' ]]; then
    stage_ran=true
  fi

  if [[ "$stage_ran" != 'true' ]]; then
    stage_result='skipped'
  else
    stage_result="$prepare_result"
  fi

  local result_json
  result_json=$(printf '{"stage":"prepare","result":"%s","ran":%s,"targets":{"version-bump":"%s"}}' \
    "$stage_result" "$(ci_json_bool "$stage_ran")" "$prepare_result")

  ci_output "stage-ran" "$stage_ran"
  ci_output "stage-result" "$stage_result"
  ci_output "result-json" "$result_json"
}

main "$@"
