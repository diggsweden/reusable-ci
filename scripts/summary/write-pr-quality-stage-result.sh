#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"
source "$SCRIPT_DIR/../ci/stage-result.sh"

main() {
  local commitlint_result licenselint_result dependencyreview_result megalint_result publiccodelint_result devbasecheck_result swift_result

  commitlint_result="$(ci_normalize_result "${COMMITLINT_RESULT:-skipped}")"
  licenselint_result="$(ci_normalize_result "${LICENSLINT_RESULT:-skipped}")"
  dependencyreview_result="$(ci_normalize_result "${DEPENDENCYREVIEW_RESULT:-skipped}")"
  megalint_result="$(ci_normalize_result "${MEGALINT_RESULT:-skipped}")"
  publiccodelint_result="$(ci_normalize_result "${PUBLICCODELINT_RESULT:-skipped}")"
  devbasecheck_result="$(ci_normalize_result "${DEVBASECHECK_RESULT:-skipped}")"
  swift_result="$(ci_normalize_result "${SWIFT_RESULT:-skipped}")"

  local stage_result
  stage_result="$(ci_aggregate_results "$commitlint_result" "$licenselint_result" "$dependencyreview_result" "$megalint_result" "$publiccodelint_result" "$devbasecheck_result" "$swift_result")"

  effective_result() {
    if [[ "$1" == "true" ]]; then printf '%s' "$2"; else printf 'skipped'; fi
  }

  local targets_json result_json
  targets_json="$(ci_build_targets_json \
    "commitlint:$(effective_result "${COMMITLINT_ENABLED:-false}" "$commitlint_result")" \
    "licenselint:$(effective_result "${LICENSLINT_ENABLED:-false}" "$licenselint_result")" \
    "dependencyreview:$(effective_result "${DEPENDENCYREVIEW_ENABLED:-false}" "$dependencyreview_result")" \
    "megalint:$(effective_result "${MEGALINT_ENABLED:-false}" "$megalint_result")" \
    "publiccodelint:$(effective_result "${PUBLICCODELINT_ENABLED:-false}" "$publiccodelint_result")" \
    "devbasecheck:$(effective_result "${DEVBASECHECK_ENABLED:-false}" "$devbasecheck_result")" \
    "swift:$(effective_result "${SWIFT_ENABLED:-false}" "$swift_result")")"
  result_json="$(ci_stage_result_json "pr-quality" "$stage_result" "true" "$targets_json")"

  ci_output "stage-ran" "true"
  ci_output "stage-result" "$stage_result"
  ci_output "result-json" "$result_json"
}

main "$@"
