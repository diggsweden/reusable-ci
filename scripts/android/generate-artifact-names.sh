#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

main() {
  readonly INCLUDE_DATE="${1:-true}"
  readonly PREFIX="${2:-}"
  readonly REPO_NAME="${3:?Usage: $0 <include-date> <prefix> <repo-name> [flavor]}"
  readonly FLAVOR="${4:-}"

  local DATE_STAMP=""
  if [[ "$INCLUDE_DATE" == "true" ]]; then
    DATE_STAMP="$(date +'%Y-%m-%d') - "
  fi

  local PREFIX_FORMATTED=""
  if [[ -n "$PREFIX" ]]; then
    PREFIX_FORMATTED="${PREFIX} - "
  fi

  local FLAVOR_SUFFIX=""
  if [[ -n "$FLAVOR" ]]; then
    FLAVOR_SUFFIX=" - ${FLAVOR}"
  fi

  local DEBUG_NAME="${DATE_STAMP}${PREFIX_FORMATTED}${REPO_NAME}${FLAVOR_SUFFIX} - APK debug"
  local RELEASE_NAME="${DATE_STAMP}${PREFIX_FORMATTED}${REPO_NAME}${FLAVOR_SUFFIX} - APK release"
  local AAB_NAME="${DATE_STAMP}${PREFIX_FORMATTED}${REPO_NAME}${FLAVOR_SUFFIX} - AAB release"
  local SBOM_NAME="${DATE_STAMP}${PREFIX_FORMATTED}${REPO_NAME}${FLAVOR_SUFFIX} - build SBOM"

  printf "debug-name=%s\n" "${DEBUG_NAME}"
  printf "release-name=%s\n" "${RELEASE_NAME}"
  printf "aab-name=%s\n" "${AAB_NAME}"
  printf "sbom-name=%s\n" "${SBOM_NAME}"

  printf "Debug artifact: %s\n" "${DEBUG_NAME}" >&2
  printf "Release artifact: %s\n" "${RELEASE_NAME}" >&2
  printf "AAB artifact: %s\n" "${AAB_NAME}" >&2
  printf "SBOM artifact: %s\n" "${SBOM_NAME}" >&2
}

main "$@"
