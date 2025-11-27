#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

readonly FLAVOR="${1:-}"
readonly BUILD_TYPES="${2:-debug,release}"
readonly INCLUDE_AAB="${3:-true}"
readonly BUILD_MODULE="${4:-app}"

FLAVOR_CAPITALIZED=""
if [[ -n "$FLAVOR" ]]; then
  FLAVOR_CAPITALIZED="$(printf "%s" "${FLAVOR}" | awk '{print toupper(substr($0,1,1)) tolower(substr($0,2))}')"
fi

TASKS="build"

if [[ "$BUILD_TYPES" == *"debug"* ]]; then
  TASKS="${TASKS} assemble${FLAVOR_CAPITALIZED}Debug"
fi

if [[ "$BUILD_TYPES" == *"release"* ]]; then
  TASKS="${TASKS} assemble${FLAVOR_CAPITALIZED}Release"
fi

if [[ "$INCLUDE_AAB" == "true" ]] && [[ "$BUILD_TYPES" == *"release"* ]]; then
  TASKS="${TASKS} ${BUILD_MODULE}:bundle${FLAVOR_CAPITALIZED}Release"
fi

printf "tasks=%s\n" "${TASKS}"
printf "Building with tasks: %s\n" "${TASKS}" >&2
