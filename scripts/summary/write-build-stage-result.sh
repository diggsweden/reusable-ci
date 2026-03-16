#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local maven_result npm_result gradle_result gradleandroid_result xcodeios_result
  local stage_ran stage_result

  maven_result="$(ci_normalize_result "${BUILD_MAVEN_RESULT:-skipped}")"
  npm_result="$(ci_normalize_result "${BUILD_NPM_RESULT:-skipped}")"
  gradle_result="$(ci_normalize_result "${BUILD_GRADLE_RESULT:-skipped}")"
  gradleandroid_result="$(ci_normalize_result "${BUILD_GRADLE_ANDROID_RESULT:-skipped}")"
  xcodeios_result="$(ci_normalize_result "${BUILD_XCODE_RESULT:-skipped}")"

  stage_ran=false
  if [[ "${MAVEN_ARTIFACTS:-[]}" != '[]' || "${NPM_ARTIFACTS:-[]}" != '[]' || "${GRADLE_ARTIFACTS:-[]}" != '[]' || "${GRADLEANDROID_ARTIFACTS:-[]}" != '[]' || "${XCODEIOS_ARTIFACTS:-[]}" != '[]' ]]; then
    stage_ran=true
  fi

  if [[ "$stage_ran" != 'true' ]]; then
    stage_result='skipped'
  elif [[ "$maven_result" == 'failure' || "$npm_result" == 'failure' || "$gradle_result" == 'failure' || "$gradleandroid_result" == 'failure' || "$xcodeios_result" == 'failure' ]]; then
    stage_result='failure'
  elif [[ "$maven_result" == 'cancelled' || "$npm_result" == 'cancelled' || "$gradle_result" == 'cancelled' || "$gradleandroid_result" == 'cancelled' || "$xcodeios_result" == 'cancelled' ]]; then
    stage_result='cancelled'
  else
    stage_result='success'
  fi

  local result_json
  result_json=$(printf '{"stage":"build","result":"%s","ran":%s,"targets":{"maven":"%s","npm":"%s","gradle":"%s","gradleandroid":"%s","xcodeios":"%s"}}' \
    "$stage_result" "$(ci_json_bool "$stage_ran")" "$maven_result" "$npm_result" "$gradle_result" "$gradleandroid_result" "$xcodeios_result")

  ci_output "stage-ran" "$stage_ran"
  ci_output "stage-result" "$stage_result"
  ci_output "result-json" "$result_json"
}

main "$@"
