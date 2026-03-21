#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Validates that a git tag follows semantic versioning format
# Usage: validate-tag-format.sh <tag-name>

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

die() {
  ci_log_error "$1"
  exit 1
}

main() {
  local TAG_NAME="${1:-}"

  [[ -n "$TAG_NAME" ]] || die "Usage: validate-tag-format.sh <tag-name>"

  printf "## Validating Tag Format\n"

  if [[ ! "$TAG_NAME" =~ $CI_SEMVER_TAG_REGEX ]]; then
    ci_log_error "✗ Invalid tag format: '$TAG_NAME'"
    printf "\nTags must follow semantic versioning: vMAJOR.MINOR.PATCH[-PRERELEASE]\n"
    printf "Valid: v1.0.0, v2.3.4-beta.1, v1.0.0-rc.2, v3.0.0-alpha, v1.0.0-dev\n"
    printf "Learn more: https://semver.org\n"
    exit 1
  fi

  local MAJOR="${BASH_REMATCH[1]}"
  local MINOR="${BASH_REMATCH[2]}"
  local PATCH="${BASH_REMATCH[3]}"
  local PRERELEASE="${BASH_REMATCH[5]}"

  printf "✓ Valid semantic version tag\n"
  printf "   Version: %s.%s.%s\n" "$MAJOR" "$MINOR" "$PATCH"

  if [[ -n "$PRERELEASE" ]]; then
    printf "   Pre-release: %s\n" "$PRERELEASE"

    if [[ "$PRERELEASE" =~ $CI_PRERELEASE_SUFFIX_REGEX ]]; then
      printf "   ✓ Pre-release identifier follows convention\n"
    else
      printf "   ℹ️ Non-standard pre-release identifier: %s\n" "$PRERELEASE"
      printf "      Standard identifiers: alpha, beta, rc, snapshot, SNAPSHOT, dev\n"
      printf "      (Release will proceed - this is informational only)\n"
    fi
  else
    printf "   Type: Stable release\n"
  fi

  printf "\n### Tag Format Summary:\n"
  printf "✓ Tag follows semantic versioning (vX.Y.Z)\n"
  printf "✓ Tag format validation passed\n"
}

main "$@"
