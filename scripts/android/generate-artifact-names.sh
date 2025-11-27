#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

readonly INCLUDE_DATE="${1:-true}"
readonly PREFIX="${2:-}"
readonly REPO_NAME="${3:?Usage: $0 <include-date> <prefix> <repo-name> [flavor]}"
readonly FLAVOR="${4:-}"

DATE_STAMP=""
if [[ "$INCLUDE_DATE" == "true" ]]; then
  DATE_STAMP="$(date +'%Y-%m-%d') - "
fi

PREFIX_FORMATTED=""
if [[ -n "$PREFIX" ]]; then
  PREFIX_FORMATTED="${PREFIX} - "
fi

FLAVOR_SUFFIX=""
if [[ -n "$FLAVOR" ]]; then
  FLAVOR_SUFFIX=" - ${FLAVOR}"
fi

DEBUG_NAME="${DATE_STAMP}${PREFIX_FORMATTED}${REPO_NAME}${FLAVOR_SUFFIX} - APK debug"
RELEASE_NAME="${DATE_STAMP}${PREFIX_FORMATTED}${REPO_NAME}${FLAVOR_SUFFIX} - APK release"
AAB_NAME="${DATE_STAMP}${PREFIX_FORMATTED}${REPO_NAME}${FLAVOR_SUFFIX} - AAB release"

printf "debug-name=%s\n" "${DEBUG_NAME}"
printf "release-name=%s\n" "${RELEASE_NAME}"
printf "aab-name=%s\n" "${AAB_NAME}"

printf "Debug artifact: %s\n" "${DEBUG_NAME}" >&2
printf "Release artifact: %s\n" "${RELEASE_NAME}" >&2
printf "AAB artifact: %s\n" "${AAB_NAME}" >&2
