#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"
source "$SCRIPT_DIR/../ci/stage-result.sh"

main() {
  local maven_registry_result gradle_github_result registry_result
  local maven_central_result gradle_central_result central_result
  local appstore_result googleplay_result containers_result

  maven_registry_result="$(ci_normalize_result "${PUBLISH_MAVEN_REGISTRY_RESULT:-skipped}")"
  gradle_github_result="$(ci_normalize_result "${PUBLISH_GRADLE_GITHUB_RESULT:-skipped}")"
  maven_central_result="$(ci_normalize_result "${PUBLISH_MAVEN_CENTRAL_RESULT:-skipped}")"
  gradle_central_result="$(ci_normalize_result "${PUBLISH_GRADLE_CENTRAL_RESULT:-skipped}")"
  appstore_result="$(ci_normalize_result "${PUBLISH_APPLE_APPSTORE_RESULT:-skipped}")"
  googleplay_result="$(ci_normalize_result "${PUBLISH_GOOGLE_PLAY_RESULT:-skipped}")"
  containers_result="$(ci_normalize_result "${BUILD_CONTAINERS_RESULT:-skipped}")"

  # Merge Maven and Gradle GitHub Packages results into a single githubpackages target.
  # success > cancelled > failure > skipped (first match wins)
  if [[ "$maven_registry_result" == "success" || "$gradle_github_result" == "success" ]]; then
    registry_result="success"
  elif [[ "$maven_registry_result" == "failure" || "$gradle_github_result" == "failure" ]]; then
    registry_result="failure"
  elif [[ "$maven_registry_result" == "cancelled" || "$gradle_github_result" == "cancelled" ]]; then
    registry_result="cancelled"
  else
    registry_result="skipped"
  fi

  # Merge Maven and Gradle central results into a single mavencentral target.
  if [[ "$maven_central_result" == "success" || "$gradle_central_result" == "success" ]]; then
    central_result="success"
  elif [[ "$maven_central_result" == "failure" || "$gradle_central_result" == "failure" ]]; then
    central_result="failure"
  elif [[ "$maven_central_result" == "cancelled" || "$gradle_central_result" == "cancelled" ]]; then
    central_result="cancelled"
  else
    central_result="skipped"
  fi

  local stage_ran=false
  if [[ "${GITHUBPACKAGES_MAVEN_ARTIFACTS:-[]}" != '[]' || "${GITHUBPACKAGES_GRADLE_ARTIFACTS:-[]}" != '[]' || "${MAVENCENTRAL_MAVEN_ARTIFACTS:-[]}" != '[]' || "${MAVENCENTRAL_GRADLE_ARTIFACTS:-[]}" != '[]' || "${XCODEIOS_ARTIFACTS:-[]}" != '[]' || "${GOOGLEPLAY_ARTIFACTS:-[]}" != '[]' || "${CONTAINERS:-[]}" != '[]' ]]; then
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
