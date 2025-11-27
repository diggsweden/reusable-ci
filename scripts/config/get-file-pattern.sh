#!/usr/bin/env bash
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

if [[ -z "$PROJECT_TYPE" ]]; then
  printf "Error: PROJECT_TYPE is required\n" >&2
  exit 1
fi

# If custom pattern provided, use it
if [[ -n "$CUSTOM_PATTERN" ]]; then
  printf "%s\n" "$CUSTOM_PATTERN"
  exit 0
fi

# Return default pattern based on project type
case "$PROJECT_TYPE" in
maven)
  printf "CHANGELOG.md :(glob)**/pom.xml\n"
  ;;
npm)
  printf "CHANGELOG.md package.json package-lock.json\n"
  ;;
gradle | gradle-android)
  printf "CHANGELOG.md gradle.properties build.gradle.kts settings.gradle.kts build.gradle settings.gradle\n"
  ;;
xcode-ios)
  printf "CHANGELOG.md :(glob)**/*.xcodeproj/project.pbxproj\n"
  ;;
python)
  printf "CHANGELOG.md pyproject.toml\n"
  ;;
go)
  printf "CHANGELOG.md go.mod\n"
  ;;
rust)
  printf "CHANGELOG.md Cargo.toml Cargo.lock\n"
  ;;
*)
  printf "CHANGELOG.md\n"
  ;;
esac
