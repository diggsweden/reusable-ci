#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"
source "$SCRIPT_DIR/../ci/stage-result.sh"

main() {
  local registry_result central_result appstore_result googleplay_result containers_result

  registry_result="$(ci_normalize_result "${PUBLISH_MAVEN_REGISTRY_RESULT:-skipped}")"
  central_result="$(ci_normalize_result "${PUBLISH_MAVEN_CENTRAL_RESULT:-skipped}")"
  appstore_result="$(ci_normalize_result "${PUBLISH_APPLE_APPSTORE_RESULT:-skipped}")"
  googleplay_result="$(ci_normalize_result "${PUBLISH_GOOGLE_PLAY_RESULT:-skipped}")"
  containers_result="$(ci_normalize_result "${BUILD_CONTAINERS_RESULT:-skipped}")"

  local stage_ran=false
  if [[ "${GITHUBPACKAGES_ARTIFACTS:-[]}" != '[]' || "${MAVENCENTRAL_ARTIFACTS:-[]}" != '[]' || "${XCODEIOS_ARTIFACTS:-[]}" != '[]' || "${GOOGLEPLAY_ARTIFACTS:-[]}" != '[]' || "${CONTAINERS:-[]}" != '[]' ]]; then
    stage_ran=true
  fi

  local stage_result
  stage_result="$(ci_stage_result "$stage_ran" "$registry_result" "$central_result" "$appstore_result" "$googleplay_result" "$containers_result")"

  local targets_json result_json
  targets_json="$(ci_build_targets_json "githubpackages:$registry_result" "mavencentral:$central_result" "appleappstore:$appstore_result" "googleplay:$googleplay_result" "containers:$containers_result")"
  result_json="$(ci_stage_result_json "publish" "$stage_result" "$stage_ran" "$targets_json")"

  ci_output "stage-ran" "$stage_ran"
  ci_output "stage-result" "$stage_result"
  ci_output "result-json" "$result_json"
}

main "$@"
