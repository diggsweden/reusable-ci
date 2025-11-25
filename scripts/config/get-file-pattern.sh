#!/bin/bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
#
# SPDX-License-Identifier: CC0-1.0
#
# Get file pattern for version bump based on project type
#
# Usage: get-file-pattern.sh <project-type> [custom-pattern]
#
# Returns the appropriate file pattern for git commit during version bump

set -euo pipefail

PROJECT_TYPE="${1:-}"
CUSTOM_PATTERN="${2:-}"

if [ -z "$PROJECT_TYPE" ]; then
  echo "Error: PROJECT_TYPE is required" >&2
  exit 1
fi

# If custom pattern provided, use it
if [ -n "$CUSTOM_PATTERN" ]; then
  echo "$CUSTOM_PATTERN"
  exit 0
fi

# Return default pattern based on project type
case "$PROJECT_TYPE" in
maven)
  echo "CHANGELOG.md :(glob)**/pom.xml"
  ;;
npm)
  echo "CHANGELOG.md package.json package-lock.json"
  ;;
gradle | gradle-android)
  echo "CHANGELOG.md gradle.properties build.gradle.kts settings.gradle.kts build.gradle settings.gradle"
  ;;
xcode-ios)
  echo "CHANGELOG.md :(glob)**/*.xcodeproj/project.pbxproj"
  ;;
python)
  echo "CHANGELOG.md pyproject.toml"
  ;;
go)
  echo "CHANGELOG.md go.mod"
  ;;
rust)
  echo "CHANGELOG.md Cargo.toml Cargo.lock"
  ;;
*)
  echo "CHANGELOG.md"
  ;;
esac
