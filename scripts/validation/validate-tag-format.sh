#!/bin/bash
# SPDX-FileCopyrightText: 2024 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Validates that a git tag follows semantic versioning format
# Usage: validate-tag-format.sh <tag-name>

set -euo pipefail

TAG_NAME="${1:-}"

if [[ -z "$TAG_NAME" ]]; then
  echo "::error::Usage: validate-tag-format.sh <tag-name>"
  exit 1
fi

echo "## Validating Tag Format"

# Check if tag follows semantic versioning pattern
# Must start with 'v' followed by X.Y.Z where X, Y, Z are numbers
# Can optionally have pre-release suffix (e.g., -alpha.1, -beta.2, -rc.1, -dev)
if [[ ! "$TAG_NAME" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9\.\-]+)?$ ]]; then
  echo "::error::❌ Invalid tag format: '$TAG_NAME'"
  echo ""
  echo "Tags must follow semantic versioning: vMAJOR.MINOR.PATCH[-PRERELEASE]"
  echo "Valid: v1.0.0, v2.3.4-beta.1, v1.0.0-rc.2, v3.0.0-alpha, v1.0.0-dev"
  echo "Learn more: https://semver.org"
  exit 1
fi

# Extract version components
VERSION_PATTERN="^v([0-9]+)\.([0-9]+)\.([0-9]+)(-(.*))?$"
if [[ "$TAG_NAME" =~ $VERSION_PATTERN ]]; then
  MAJOR="${BASH_REMATCH[1]}"
  MINOR="${BASH_REMATCH[2]}"
  PATCH="${BASH_REMATCH[3]}"
  PRERELEASE="${BASH_REMATCH[5]}"
  
  echo "✅ Valid semantic version tag"
  echo "   Version: $MAJOR.$MINOR.$PATCH"
  if [[ -n "$PRERELEASE" ]]; then
    echo "   Pre-release: $PRERELEASE"
    
    # Validate pre-release format matches our allowed patterns
    if [[ "$PRERELEASE" =~ ^(alpha|beta|rc|snapshot|SNAPSHOT|dev)(\.[0-9]+)?$ ]]; then
      echo "   ✅ Pre-release identifier follows convention"
    else
      echo "   ℹ️ Non-standard pre-release identifier: $PRERELEASE"
      echo "      Standard identifiers: alpha, beta, rc, snapshot, SNAPSHOT, dev"
      echo "      (Release will proceed - this is informational only)"
    fi
  else
    echo "   Type: Stable release"
  fi
fi

echo ""
echo "### Tag Format Summary:"
echo "✅ Tag follows semantic versioning (vX.Y.Z)"
echo "✅ Tag format validation passed"
