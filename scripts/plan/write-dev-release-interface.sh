#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local dev_context dev_policy
  dev_context=$(printf '{"project_type":"%s","branch":"%s","release_sha":"%s","release_actor":"%s","release_repository":"%s","working_directory":"%s","java_version":"%s","node_version":"%s","container_file":"%s","registry":"%s","reusable_ci_ref":"%s","npm_registry":"%s","package_scope":"%s","npm_registry_username":"%s"}' \
    "$PROJECT_TYPE" "$BRANCH" "$RELEASE_SHA" "$RELEASE_ACTOR" "$RELEASE_REPOSITORY" \
    "$WORKING_DIRECTORY" "$JAVA_VERSION" "$NODE_VERSION" "$CONTAINER_FILE" "$REGISTRY" \
    "$REUSABLE_CI_REF" "$NPM_REGISTRY" "$PACKAGE_SCOPE" "${NPM_REGISTRY_USERNAME:-}")

  dev_policy=$(printf '{"publish_npm":%s,"use_github_token":%s}' \
    "$(ci_json_bool "$PUBLISH_NPM")" \
    "$(ci_json_bool "$USE_GITHUB_TOKEN")")

  ci_output "dev-context-json" "$dev_context"
  ci_output "dev-policy-json" "$dev_policy"
}

main "$@"
