#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

readonly PROJECT="${1:-}"
readonly WORKSPACE="${2:-}"

if [[ -n "$PROJECT" ]]; then
  PROJECT_FILE="$PROJECT"
elif [[ -n "$WORKSPACE" ]]; then
  PROJECT_FILE=$(find . -name "*.xcodeproj" -type d | head -1)
else
  PROJECT_FILE=$(find . -name "*.xcodeproj" -type d | head -1)
fi

if [[ -f "$PROJECT_FILE/project.pbxproj" ]]; then
  VERSION=$(grep -m1 'MARKETING_VERSION' "$PROJECT_FILE/project.pbxproj" | awk -F' = ' '{print $2}' | tr -d ';' || printf "unknown")
  BUILD=$(grep -m1 'CURRENT_PROJECT_VERSION' "$PROJECT_FILE/project.pbxproj" | awk -F' = ' '{print $2}' | tr -d ';' || printf "unknown")
  printf "version=%s\n" "${VERSION}"
  printf "build=%s\n" "${BUILD}"
  printf "Version: %s (%s)\n" "${VERSION}" "${BUILD}" >&2
else
  printf "::warning::Could not determine version from project file\n" >&2
  printf "version=unknown\n"
  printf "build=unknown\n"
fi
