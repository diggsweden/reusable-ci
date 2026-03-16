#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local commitlint_result licenselint_result dependencyreview_result megalint_result publiccodelint_result devbasecheck_result swift_result
  local stage_result

  commitlint_result="$(ci_normalize_result "${COMMITLINT_RESULT:-skipped}")"
  licenselint_result="$(ci_normalize_result "${LICENSLINT_RESULT:-skipped}")"
  dependencyreview_result="$(ci_normalize_result "${DEPENDENCYREVIEW_RESULT:-skipped}")"
  megalint_result="$(ci_normalize_result "${MEGALINT_RESULT:-skipped}")"
  publiccodelint_result="$(ci_normalize_result "${PUBLICCODELINT_RESULT:-skipped}")"
  devbasecheck_result="$(ci_normalize_result "${DEVBASECHECK_RESULT:-skipped}")"
  swift_result="$(ci_normalize_result "${SWIFT_RESULT:-skipped}")"

  if [[ "$commitlint_result" == 'failure' || "$licenselint_result" == 'failure' || "$dependencyreview_result" == 'failure' || "$megalint_result" == 'failure' || "$publiccodelint_result" == 'failure' || "$devbasecheck_result" == 'failure' || "$swift_result" == 'failure' ]]; then
    stage_result='failure'
  elif [[ "$commitlint_result" == 'cancelled' || "$licenselint_result" == 'cancelled' || "$dependencyreview_result" == 'cancelled' || "$megalint_result" == 'cancelled' || "$publiccodelint_result" == 'cancelled' || "$devbasecheck_result" == 'cancelled' || "$swift_result" == 'cancelled' ]]; then
    stage_result='cancelled'
  else
    stage_result='success'
  fi

  effective_result() {
    if [[ "$1" == "true" ]]; then printf '%s' "$2"; else printf 'skipped'; fi
  }

  local result_json
  result_json=$(printf '{"stage":"pr-quality","result":"%s","ran":true,"targets":{"commitlint":"%s","licenselint":"%s","dependencyreview":"%s","megalint":"%s","publiccodelint":"%s","devbasecheck":"%s","swift":"%s"}}' \
    "$stage_result" \
    "$(effective_result "${COMMITLINT_ENABLED:-false}" "$commitlint_result")" \
    "$(effective_result "${LICENSLINT_ENABLED:-false}" "$licenselint_result")" \
    "$(effective_result "${DEPENDENCYREVIEW_ENABLED:-false}" "$dependencyreview_result")" \
    "$(effective_result "${MEGALINT_ENABLED:-false}" "$megalint_result")" \
    "$(effective_result "${PUBLICCODELINT_ENABLED:-false}" "$publiccodelint_result")" \
    "$(effective_result "${DEVBASECHECK_ENABLED:-false}" "$devbasecheck_result")" \
    "$(effective_result "${SWIFT_ENABLED:-false}" "$swift_result")")

  ci_output "stage-ran" "true"
  ci_output "stage-result" "$stage_result"
  ci_output "result-json" "$result_json"
}

main "$@"
