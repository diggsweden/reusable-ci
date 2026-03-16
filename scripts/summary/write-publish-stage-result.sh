#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local github_result central_result appstore_result googleplay_result containers_result
  local stage_ran stage_result

  github_result="$(ci_normalize_result "${PUBLISH_MAVEN_GITHUB_RESULT:-skipped}")"
  central_result="$(ci_normalize_result "${PUBLISH_MAVEN_CENTRAL_RESULT:-skipped}")"
  appstore_result="$(ci_normalize_result "${PUBLISH_APPLE_APPSTORE_RESULT:-skipped}")"
  googleplay_result="$(ci_normalize_result "${PUBLISH_GOOGLE_PLAY_RESULT:-skipped}")"
  containers_result="$(ci_normalize_result "${BUILD_CONTAINERS_RESULT:-skipped}")"

  stage_ran=false
  if [[ "${GITHUBPACKAGES_ARTIFACTS:-[]}" != '[]' || "${MAVENCENTRAL_ARTIFACTS:-[]}" != '[]' || "${XCODEIOS_ARTIFACTS:-[]}" != '[]' || "${GOOGLEPLAY_ARTIFACTS:-[]}" != '[]' || "${CONTAINERS:-[]}" != '[]' ]]; then
    stage_ran=true
  fi

  if [[ "$stage_ran" != 'true' ]]; then
    stage_result='skipped'
  elif [[ "$github_result" == 'failure' || "$central_result" == 'failure' || "$appstore_result" == 'failure' || "$googleplay_result" == 'failure' || "$containers_result" == 'failure' ]]; then
    stage_result='failure'
  elif [[ "$github_result" == 'cancelled' || "$central_result" == 'cancelled' || "$appstore_result" == 'cancelled' || "$googleplay_result" == 'cancelled' || "$containers_result" == 'cancelled' ]]; then
    stage_result='cancelled'
  else
    stage_result='success'
  fi

  local result_json
  result_json=$(printf '{"stage":"publish","result":"%s","ran":%s,"targets":{"githubpackages":"%s","mavencentral":"%s","appleappstore":"%s","googleplay":"%s","containers":"%s"}}' \
    "$stage_result" "$(ci_json_bool "$stage_ran")" "$github_result" "$central_result" "$appstore_result" "$googleplay_result" "$containers_result")

  ci_output "stage-ran" "$stage_ran"
  ci_output "stage-result" "$stage_result"
  ci_output "result-json" "$result_json"
}

main "$@"
