#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

if [[ -f "gradle.properties" ]]; then
  VERSION=$(grep "versionName=" gradle.properties | cut -d'=' -f2 || printf "unknown")
  VERSION_CODE=$(grep "versionCode=" gradle.properties | cut -d'=' -f2 || printf "unknown")
  printf "version=%s\n" "${VERSION}"
  printf "version-code=%s\n" "${VERSION_CODE}"
  printf "Version: %s (%s)\n" "${VERSION}" "${VERSION_CODE}" >&2
else
  printf "::warning::gradle.properties not found, version info unavailable\n" >&2
  printf "version=unknown\n"
  printf "version-code=unknown\n"
fi
