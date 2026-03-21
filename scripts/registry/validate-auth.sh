#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Registry Authentication Validator
#
# Purpose: Validates registry authentication configuration before publishing.
# Ensures proper credentials are provided and warns about misconfigurations.
#
# Usage (positional args): validate-auth.sh USE_CI_TOKEN REGISTRY EXPECTED_REGISTRY HAS_PASSWORD
# Usage (env vars):        USE_CI_TOKEN=... TARGET_REGISTRY=... validate-auth.sh
#
# When called with no args, reads from env vars:
#   USE_CI_TOKEN      - "true" or "false" - Whether to use the CI platform token
#   TARGET_REGISTRY   - Registry URL being used (e.g., "ghcr.io")
#   CI_REGISTRY       - Expected registry for the CI token (defaults to "ghcr.io")
#   REGISTRY_PASSWORD - Registry password (presence checked, not value)
#
# When called with positional args:
#   $1 USE_CI_TOKEN, $2 REGISTRY, $3 EXPECTED_REGISTRY, $4 HAS_PASSWORD ("true"/"false")
#
# Exit codes:
#   0 - Configuration is valid
#   1 - Configuration is invalid (missing required secrets)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  local use_ci_token registry expected_registry has_password

  if [[ $# -ge 4 ]]; then
    use_ci_token="$1"
    registry="$2"
    expected_registry="$3"
    has_password="$4"
  else
    use_ci_token="${USE_CI_TOKEN:?USE_CI_TOKEN is required}"
    registry="${TARGET_REGISTRY:?TARGET_REGISTRY is required}"
    expected_registry="${CI_REGISTRY:-ghcr.io}"
    if [[ -n "${REGISTRY_PASSWORD:-}" ]]; then
      has_password="true"
    else
      has_password="false"
    fi
  fi

  # Error: Using custom auth but no password provided
  if [[ "$use_ci_token" = "false" && "$has_password" = "false" ]]; then
    ci_log_error "registry-password secret is required when use-ci-token=false"
    exit 1
  fi

  # Warning: Using CI token with non-default registry
  if [[ "$registry" != "$expected_registry" && "$use_ci_token" = "true" ]]; then
    ci_log_warning "Using CI token with non-$expected_registry registry ($registry)"
    ci_log_warning "This will likely fail. Set use-ci-token=false and provide registry-password secret"
  fi

  printf "✓ Registry authentication configuration is valid\n"
}

main "$@"
