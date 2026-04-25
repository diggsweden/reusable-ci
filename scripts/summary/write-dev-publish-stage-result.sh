#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"
source "$SCRIPT_DIR/../ci/stage-result.sh"

main() {
  local container_result npm_result sbom_result

  container_result="$(ci_normalize_result "${BUILD_DEV_CONTAINER_RESULT:-skipped}")"
  npm_result="$(ci_normalize_result "${PUBLISH_NPM_DEV_RESULT:-skipped}")"
  sbom_result="$(ci_normalize_result "${GENERATE_DEV_SBOMS_RESULT:-skipped}")"

  local stage_ran="false"
  case "${PROJECT_TYPE:-}" in
  maven | npm | gradle) stage_ran="true" ;;
  esac

  # SBOM aggregation is opted-in via the sboms input (non-'none'); include in
  # the aggregate so an explicit opt-in that crashes surfaces as stage failure
  # rather than a silent pass.
  local stage_result
  stage_result="$(ci_stage_result "$stage_ran" "$container_result" "$npm_result" "$sbom_result")"

  local npm_target="skipped"
  if [[ "${PROJECT_TYPE:-}" == "npm" && "${PUBLISH_NPM:-false}" == "true" ]]; then
    npm_target="$npm_result"
  fi

  local targets_json result_json
  targets_json="$(ci_build_targets_json \
    "container:$container_result" \
    "npm:$npm_target" \
    "sbom:$sbom_result")"
  result_json="$(ci_stage_result_json "dev-publish" "$stage_result" "$stage_ran" "$targets_json" "project_type:${PROJECT_TYPE:-unknown}")"

  local artifacts_json
  artifacts_json=$(printf '{"container_image":"%s","container_digest":"%s","npm_package_name":"%s","npm_package_version":"%s","npm_publish_status":"%s"}' \
    "${CONTAINER_IMAGE:-}" "${CONTAINER_DIGEST:-}" "${NPM_PACKAGE_NAME:-}" "${NPM_PACKAGE_VERSION:-}" "${NPM_PUBLISH_STATUS:-published}")

  ci_output "stage-ran" "$stage_ran"
  ci_output "stage-result" "$stage_result"
  ci_output "result-json" "$result_json"
  ci_output "artifacts-json" "$artifacts_json"
}

main "$@"
