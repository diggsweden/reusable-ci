#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local container_result npm_result

  container_result="$(ci_normalize_result "${BUILD_DEV_CONTAINER_RESULT:-skipped}")"
  npm_result="$(ci_normalize_result "${PUBLISH_NPM_DEV_RESULT:-skipped}")"

  local stage_ran="false"
  case "${PROJECT_TYPE:-}" in
  maven | npm | gradle) stage_ran="true" ;;
  esac

  local stage_result
  if [[ "$stage_ran" != "true" ]]; then
    stage_result="skipped"
  elif [[ "$container_result" == "failure" || "$npm_result" == "failure" ]]; then
    stage_result="failure"
  elif [[ "$container_result" == "cancelled" || "$npm_result" == "cancelled" ]]; then
    stage_result="cancelled"
  else
    stage_result="success"
  fi

  local npm_target="skipped"
  if [[ "${PROJECT_TYPE:-}" == "npm" && "${PUBLISH_NPM:-false}" == "true" ]]; then
    npm_target="$npm_result"
  fi

  local result_json
  result_json=$(printf '{"stage":"dev-publish","result":"%s","ran":%s,"project_type":"%s","targets":{"container":"%s","npm":"%s"}}' \
    "$stage_result" "$(ci_json_bool "$stage_ran")" "${PROJECT_TYPE:-unknown}" "$container_result" "$npm_target")

  local artifacts_json
  artifacts_json=$(printf '{"container_image":"%s","container_digest":"%s","npm_package_name":"%s","npm_package_version":"%s"}' \
    "${CONTAINER_IMAGE:-}" "${CONTAINER_DIGEST:-}" "${NPM_PACKAGE_NAME:-}" "${NPM_PACKAGE_VERSION:-}")

  ci_output "stage-ran" "$stage_ran"
  ci_output "stage-result" "$stage_result"
  ci_output "result-json" "$result_json"
  ci_output "artifacts-json" "$artifacts_json"
}

main "$@"
