#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local release_context release_policy
  release_context=$(printf '{"project_type":"%s","build_type":"%s","artifact_name":"%s"}' \
    "$FIRST_PROJECT_TYPE" "$FIRST_BUILD_TYPE" "$FIRST_ARTIFACT_NAME")

  release_policy=$(printf '{"sign_artifacts":%s,"check_authorization":%s,"run_version_bump":%s,"create_github_release":%s,"create_draft_release":%s,"generate_sbom":%s,"make_latest":%s,"has_containers":%s}' \
    "$(ci_json_bool "$SHOULD_SIGN_ARTIFACTS")" \
    "$(ci_json_bool "$SHOULD_CHECK_AUTHORIZATION")" \
    "$(ci_json_bool "$SHOULD_RUN_VERSION_BUMP")" \
    "$(ci_json_bool "$SHOULD_CREATE_GITHUB_RELEASE")" \
    "$(ci_json_bool "$SHOULD_CREATE_DRAFT_RELEASE")" \
    "$(ci_json_bool "$SHOULD_GENERATE_SBOM")" \
    "$(ci_json_bool "$SHOULD_MAKE_LATEST")" \
    "$(ci_json_bool "$HAS_CONTAINERS")")

  ci_output "release-context-json" "$release_context"
  ci_output "release-policy-json" "$release_policy"
}

main "$@"
