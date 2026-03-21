#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"
source "$SCRIPT_DIR/../ci/stage-result.sh"

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

  local targets_json result_json
  targets_json="$(ci_build_targets_json "maven:$maven_result" "npm:$npm_result" "gradle:$gradle_result")"
  result_json="$(ci_stage_result_json "dev-build" "$stage_result" "$stage_ran" "$targets_json" "project_type:${PROJECT_TYPE:-unknown}")"

  ci_output "stage-ran" "$stage_ran"
  ci_output "stage-result" "$stage_result"
  ci_output "result-json" "$result_json"
}

main "$@"
