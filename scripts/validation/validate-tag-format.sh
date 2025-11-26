#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
# SPDX-License-Identifier: CC0-1.0

# Validates that a git tag follows semantic versioning format
# Usage: validate-tag-format.sh <tag-name>

set -uo pipefail

TAG_NAME="${1:-}"

if [[ -z "$TAG_NAME" ]]; then
  printf "::error::Usage: validate-tag-format.sh <tag-name>\n"
  exit 1
fi

printf "## Validating Tag Format\n"

# Check if tag follows semantic versioning pattern
# Must start with 'v' followed by X.Y.Z where X, Y, Z are numbers
# Can optionally have pre-release suffix (e.g., -alpha.1, -beta.2, -rc.1, -dev)
if [[ ! "$TAG_NAME" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9\.\-]+)?$ ]]; then
  printf "::error::✗ Invalid tag format: '%s'\n" "$TAG_NAME"
  printf "\n"
  printf "Tags must follow semantic versioning: vMAJOR.MINOR.PATCH[-PRERELEASE]\n"
  printf "Valid: v1.0.0, v2.3.4-beta.1, v1.0.0-rc.2, v3.0.0-alpha, v1.0.0-dev\n"
  printf "Learn more: https://semver.org\n"
  exit 1
fi

# Extract version components
VERSION_PATTERN="^v([0-9]+)\.([0-9]+)\.([0-9]+)(-(.*))?$"
if [[ "$TAG_NAME" =~ $VERSION_PATTERN ]]; then
  MAJOR="${BASH_REMATCH[1]}"
  MINOR="${BASH_REMATCH[2]}"
  PATCH="${BASH_REMATCH[3]}"
  PRERELEASE="${BASH_REMATCH[5]}"

  printf "✓ Valid semantic version tag\n"
  printf "   Version: %s.%s.%s\n" "$MAJOR" "$MINOR" "$PATCH"
  if [[ -n "$PRERELEASE" ]]; then
    printf "   Pre-release: %s\n" "$PRERELEASE"

    # Validate pre-release format matches our allowed patterns
    if [[ "$PRERELEASE" =~ ^(alpha|beta|rc|snapshot|SNAPSHOT|dev)(\.[0-9]+)?$ ]]; then
      printf "   ✓ Pre-release identifier follows convention\n"
    else
      printf "   ℹ️ Non-standard pre-release identifier: %s\n" "$PRERELEASE"
      printf "      Standard identifiers: alpha, beta, rc, snapshot, SNAPSHOT, dev\n"
      printf "      (Release will proceed - this is informational only)\n"
    fi
  else
    printf "   Type: Stable release\n"
  fi
fi

printf "\n"
printf "### Tag Format Summary:\n"
printf "✓ Tag follows semantic versioning (vX.Y.Z)\n"
printf "✓ Tag format validation passed\n"
