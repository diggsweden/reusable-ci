#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local project_type dev_context dev_policy

  # Project type resolution: caller-provided input wins. When omitted (only
  # valid for the artifacts-config path), fall back to the first artefact's
  # project_type as parsed from artifacts.yml. Fail loudly if neither is set
  # — silently defaulting would mis-route every later stage.
  project_type="${PROJECT_TYPE:-}"
  if [[ -z "$project_type" ]]; then
    project_type="${FALLBACK_PROJECT_TYPE:-}"
  fi
  if [[ -z "$project_type" ]]; then
    printf 'error: project-type input is empty and no artifacts-config first-project-type fallback is available\n' >&2
    exit 1
  fi

  dev_context=$(printf '{"project_type":"%s","branch":"%s","release_sha":"%s","release_actor":"%s","release_repository":"%s","working_directory":"%s","java_version":"%s","node_version":"%s","rust_toolchain":"%s","container_file":"%s","registry":"%s","reusable_ci_ref":"%s","npm_registry":"%s","package_scope":"%s","npm_registry_username":"%s"}' \
    "$project_type" "$BRANCH" "$RELEASE_SHA" "$RELEASE_ACTOR" "$RELEASE_REPOSITORY" \
    "$WORKING_DIRECTORY" "$JAVA_VERSION" "$NODE_VERSION" "${RUST_TOOLCHAIN:-stable}" "$CONTAINER_FILE" "$REGISTRY" \
    "$REUSABLE_CI_REF" "$NPM_REGISTRY" "$PACKAGE_SCOPE" "${NPM_REGISTRY_USERNAME:-}")

  dev_policy=$(printf '{"publish_npm":%s,"use_ci_token":%s}' \
    "$(ci_json_bool "$PUBLISH_NPM")" \
    "$(ci_json_bool "$USE_CI_TOKEN")")

  ci_output "dev-context-json" "$dev_context"
  ci_output "dev-policy-json" "$dev_policy"
}

main "$@"
