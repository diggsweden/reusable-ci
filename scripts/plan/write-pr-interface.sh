#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local swift_bool="false"
  if [[ "${LINTER_SWIFTFORMAT}" == "true" || "${LINTER_SWIFTLINT}" == "true" ]]; then
    swift_bool="true"
  fi

  local pr_context pr_policy
  pr_context=$(printf '{"project_type":"%s","base_branch":"%s","reusable_ci_ref":"%s"}' \
    "$PROJECT_TYPE" "${BASE_BRANCH:-}" "$REUSABLE_CI_REF")

  pr_policy=$(printf '{"commitlint":%s,"licenselint":%s,"dependencyreview":%s,"megalint":%s,"publiccodelint":%s,"devbasecheck":%s,"swiftformat":%s,"swiftlint":%s,"swift":%s}' \
    "$(ci_json_bool "$LINTER_COMMITLINT")" \
    "$(ci_json_bool "$LINTER_LICENSLINT")" \
    "$(ci_json_bool "$LINTER_DEPENDENCYREVIEW")" \
    "$(ci_json_bool "$LINTER_MEGALINT")" \
    "$(ci_json_bool "$LINTER_PUBLICCODELINT")" \
    "$(ci_json_bool "$LINTER_DEVBASECHECK")" \
    "$(ci_json_bool "$LINTER_SWIFTFORMAT")" \
    "$(ci_json_bool "$LINTER_SWIFTLINT")" \
    "$swift_bool")

  ci_output "pr-context-json" "$pr_context"
  ci_output "pr-policy-json" "$pr_policy"
}

main "$@"
