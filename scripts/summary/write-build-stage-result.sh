#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"
source "$SCRIPT_DIR/../ci/stage-result.sh"

main() {
  local maven_result npm_result gradle_result gradleandroid_result xcodeios_result

  maven_result="$(ci_normalize_result "${BUILD_MAVEN_RESULT:-skipped}")"
  npm_result="$(ci_normalize_result "${BUILD_NPM_RESULT:-skipped}")"
  gradle_result="$(ci_normalize_result "${BUILD_GRADLE_RESULT:-skipped}")"
  gradleandroid_result="$(ci_normalize_result "${BUILD_GRADLE_ANDROID_RESULT:-skipped}")"
  xcodeios_result="$(ci_normalize_result "${BUILD_XCODE_RESULT:-skipped}")"

  local stage_ran=false
  if [[ "${MAVEN_ARTIFACTS:-[]}" != '[]' || "${NPM_ARTIFACTS:-[]}" != '[]' || "${GRADLE_ARTIFACTS:-[]}" != '[]' || "${GRADLEANDROID_ARTIFACTS:-[]}" != '[]' || "${XCODEIOS_ARTIFACTS:-[]}" != '[]' ]]; then
    stage_ran=true
  fi

  local stage_result
  stage_result="$(ci_stage_result "$stage_ran" "$maven_result" "$npm_result" "$gradle_result" "$gradleandroid_result" "$xcodeios_result")"

  local targets_json result_json
  targets_json="$(ci_build_targets_json "maven:$maven_result" "npm:$npm_result" "gradle:$gradle_result" "gradleandroid:$gradleandroid_result" "xcodeios:$xcodeios_result")"
  result_json="$(ci_stage_result_json "build" "$stage_result" "$stage_ran" "$targets_json")"

  ci_output "stage-ran" "$stage_ran"
  ci_output "stage-result" "$stage_result"
  ci_output "result-json" "$result_json"
}

main "$@"
