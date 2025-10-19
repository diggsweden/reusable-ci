#!/bin/bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
#
# SPDX-License-Identifier: CC0-1.0

# Registry Authentication Validator
#
# Purpose: Validates registry authentication configuration before publishing.
# Ensures proper credentials are provided and warns about misconfigurations.
#
# Usage: validate-auth.sh USE_GITHUB_TOKEN REGISTRY EXPECTED_REGISTRY HAS_PASSWORD
#
# Arguments:
#   USE_GITHUB_TOKEN  - "true" or "false" - Whether to use GITHUB_TOKEN
#   REGISTRY          - Registry URL being used (e.g., "ghcr.io")
#   EXPECTED_REGISTRY - Expected registry for GITHUB_TOKEN (e.g., "ghcr.io")
#   HAS_PASSWORD      - "true" or "false" - Whether registry-password secret is set
#
# Examples:
#   validate-auth.sh true "ghcr.io" "ghcr.io" false
#   validate-auth.sh false "docker.io" "ghcr.io" true
#
# Exit codes:
#   0 - Configuration is valid
#   1 - Configuration is invalid (missing required secrets)

set -euo pipefail

USE_GITHUB_TOKEN="$1"
REGISTRY="$2"
EXPECTED_REGISTRY="$3"
HAS_PASSWORD="$4"

# Error: Using custom auth but no password provided
if [ "$USE_GITHUB_TOKEN" = "false" ] && [ "$HAS_PASSWORD" = "false" ]; then
  echo "::error::registry-password secret is required when use-github-token=false"
  exit 1
fi

# Warning: Using GITHUB_TOKEN with non-default registry
if [ "$REGISTRY" != "$EXPECTED_REGISTRY" ] && [ "$USE_GITHUB_TOKEN" = "true" ]; then
  echo "::warning::Using GITHUB_TOKEN with non-$EXPECTED_REGISTRY registry ($REGISTRY)"
  echo "::warning::This will likely fail. Set use-github-token=false and provide registry-password secret"
fi

echo "✓ Registry authentication configuration is valid"
