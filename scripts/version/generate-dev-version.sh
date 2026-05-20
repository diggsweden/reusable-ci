#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Development Version Generator
#
# Purpose: Generates standardized development version tags for NPM packages and container images.
#
# Output Format: {BASE_VERSION}-dev-{BRANCH}-{SHORT_SHA}
# Example: 0.5.9-dev-feat-awesome-abc1234
#
# How it works:
# 1. Finds the latest semver tag (vX.Y.Z) in git history
# 2. Falls back to 0.0.0 if no tags exist
# 3. Sanitizes branch name for version compatibility
# 4. Appends short commit SHA for uniqueness
#
# Usage: generate-dev-version.sh
# Returns: dev version string on stdout

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/env.sh"
source "$SCRIPT_DIR/../ci/strings.sh"

main() {
  git fetch --tags || true

  # Find latest semver tag using git describe or fallback to sorted tag list
  local LATEST_TAG
  LATEST_TAG=$(git describe --tags --match="v[0-9]*.[0-9]*.[0-9]*" --abbrev=0 2>/dev/null ||
    git tag -l "v[0-9]*.[0-9]*.[0-9]*" | sort -V | tail -n1 || printf "")

  local BASE_VERSION
  if [[ -z "$LATEST_TAG" ]]; then
    BASE_VERSION="0.0.0"
  else
    BASE_VERSION="${LATEST_TAG#v}"
  fi

  # Sanitize branch name for version compatibility (Docker tag + filesystem
  # safe). Single rule lives in scripts/ci/strings.sh.
  local SANITIZED_BRANCH
  SANITIZED_BRANCH=$(sanitize_path_token "$CI_REF_NAME")

  # Get short SHA (7 characters)
  local SHORT_SHA
  SHORT_SHA=$(git rev-parse --short=7 HEAD)

  # Output final dev version
  printf "%s-dev-%s-%s\n" "${BASE_VERSION}" "${SANITIZED_BRANCH}" "${SHORT_SHA}"
}

main "$@"
