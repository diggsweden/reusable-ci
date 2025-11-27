#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
#
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

git fetch --tags || true

# Find latest semver tag using git describe or fallback to sorted tag list
LATEST_TAG=$(git describe --tags --match="v[0-9]*.[0-9]*.[0-9]*" --abbrev=0 2>/dev/null ||
  git tag -l "v[0-9]*.[0-9]*.[0-9]*" | sort -V | tail -n1 || printf "")

if [[ -z "$LATEST_TAG" ]]; then
  BASE_VERSION="0.0.0"
else
  BASE_VERSION="${LATEST_TAG#v}"
fi

# Sanitize branch name for version compatibility
# - Replace non-alphanumeric (except . _ -) with dash
# - Remove leading/trailing dashes
BRANCH_NAME="${GITHUB_REF#refs/heads/}"
SANITIZED_BRANCH=$(printf "%s" "$BRANCH_NAME" | sed 's|[^a-zA-Z0-9._-]|-|g' | sed 's|^-*||; s|-*$||')

# Get short SHA (7 characters)
SHORT_SHA=$(git rev-parse --short=7 HEAD)

# Output final dev version
printf "%s-dev-%s-%s\n" "${BASE_VERSION}" "${SANITIZED_BRANCH}" "${SHORT_SHA}"
