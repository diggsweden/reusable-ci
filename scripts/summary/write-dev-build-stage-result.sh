#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local maven_result npm_result gradle_result target_key target_result

  maven_result="$(ci_normalize_result "${BUILD_MAVEN_DEV_RESULT:-skipped}")"
  npm_result="$(ci_normalize_result "${BUILD_NPM_DEV_RESULT:-skipped}")"
  gradle_result="$(ci_normalize_result "${BUILD_GRADLE_DEV_RESULT:-skipped}")"

  case "${PROJECT_TYPE:-}" in
  maven)
    target_key='maven'
    target_result="$maven_result"
    ;;
  npm)
    target_key='npm'
    target_result="$npm_result"
    ;;
  gradle)
    target_key='gradle'
    target_result="$gradle_result"
    ;;
  *)
    target_key='unknown'
    target_result='skipped'
    ;;
  esac

  local stage_ran="false"
  if [[ "$target_key" != "unknown" ]]; then stage_ran="true"; fi

  local stage_result
  if [[ "$stage_ran" == "true" ]]; then
    stage_result="$target_result"
  else
    stage_result="skipped"
  fi

  local result_json
  result_json=$(printf '{"stage":"dev-build","result":"%s","ran":%s,"project_type":"%s","targets":{"maven":"%s","npm":"%s","gradle":"%s"}}' \
    "$stage_result" "$(ci_json_bool "$stage_ran")" "${PROJECT_TYPE:-unknown}" "$maven_result" "$npm_result" "$gradle_result")

  ci_output "stage-ran" "$stage_ran"
  ci_output "stage-result" "$stage_result"
  ci_output "result-json" "$result_json"
}

main "$@"
